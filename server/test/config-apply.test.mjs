import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import { basename, join } from "node:path";
import { createHash } from "node:crypto";
import { applyConfig, applyStatus, recoverInterruptedApply } from "../src/config-apply.mjs";

const ENV = { EUFY_EMAIL: "e", EUFY_PASSWORD: "p" };
const GOOD = "schema_version: 2\nport: 3000\n";
const NEXT = "schema_version: 2\nport: 3001\n";
const digest = (text) => createHash("sha256").update(text).digest("hex");

async function captureRejection(promise, pattern) {
  let captured;
  await assert.rejects(promise, (error) => { captured = error; return pattern.test(error.message); });
  return captured;
}

test("apply stages a candidate, preserves mode, and does not restart unchanged YAML", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD, { mode: 0o640 });
  let restarts = 0;
  const first = await applyConfig({ target, yamlText: NEXT, env: ENV, restart: async () => { restarts++; }, health: async (cfg) => assert.equal(cfg.port, 3001) });
  assert.equal(first.changed, true);
  assert.equal(restarts, 1);
  assert.equal(await fs.readFile(first.backup, "utf8"), GOOD);
  assert.equal((await fs.stat(target)).mode & 0o777, 0o640);
  const again = await applyConfig({ target, yamlText: NEXT, env: ENV, restart: async () => { restarts++; }, health: async () => {} });
  assert.equal(again.changed, false);
  assert.equal(restarts, 1);
  assert.equal((await applyStatus(target)).lastRollback, null);
});

test("bad candidate is rejected before active config changes", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD);
  await assert.rejects(applyConfig({ target, yamlText: "schema_version: 2\nport: 0\n", env: ENV, restart: async () => { throw new Error("must not restart"); }, health: async () => {} }), /port/);
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
});

test("failed health check rolls back and records failure", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD);
  let restarts = 0;
  await assert.rejects(applyConfig({ target, yamlText: NEXT, env: ENV, restart: async () => { restarts++; }, health: async (cfg) => { if (cfg.port === 3001) throw new Error("unhealthy"); } }), /restored previous config/);
  assert.equal(restarts, 2);
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
  const status = await applyStatus(target);
  assert.match(status.lastRollback.reason, /unhealthy/);
  assert.equal(await fs.readFile(status.lastRollback.failed, "utf8"), NEXT);
});

test("archive failure cannot keep a bad active YAML after a failed health check", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD);
  let restarts = 0;
  const failure = await captureRejection(applyConfig({
    target, yamlText: NEXT, env: ENV,
    restart: async () => { restarts++; },
    health: async (cfg) => {
      if (cfg.port !== 3001) return;
      const pending = JSON.parse(await fs.readFile(`${target}.apply-pending.json`, "utf8"));
      await fs.mkdir(pending.failed); // rename of the staged archive must fail
      throw new Error("unhealthy");
    },
  }), /restored previous config/);
  assert.equal(restarts, 2);
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
  assert.equal(failure.lastRollback.failed, null);
  assert.match(failure.lastRollback.archiveError, /EISDIR|directory/);
  assert.match((await applyStatus(target)).lastRollback.archiveError, /EISDIR|directory/);
  await assert.rejects(fs.stat(`${target}.apply-pending.json`), { code: "ENOENT" });
});

test("status write failure still restores the active YAML and clears the journal", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD);
  await fs.mkdir(`${target}.apply-status.json`);
  const failure = await captureRejection(applyConfig({
    target, yamlText: NEXT, env: ENV, restart: async () => {},
    health: async (cfg) => { if (cfg.port === 3001) throw new Error("unhealthy"); },
  }), /restored previous config/);
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
  assert.match(failure.lastRollback.statusError, /EISDIR|directory/);
  assert.equal(await fs.readFile(failure.lastRollback.failed, "utf8"), NEXT);
  await assert.rejects(fs.stat(`${target}.apply-pending.json`), { code: "ENOENT" });
});

test("a full target write restores through the backup link without consuming it", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD);
  let failRestore = false;
  const realOpen = fs.open;
  t.mock.method(fs, "open", (path, ...args) => {
    if (failRestore && /^\.bridge\.yaml\.[0-9a-f-]{36}\.tmp$/.test(basename(path))) {
      return Promise.reject(Object.assign(new Error("disk full"), { code: "ENOSPC" }));
    }
    return realOpen(path, ...args);
  });
  const failure = await captureRejection(applyConfig({
    target, yamlText: NEXT, env: ENV, restart: async () => {},
    health: async (cfg) => { if (cfg.port === 3001) { failRestore = true; throw new Error("unhealthy"); } },
  }), /restored previous config/);
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
  assert.equal(await fs.readFile(failure.lastRollback.backup, "utf8"), GOOD);
  assert.equal(failure.lastRollback.backupConsumed, undefined);
});

