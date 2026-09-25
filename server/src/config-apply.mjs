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
  await atomicWrite(pendingPath, JSON.stringify({ owner: process.pid, ownerStart: await processStartToken(process.pid), target, backup, failed, mode, ...ownership, at: new Date(now()).toISOString() }) + "\n");
  await atomicWrite(target, yamlText, { mode, ...ownership });
  try {
    await restart();
    await health(cfg);
    const result = { changed: true, target, backup, sha256: sha256(yamlText), appliedAt: new Date(now()).toISOString() };
    await atomicWrite(statusPath, JSON.stringify({ ...result, lastRollback: null }) + "\n");
    await fs.unlink(pendingPath);
    return result;
  } catch (error) {
    // Keep the failed candidate for diagnosis; restore previous bytes with another atomic rename.
    await atomicWrite(failed, yamlText, { mode, ...ownership });
    if (old) await atomicWrite(target, old, { mode, ...ownership });
    else await fs.unlink(target).catch(() => {});
    let recoveryError;
    try { await restart(); if (old) await health(loadConfig({ env, rawText: old.toString() }).cfg); }
    catch (e) { recoveryError = e; }
    const lastRollback = { at: new Date(now()).toISOString(), reason: error.message, failed, backup, recoveryError: recoveryError?.message };
    await atomicWrite(statusPath, JSON.stringify({ target, sha256: old ? sha256(old) : null, lastRollback }) + "\n");
    await fs.unlink(pendingPath);
    throw Object.assign(new Error(`apply failed; restored previous config: ${error.message}${recoveryError ? `; recovery check failed: ${recoveryError.message}` : ""}`), { lastRollback });
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
  const candidate = await readOptional(target);
  if (candidate) await atomicWrite(pending.failed, candidate, { mode: pending.mode, uid: pending.uid, gid: pending.gid });
  const backup = pending.backup ? await fs.readFile(pending.backup) : null;
  if (backup) await atomicWrite(target, backup, { mode: pending.mode, uid: pending.uid, gid: pending.gid });
  else { await fs.unlink(target).catch(() => {}); await syncDir(dirname(target)); }
  const lastRollback = { at: new Date().toISOString(), reason: "apply process stopped before health confirmation", failed: pending.failed, backup: pending.backup };
  await atomicWrite(`${target}.apply-status.json`, JSON.stringify({ target, sha256: backup ? sha256(backup) : null, lastRollback }) + "\n");
  await fs.unlink(pendingPath);
  await fs.unlink(`${target}.apply-lock`).catch(() => {});
  return { recovered: true, lastRollback };
}

export async function applyStatus(target) {
  try { return JSON.parse(await fs.readFile(`${target}.apply-status.json`, "utf8")); }
  catch (error) { if (error.code === "ENOENT") return null; throw error; }
}
