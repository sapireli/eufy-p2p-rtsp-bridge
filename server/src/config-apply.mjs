import fs from "node:fs/promises";
import { dirname, basename, join } from "node:path";
import { randomUUID, createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { loadConfig } from "./config.mjs";

const exec = promisify(execFile);

const sha256 = (s) => createHash("sha256").update(s).digest("hex");

async function syncDir(dir) {
  const handle = await fs.open(dir, "r");
  try { await handle.sync(); } finally { await handle.close(); }
}

async function atomicWrite(path, bytes, { mode = 0o600, uid, gid } = {}) {
  const dir = dirname(path);
  const staged = join(dir, `.${basename(path)}.${randomUUID()}.tmp`);
  const handle = await fs.open(staged, "wx", mode);
  try {
    if (uid != null && gid != null) await handle.chown(uid, gid);
    await handle.chmod(mode);
    await handle.writeFile(bytes);
    await handle.sync();
  } catch (error) {
    await handle.close();
    await fs.unlink(staged).catch(() => {});
    throw error;
  }
  await handle.close();
  try { await fs.rename(staged, path); await syncDir(dir); }
  catch (error) { await fs.unlink(staged).catch(() => {}); throw error; }
}

async function readOptional(path) {
  try { return await fs.readFile(path); } catch (error) { if (error.code === "ENOENT") return null; throw error; }
}

async function restorePrevious(target, previous, backup, options) {
  if (previous) {
    try {
      await atomicWrite(target, previous, options);
      return { backup };
    } catch (writeError) {
      // A full filesystem can reject another copy of the old YAML. The
      // durable backup is already on this filesystem and can replace it.
      if (!backup || (writeError.code !== "ENOSPC" && writeError.code !== "EDQUOT")) throw writeError;
      const staged = join(dirname(target), `.${basename(target)}.${randomUUID()}.restore`);
      let replaced = false, linkFailure;
      try {
        await fs.link(backup, staged);
        await fs.rename(staged, target);
        replaced = true;
        await syncDir(dirname(target));
        return { backup };
      } catch (linkError) {
        await fs.unlink(staged).catch(() => {});
        if (replaced) throw linkError;
        linkFailure = linkError;
      }
      try {
        await fs.rename(backup, target);
        await syncDir(dirname(target));
        return { backup: null, backupConsumed: true };
      } catch (renameError) {
        throw new AggregateError([writeError, linkFailure, renameError], `could not restore previous config from its backup: ${writeError.message}; ${linkFailure.message}; ${renameError.message}`);
      }
    }
  }
  try { await fs.unlink(target); } catch (error) { if (error.code !== "ENOENT") throw error; }
  await syncDir(dirname(target));
  return { backup: null };
}

async function archiveFailed(path, candidate, options) {
  if (!candidate) {
    try { return { failed: (await fs.stat(path)).isFile() ? path : null }; }
    catch (error) { return { failed: null, ...(error.code === "ENOENT" ? {} : { archiveError: error.message }) }; }
  }
  try { await atomicWrite(path, candidate, options); return { failed: path }; }
  catch (error) { return { failed: null, archiveError: error.message }; }
}

async function recordRollback(path, target, previous, lastRollback) {
  try { await atomicWrite(path, JSON.stringify({ target, sha256: previous ? sha256(previous) : null, lastRollback }) + "\n"); }
  catch (error) { lastRollback.statusError = error.message; }
}

async function serviceIdentity() {
  try {
    const [user, group] = await Promise.all([exec("id", ["-u", "eufy-wall"]), exec("id", ["-g", "eufy-wall"])]);
    return { uid: Number(user.stdout.trim()), gid: Number(group.stdout.trim()) };
  } catch { return null; }
}

async function processStartToken(pid) {
  try {
    const stat = await fs.readFile(`/proc/${pid}/stat`, "utf8");
    return stat.slice(stat.lastIndexOf(")") + 2).split(" ")[19]; // Linux field 22: process start time in ticks
  } catch { return null; }
}

async function ownerIsRunning(owner, ownerStart) {
  if (!Number.isInteger(owner) || owner <= 0) return false;
  try { process.kill(owner, 0); }
  catch (error) {
    if (error.code === "ESRCH") return false;
    if (error.code === "EPERM") return true;
    throw error;
  }
  const currentStart = await processStartToken(owner);
  return !ownerStart || !currentStart || ownerStart === currentStart;
}

async function createLock(path) {
  const dir = dirname(path);
  const staged = join(dir, `.${basename(path)}.${randomUUID()}.tmp`);
  const handle = await fs.open(staged, "wx", 0o600);
  try {
    await handle.writeFile(JSON.stringify({ owner: process.pid, ownerStart: await processStartToken(process.pid) }) + "\n");
    await handle.sync();
  } catch (error) {
    await handle.close();
    await fs.unlink(staged).catch(() => {});
    throw error;
  }
  await handle.close();
  let linked = false;
  try { await fs.link(staged, path); linked = true; await syncDir(dir); }
  catch (error) { if (linked) await fs.unlink(path).catch(() => {}); throw error; }
  finally { await fs.unlink(staged).catch(() => {}); }
}

async function recoverStaleLock(path) {
  let raw;
  try { raw = await fs.readFile(path, "utf8"); }
  catch (error) { if (error.code === "ENOENT") return { recovered: false }; throw error; }
  let owner;
  try {
    owner = raw.trim().startsWith("{") ? JSON.parse(raw) : { owner: Number(raw.trim()) };
  } catch { throw new Error(`invalid config apply lock ${path}; inspect it before removal`); }
  if (!Number.isInteger(owner.owner) || owner.owner <= 0 || (owner.ownerStart != null && typeof owner.ownerStart !== "string")) {
    throw new Error(`invalid config apply lock ${path}; inspect it before removal`);
  }
  if (await ownerIsRunning(owner.owner, owner.ownerStart)) return { recovered: false, inProgress: true };
  await fs.unlink(path);
  await syncDir(dirname(path));
  return { recovered: true, staleLock: true };
}

/** Apply a validated candidate, restart the service, and restore the previous bytes on failure. */
async function applyConfigLocked({ target, yamlText, env = process.env, restart, health, now = () => Date.now() }) {
  if (!target || typeof yamlText !== "string") throw new Error("target path and YAML text are required");
  const { cfg } = loadConfig({ env, rawText: yamlText });
  const old = await readOptional(target);
  if (old?.equals(Buffer.from(yamlText))) return { changed: false, sha256: sha256(yamlText), target };
  const oldStat = old ? await fs.stat(target) : null;
  const identity = oldStat ? null : await serviceIdentity();
  const mode = oldStat ? oldStat.mode & 0o777 : identity ? 0o640 : 0o600;
  const ownership = oldStat ? { uid: oldStat.uid, gid: oldStat.gid } : identity ?? {};
  const timestamp = new Date(now()).toISOString().replaceAll(":", "-");
  const backup = old ? `${target}.bak-${timestamp}-${randomUUID().slice(0, 8)}` : null;
  const failed = `${target}.failed-${timestamp}-${randomUUID().slice(0, 8)}`;
  const statusPath = `${target}.apply-status.json`;
  const pendingPath = `${target}.apply-pending.json`;
  if (backup) await atomicWrite(backup, old, { mode, ...ownership });
  await atomicWrite(pendingPath, JSON.stringify({ owner: process.pid, ownerStart: await processStartToken(process.pid), target, backup, previousSha256: old ? sha256(old) : null, failed, mode, ...ownership, at: new Date(now()).toISOString() }) + "\n");
  await atomicWrite(target, yamlText, { mode, ...ownership });
  try {
    await restart();
    await health(cfg);
    const result = { changed: true, target, backup, sha256: sha256(yamlText), appliedAt: new Date(now()).toISOString() };
    await atomicWrite(statusPath, JSON.stringify({ ...result, lastRollback: null }) + "\n");
    await fs.unlink(pendingPath);
    return result;
  } catch (error) {
    let restored;
    try { restored = await restorePrevious(target, old, backup, { mode, ...ownership }); }
    catch (restoreError) {
      throw new Error(`apply failed: ${error.message}; previous config restoration failed: ${restoreError.message}; pending journal retained`, { cause: restoreError });
    }
    let recoveryError;
    try { await restart(); if (old) await health(loadConfig({ env, rawText: old.toString() }).cfg); }
    catch (e) { recoveryError = e; }
    const archive = await archiveFailed(failed, Buffer.from(yamlText), { mode, ...ownership });
    const lastRollback = { at: new Date(now()).toISOString(), reason: error.message, ...archive, ...restored, recoveryError: recoveryError?.message };
    await recordRollback(statusPath, target, old, lastRollback);
    await fs.unlink(pendingPath);
    const detail = [recoveryError && `recovery check failed: ${recoveryError.message}`, archive.archiveError && `failed candidate archive failed: ${archive.archiveError}`, lastRollback.statusError && `rollback status write failed: ${lastRollback.statusError}`].filter(Boolean).join("; ");
    throw Object.assign(new Error(`apply failed; restored previous config: ${error.message}${detail ? `; ${detail}` : ""}`), { lastRollback });
  }
}

export async function applyConfig(options) {
  if (!options?.target) throw new Error("target path is required");
  const lockPath = `${options.target}.apply-lock`;
  const recovery = await recoverInterruptedApply(options.target);
  if (recovery.inProgress) throw new Error(`another config apply holds ${lockPath}; check the running process before removing a stale lock`);
  try {
    await createLock(lockPath);
  } catch (error) {
    if (error.code !== "EEXIST") throw error;
    throw new Error(`another config apply holds ${lockPath}; check the running process before removing a stale lock`);
  }
  try {
    return await applyConfigLocked(options);
  } finally {
    await fs.unlink(lockPath).catch(() => {});
  }
}

/** Run as root before service start; restore a candidate whose applying process died mid-transaction. */
export async function recoverInterruptedApply(target) {
  const pendingPath = `${target}.apply-pending.json`;
  let pending;
  try { pending = JSON.parse(await fs.readFile(pendingPath, "utf8")); }
  catch (error) { if (error.code === "ENOENT") return recoverStaleLock(`${target}.apply-lock`); throw error; }
  if (pending.target !== target || !pending.failed) throw new Error("invalid pending apply journal");
  if (await ownerIsRunning(pending.owner, pending.ownerStart)) return { recovered: false, inProgress: true };
  let backup = null, alreadyRestored = false;
  if (pending.backup) {
    try { backup = await fs.readFile(pending.backup); }
    catch (error) {
      if (error.code !== "ENOENT" || !pending.previousSha256) throw error;
      const current = await readOptional(target);
      if (!current || sha256(current) !== pending.previousSha256) throw error;
      backup = current;
      alreadyRestored = true;
    }
  }
  let candidate, candidateReadError;
  try { candidate = await readOptional(target); } catch (error) { candidateReadError = error; }
  const options = { mode: pending.mode, uid: pending.uid, gid: pending.gid };
  const restored = alreadyRestored ? { backup: null, backupConsumed: true } : await restorePrevious(target, backup, pending.backup, options);
  const archive = candidateReadError
    ? { failed: null, archiveError: `could not read failed candidate: ${candidateReadError.message}` }
    : await archiveFailed(pending.failed, candidate && (!backup || !candidate.equals(backup)) ? candidate : null, options);
  const lastRollback = { at: new Date().toISOString(), reason: "apply process stopped before health confirmation", ...archive, ...restored };
  await recordRollback(`${target}.apply-status.json`, target, backup, lastRollback);
  await fs.unlink(pendingPath);
  await fs.unlink(`${target}.apply-lock`).catch(() => {});
  return { recovered: true, lastRollback };
}

export async function applyStatus(target) {
  try { return JSON.parse(await fs.readFile(`${target}.apply-status.json`, "utf8")); }
  catch (error) { if (error.code === "ENOENT") return null; throw error; }
}