test("a full target write and link failure consume the backup to restore YAML", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD);
  let failRestore = false;
  const realOpen = fs.open, realLink = fs.link;
  t.mock.method(fs, "open", (path, ...args) => {
    if (failRestore && /^\.bridge\.yaml\.[0-9a-f-]{36}\.tmp$/.test(basename(path))) {
      return Promise.reject(Object.assign(new Error("disk full"), { code: "ENOSPC" }));
    }
    return realOpen(path, ...args);
  });
  t.mock.method(fs, "link", (source, path) => {
    if (failRestore && path.endsWith(".restore")) return Promise.reject(Object.assign(new Error("no inode"), { code: "ENOSPC" }));
    return realLink(source, path);
  });
  const failure = await captureRejection(applyConfig({
    target, yamlText: NEXT, env: ENV, restart: async () => {},
    health: async (cfg) => { if (cfg.port === 3001) { failRestore = true; throw new Error("unhealthy"); } },
  }), /restored previous config/);
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
  assert.equal(failure.lastRollback.backup, null);
  assert.equal(failure.lastRollback.backupConsumed, true);
});

test("a permission error does not disguise a failed restore as low disk space", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD);
  let failRestore = false;
  const realOpen = fs.open;
  t.mock.method(fs, "open", (path, ...args) => {
    if (failRestore && /^\.bridge\.yaml\.[0-9a-f-]{36}\.tmp$/.test(basename(path))) {
      return Promise.reject(Object.assign(new Error("permission denied"), { code: "EACCES" }));
    }
    return realOpen(path, ...args);
  });
  const failure = await captureRejection(applyConfig({
    target, yamlText: NEXT, env: ENV, restart: async () => {},
    health: async (cfg) => { if (cfg.port === 3001) { failRestore = true; throw new Error("unhealthy"); } },
  }), /previous config restoration failed: permission denied/);
  assert.equal(failure.lastRollback, undefined);
  assert.equal(await fs.readFile(target, "utf8"), NEXT);
  const pending = JSON.parse(await fs.readFile(`${target}.apply-pending.json`, "utf8"));
  assert.equal(await fs.readFile(pending.backup, "utf8"), GOOD);
});

test("interrupted transaction is restored before the service starts", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  const backup = `${target}.bak-test`;
  const failed = `${target}.failed-test`;
  await fs.writeFile(target, NEXT);
  await fs.writeFile(backup, GOOD);
  await fs.writeFile(`${target}.apply-pending.json`, JSON.stringify({ owner: 99999999, target, backup, failed, mode: 0o600 }));
  await fs.writeFile(`${target}.apply-lock`, "99999999\n");
  const result = await recoverInterruptedApply(target);
  assert.equal(result.recovered, true);
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
  assert.equal(await fs.readFile(failed, "utf8"), NEXT);
  await assert.rejects(fs.stat(`${target}.apply-lock`), { code: "ENOENT" });
  assert.match((await applyStatus(target)).lastRollback.reason, /stopped before health/);
});

test("interrupted recovery restores YAML when archive and status writes fail", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  const backup = `${target}.bak-test`;
  const failed = `${target}.failed-test`;
  await fs.writeFile(target, NEXT);
  await fs.writeFile(backup, GOOD);
  await fs.mkdir(failed);
  await fs.mkdir(`${target}.apply-status.json`);
  await fs.writeFile(`${target}.apply-pending.json`, JSON.stringify({ owner: 99999999, target, backup, failed, mode: 0o600 }));
  const result = await recoverInterruptedApply(target);
  assert.equal(result.recovered, true);
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
  assert.equal(await fs.readFile(backup, "utf8"), GOOD);
  assert.equal(result.lastRollback.failed, null);
  assert.match(result.lastRollback.archiveError, /EISDIR|directory/);
  assert.match(result.lastRollback.statusError, /EISDIR|directory/);
  await assert.rejects(fs.stat(`${target}.apply-pending.json`), { code: "ENOENT" });
});

