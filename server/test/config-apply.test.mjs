import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import { join } from "node:path";
import { applyConfig, applyStatus, recoverInterruptedApply } from "../src/config-apply.mjs";

const ENV = { EUFY_EMAIL: "e", EUFY_PASSWORD: "p" };
const GOOD = "schema_version: 2\nport: 3000\n";
const NEXT = "schema_version: 2\nport: 3001\n";

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
