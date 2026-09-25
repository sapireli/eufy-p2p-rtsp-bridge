import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, readFile, chmod, rm } from "node:fs/promises";
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

test("setup rejects malformed answer files before touching config", async (t) => {
  const dir = await mkdtemp(join(tmpdir(), "ewb-answers-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const answers = join(dir, "answers.yaml");
  await writeFile(answers, "probe_streams: yes\n");
  const result = await run(["setup", "--answers", answers, "--json"], { env: { BRIDGE_CONFIG: join(dir, "bridge.yaml") } });
  assert.equal(result.code, 1);
  assert.match(JSON.parse(result.err).error, /probe_streams/);
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

test("headless setup restarts on loopback before applying the client listener", async (t) => {
  const srv = http.createServer((req, res) => {
    res.setHeader("content-type", "application/json");
    if (req.url === "/healthz") return res.end(JSON.stringify({ ok: true, auth: { state: "ok" } }));
    if (req.url === "/auth/status") return res.end(JSON.stringify({ state: "ok" }));
    if (req.url === "/api/cameras") return res.end("[]");
    res.statusCode = 404; res.end("{}");
  });
  await new Promise((resolve) => srv.listen(0, "127.0.0.1", resolve));
  t.after(() => srv.close());
  const dir = await mkdtemp(join(tmpdir(), "ewb-setup-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const config = join(dir, "bridge.yaml");
  const environment = join(dir, "bridge.env");
  const answers = join(dir, "answers.yaml");
  const restartLog = join(dir, "restarts.log");
  await writeFile(answers, `email: wall@example.com\npassword: test-password\nport: ${srv.address().port}\nlan_cidr: null\nlan_force: false\nhost: 0.0.0.0\nclient_host: bridge.example\nprobe_streams: false\n`, { mode: 0o600 });
  const systemctl = join(dir, "systemctl");
  await writeFile(systemctl, "#!/bin/sh\nif [ \"$1\" = is-enabled ]; then exit 1; fi\nprintf '%s ' \"$1\" >> \"$RESTART_LOG\"\nawk '/^host:/ { print $2 }' \"$BRIDGE_CONFIG\" >> \"$RESTART_LOG\"\nexit 0\n", { mode: 0o755 });
  await chmod(systemctl, 0o755);
  const result = await run(["setup", "--answers", answers, "--json"], { env: { BRIDGE_CONFIG: config, BRIDGE_ENV: environment, RESTART_LOG: restartLog, PATH: `${dir}:${process.env.PATH}` } });
  assert.equal(result.code, 0, result.err);
  const report = JSON.parse(result.out);
  assert.equal(report.bridgeUrl, `http://bridge.example:${srv.address().port}`);
  assert.equal(report.lanPolicy.lanCidr, null);
  assert.equal(report.serviceEnabled, true);
  assert.deepEqual((await readFile(restartLog, "utf8")).trim().split("\n"), ["restart 127.0.0.1", "restart 0.0.0.0", "enable 0.0.0.0"]);
  assert.match(await readFile(config, "utf8"), /^host: 0\.0\.0\.0$/m);
  assert.doesNotMatch(result.out + result.err, /test-password/);
});

test("a failed live probe rolls back setup without enabling the service", async (t) => {
  const srv = http.createServer((req, res) => {
    res.setHeader("content-type", "application/json");
    if (req.url === "/healthz") return res.end(JSON.stringify({ ok: true, auth: { state: "ok" } }));
    if (req.url === "/auth/status") return res.end(JSON.stringify({ state: "ok" }));
    if (req.url === "/api/cameras") return res.end(JSON.stringify([{ sn: "A", name: "Door", enabled: true, mode: "always", powered: true, streamKey: null }]));
    res.statusCode = 404; res.end("{}");
  });
  await new Promise((resolve) => srv.listen(0, "127.0.0.1", resolve));
  t.after(() => srv.close());
  const dir = await mkdtemp(join(tmpdir(), "ewb-setup-fail-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const config = join(dir, "bridge.yaml"), environment = join(dir, "bridge.env"), answers = join(dir, "answers.yaml"), restartLog = join(dir, "actions.log");
  await writeFile(answers, `email: wall@example.com\npassword: test-password\nport: ${srv.address().port}\nlan_force: false\nhost: 0.0.0.0\nclient_host: bridge.example\nprobe_streams: true\n`, { mode: 0o600 });
  const systemctl = join(dir, "systemctl");
  await writeFile(systemctl, "#!/bin/sh\nif [ \"$1\" = is-enabled ]; then exit 1; fi\nprintf '%s ' \"$1\" >> \"$RESTART_LOG\"\nif [ -f \"$BRIDGE_CONFIG\" ]; then awk '/^host:/ { print $2 }' \"$BRIDGE_CONFIG\" >> \"$RESTART_LOG\"; else echo missing >> \"$RESTART_LOG\"; fi\nexit 0\n", { mode: 0o755 });
  await chmod(systemctl, 0o755);
  const result = await run(["setup", "--answers", answers, "--json"], { env: { BRIDGE_CONFIG: config, BRIDGE_ENV: environment, RESTART_LOG: restartLog, PATH: `${dir}:${process.env.PATH}` } });
  assert.equal(result.code, 1);
  assert.match(JSON.parse(result.err).error, /no RTSP stream key/);
  assert.deepEqual((await readFile(restartLog, "utf8")).trim().split("\n"), ["restart 127.0.0.1", "restart 0.0.0.0", "restart missing", "disable missing"]);
  await assert.rejects(readFile(config), { code: "ENOENT" });
  await assert.rejects(readFile(environment), { code: "ENOENT" });
});