test("recovery accepts a prior backup consumed to restore YAML without free space", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  const backup = `${target}.bak-test`;
  const failed = `${target}.failed-test`;
  await fs.writeFile(target, GOOD); // previous backup was atomically renamed over the candidate
  await fs.writeFile(failed, NEXT); // prior recovery archived the candidate before it stopped
  await fs.writeFile(`${target}.apply-pending.json`, JSON.stringify({
    owner: 99999999, target, backup, previousSha256: digest(GOOD), failed, mode: 0o600,
  }));
  const result = await recoverInterruptedApply(target);
  assert.equal(result.recovered, true);
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
  assert.equal(result.lastRollback.backupConsumed, true);
  assert.equal(result.lastRollback.failed, failed);
  assert.equal(await fs.readFile(failed, "utf8"), NEXT);
});

test("recover does not interrupt a live applying process", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(`${target}.apply-pending.json`, JSON.stringify({ owner: process.pid, target, failed: `${target}.failed`, mode: 0o600 }));
  assert.deepEqual(await recoverInterruptedApply(target), { recovered: false, inProgress: true });
});

test("recover detects PID reuse with a different Linux process start token", async (t) => {
  try { await fs.readFile("/proc/self/stat"); } catch { t.skip("Linux procfs required"); return; }
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  const backup = `${target}.bak-test`;
  await fs.writeFile(target, NEXT);
  await fs.writeFile(backup, GOOD);
  await fs.writeFile(`${target}.apply-pending.json`, JSON.stringify({ owner: process.pid, ownerStart: "definitely-not-this-process", target, backup, failed: `${target}.failed`, mode: 0o600 }));
  assert.equal((await recoverInterruptedApply(target)).recovered, true);
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
});

test("recover clears a dead pre-journal lock without touching the active config", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD);
  await fs.writeFile(`${target}.apply-lock`, "99999999\n"); // legacy lock format
  assert.deepEqual(await recoverInterruptedApply(target), { recovered: true, staleLock: true });
  assert.equal(await fs.readFile(target, "utf8"), GOOD);
  await assert.rejects(fs.stat(`${target}.apply-lock`), { code: "ENOENT" });
});

test("apply repairs a dead pre-journal lock before changing config", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD);
  await fs.writeFile(`${target}.apply-lock`, JSON.stringify({ owner: 99999999, ownerStart: "old" }));
  const result = await applyConfig({ target, yamlText: NEXT, env: ENV, restart: async () => {}, health: async () => {} });
  assert.equal(result.changed, true);
  assert.equal(await fs.readFile(target, "utf8"), NEXT);
  await assert.rejects(fs.stat(`${target}.apply-lock`), { code: "ENOENT" });
});

test("recover preserves a live pre-journal lock and refuses malformed ownership", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(`${target}.apply-lock`, JSON.stringify({ owner: process.pid }));
  assert.deepEqual(await recoverInterruptedApply(target), { recovered: false, inProgress: true });
  await assert.rejects(applyConfig({ target, yamlText: GOOD, env: ENV, restart: async () => {}, health: async () => {} }), /another config apply/);
  await fs.writeFile(`${target}.apply-lock`, "not-a-pid");
  await assert.rejects(recoverInterruptedApply(target), /invalid config apply lock/);
});

test("recover detects reused PID in a pre-journal lock on Linux", async (t) => {
  try { await fs.readFile("/proc/self/stat"); } catch { t.skip("Linux procfs required"); return; }
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(`${target}.apply-lock`, JSON.stringify({ owner: process.pid, ownerStart: "not-this-start" }));
  assert.deepEqual(await recoverInterruptedApply(target), { recovered: true, staleLock: true });
});

test("concurrent apply refuses to overwrite the active transaction", async (t) => {
  const dir = await fs.mkdtemp(join(os.tmpdir(), "ewb-apply-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const target = join(dir, "bridge.yaml");
  await fs.writeFile(target, GOOD);
  let release;
  const gate = new Promise((r) => release = r);
  const first = applyConfig({ target, yamlText: NEXT, env: ENV, restart: async () => gate, health: async () => {} });
  for (let i = 0; i < 100; i++) { try { await fs.stat(`${target}.apply-lock`); break; } catch { await new Promise((r) => setTimeout(r, 1)); } }
  await assert.rejects(applyConfig({ target, yamlText: GOOD, env: ENV, restart: async () => {}, health: async () => {} }), /another config apply/);
  release();
  await first;
});
