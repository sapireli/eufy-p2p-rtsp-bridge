import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import http from "node:http";

const cli = new URL("../cli.mjs", import.meta.url).pathname;
function run(args, { input = "", env = {} } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [cli, ...args], { env: { ...process.env, EUFY_EMAIL: "test@example.com", EUFY_PASSWORD: "secret", ...env } });
    let out = "", err = "";
    child.stdout.on("data", (x) => out += x);
    child.stderr.on("data", (x) => err += x);
    child.on("error", reject);
    child.on("close", (code) => resolve({ code, out, err }));
    child.stdin.end(input);
  });
}

test("validate accepts file and stdin with the same strict schema", async (t) => {
  const dir = await mkdtemp(join(tmpdir(), "ewb-cli-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const file = join(dir, "candidate.yaml");
  await writeFile(file, "schema_version: 2\nport: 3000\n");
  for (const source of [file, "-"]) {
    const result = await run(["config", "validate", source, "--json"], { input: "schema_version: 2\nport: 3000\n" });
    assert.equal(result.code, 0, result.err);
    assert.equal(JSON.parse(result.out).schemaVersion, 2);
  }
  const bad = await run(["config", "validate", "-", "--json"], { input: "schema_version: 2\nsecret_typo: value\n" });
  assert.equal(bad.code, 1);
  assert.match(JSON.parse(bad.err).error, /secret_typo/);
});

test("example is nonsecret and can be validated with environment credentials", async () => {
  const example = await run(["config", "example"]);
  assert.equal(example.code, 0);
  assert.doesNotMatch(example.out, /password: change-me/);
  const valid = await run(["config", "validate", "-", "--json"], { input: example.out });
  assert.equal(valid.code, 0, valid.err);
});

test("headless setup requires an answer file and never starts a service", async () => {
  const result = await run(["setup", "--json"]);
  assert.equal(result.code, 1);
  assert.match(JSON.parse(result.err).error, /terminal or --answers/);
});

test("status redacts captcha data; doctor reports bad host dependencies", async (t) => {
  const srv = http.createServer((req, res) => {
    res.setHeader("content-type", "application/json");
    res.end(JSON.stringify({ ok: true, auth: { state: "require_captcha", image: "SECRET_IMAGE_BYTES" }, cameras: 2, go2rtc: "stopped" }));
  });
  await new Promise((resolve) => srv.listen(0, "127.0.0.1", resolve));
  t.after(() => srv.close());
  const dir = await mkdtemp(join(tmpdir(), "ewb-cli-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const file = join(dir, "bridge.yaml");
  await writeFile(file, `schema_version: 2\nport: ${srv.address().port}\ngo2rtc_bin: /missing/go2rtc\n`);
  const status = await run(["status", "--json"], { env: { BRIDGE_CONFIG: file } });
  assert.equal(status.code, 0, status.err);
  assert.equal(JSON.parse(status.out).live.auth.state, "require_captcha");
  assert.doesNotMatch(status.out, /SECRET_IMAGE_BYTES/);
  const doctor = await run(["doctor", "--json"], { env: { BRIDGE_CONFIG: file } });
  assert.equal(doctor.code, 1);
  const checks = JSON.parse(doctor.out).checks;
  assert.ok(checks.some((c) => c.code === "GO2RTC" && !c.ok));
  assert.ok(checks.some((c) => c.code === "HEALTH" && !c.ok));
});

test("status reaches a bridge bound to a specific non-default address", async (t) => {
  const srv = http.createServer((_, res) => {
    res.setHeader("content-type", "application/json");
    res.end(JSON.stringify({ ok: true, auth: { state: "ok" }, cameras: 1, go2rtc: "running" }));
  });
  try { await new Promise((resolve, reject) => srv.listen(0, "127.0.0.2", resolve).once("error", reject)); }
  catch { t.skip("127.0.0.2 is not available"); return; }
  t.after(() => srv.close());
  const dir = await mkdtemp(join(tmpdir(), "ewb-cli-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const file = join(dir, "bridge.yaml");
  await writeFile(file, `schema_version: 2\nhost: 127.0.0.2\nport: ${srv.address().port}\n`);
  const status = await run(["status", "--json"], { env: { BRIDGE_CONFIG: file } });
  assert.equal(status.code, 0, status.err);
  assert.equal(JSON.parse(status.out).live.auth.state, "ok");
});
