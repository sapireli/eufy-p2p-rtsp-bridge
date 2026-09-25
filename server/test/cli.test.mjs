import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, readFile, chmod, rm, readdir, stat } from "node:fs/promises";
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
  const invalid = JSON.parse(bad.err);
  assert.match(invalid.error, /secret_typo/);
  assert.deepEqual(invalid.diagnostics.map((d) => [d.code, d.severity, d.path]), [["CONFIG_UNSUPPORTED_KEY", "error", "secret_typo"]]);
  assert.match(invalid.diagnostics[0].remedy, /config example/);
  const syntax = await run(["config", "validate", "-", "--json"], { input: "eufy: { password: INLINE-SECRET, email: user@example.com\n" });
  assert.equal(syntax.code, 1);
  assert.equal(JSON.parse(syntax.err).diagnostics[0].code, "CONFIG_YAML_INVALID");
  assert.doesNotMatch(syntax.out + syntax.err, /INLINE-SECRET/);
  const apply = await run(["config", "apply", "-", "--json"], { input: "schema_version: 2\nport: 99999\n", env: { BRIDGE_CONFIG: file } });
  assert.equal(apply.code, 1);
  assert.deepEqual(JSON.parse(apply.err).diagnostics.map((d) => [d.code, d.path]), [["CONFIG_INVALID", "port"]]);
  assert.match(JSON.parse(apply.err).diagnostics[0].remedy, /config explain port/);
  assert.equal(await readFile(file, "utf8"), "schema_version: 2\nport: 3000\n");
});

test("mistyped config commands fail before touching the active file", async (t) => {
  const dir = await mkdtemp(join(tmpdir(), "ewb-cli-args-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const active = join(dir, "active.yaml"), candidate = join(dir, "candidate.yaml");
  const original = "schema_version: 2\nport: 3000\n";
  await writeFile(active, original);
  await writeFile(candidate, "schema_version: 2\nport: 3001\n");
  const env = { BRIDGE_CONFIG: active };
  for (const args of [
    ["config", "apply", candidate, "--output", join(dir, "unexpected.yaml")],
    ["config", "apply", candidate, "extra"],
    ["config", "validate", candidate, "extra"],
    ["config", "migrate", candidate, "--bogus"],
    ["config", "example", "extra"],
    ["config", "recover", "extra"],
    ["setup", "--answers", candidate, "extra"],
    ["inventory", "export", join(dir, "inventory.json"), "--bogus", "host"],
  ]) {
    const result = await run([...args, "--json"], { env });
    assert.equal(result.code, 1, `${args.join(" ")}: ${result.err}`);
    const error = JSON.parse(result.err);
    assert.match(error.error, /usage:/, args.join(" "));
    if (args[0] === "config") {
      assert.equal(error.code, "CLI_USAGE");
      assert.match(error.diagnostics[0].remedy, /command syntax/);
    }
    assert.equal(await readFile(active, "utf8"), original);
  }
  assert.deepEqual((await readdir(dir)).sort(), ["active.yaml", "candidate.yaml"]);
});

test("file and stdin config reads reject oversized YAML before parsing", async (t) => {
  const dir = await mkdtemp(join(tmpdir(), "ewb-cli-limit-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const file = join(dir, "oversized.yaml");
  const oversized = `#${"x".repeat(1024 * 1024)}\n`;
  await writeFile(file, oversized);
  for (const source of [file, "-"]) {
    const result = await run(["config", "validate", source, "--json"], { input: source === "-" ? oversized : "" });
    assert.equal(result.code, 1);
    assert.match(JSON.parse(result.err).error, /config exceeds 1 MiB/);
  }
});

test("example is nonsecret and can be validated with environment credentials", async () => {
  const example = await run(["config", "example"]);
  assert.equal(example.code, 0);
  assert.doesNotMatch(example.out, /password: change-me/);
  const structured = await run(["config", "example", "--json"]);
  assert.equal(structured.code, 0, structured.err);
  assert.deepEqual(JSON.parse(structured.out), { ok: true, yaml: example.out });
  const valid = await run(["config", "validate", "-", "--json"], { input: example.out });
  assert.equal(valid.code, 0, valid.err);
});

test("migration produces a reviewable candidate and path diff without touching active config", async (t) => {
  const dir = await mkdtemp(join(tmpdir(), "ewb-migrate-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const active = join(dir, "active.yaml"), output = join(dir, "candidate.yaml");
  const old = "schema_version: 2\nport: 3000\n";
  await writeFile(active, old);
  const source = "schema_version: 1\nport: 3001\ncameras: { DOOR: { mode: on_motion } }\n";
  const report = await run(["config", "migrate", "-", "--json"], { input: source, env: { BRIDGE_CONFIG: active } });
  assert.equal(report.code, 0, report.err);
  assert.equal(JSON.parse(report.out).lossless, true);
  assert.match(JSON.parse(report.out).candidateYaml, /^schema_version: 2/m);
  assert.deepEqual(JSON.parse(report.out).diff.changed, ["port"]);
  assert.equal(await readFile(active, "utf8"), old);
  const written = await run(["config", "migrate", "-", "--output", output, "--json"], { input: source, env: { BRIDGE_CONFIG: active } });
  assert.equal(written.code, 0, written.err);
  assert.equal(JSON.parse(written.out).candidateYaml, undefined);
  assert.match(await readFile(output, "utf8"), /schema_version: 2/);
  assert.equal((await stat(output)).mode & 0o777, 0o600);
  const refused = await run(["config", "migrate", "-", "--output", active, "--json"], { input: source, env: { BRIDGE_CONFIG: active } });
  assert.equal(refused.code, 1);
  assert.match(JSON.parse(refused.err).error, /active config/);
  assert.equal(await readFile(active, "utf8"), old);
});

test("migration reports lossy fields and keeps inline passwords out of CLI output", async (t) => {
  const dir = await mkdtemp(join(tmpdir(), "ewb-migrate-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const output = join(dir, "candidate.yaml");
  const source = "eufy: { email: wall@example.com, password: private-password }\nunknown: value\n";
  const blocked = await run(["config", "migrate", "-", "--json"], { input: source, env: { BRIDGE_CONFIG: join(dir, "active.yaml") } });
  assert.equal(blocked.code, 1);
  assert.match(JSON.parse(blocked.err).error, /--output/);
  assert.doesNotMatch(blocked.out + blocked.err, /private-password/);
  const migrated = await run(["config", "migrate", "-", "--output", output, "--json"], { input: source, env: { BRIDGE_CONFIG: join(dir, "active.yaml") } });
  assert.equal(migrated.code, 2);
  assert.deepEqual(JSON.parse(migrated.out).unsupportedPaths, ["unknown"]);
  assert.equal(JSON.parse(migrated.out).lossless, false);
  assert.doesNotMatch(migrated.out + migrated.err, /private-password/);
  assert.match(await readFile(output, "utf8"), /private-password/);
  assert.equal((await stat(output)).mode & 0o777, 0o600);
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
  const config = join(dir, "bridge.yaml");
  for (const [text, error] of [
    ["probe_streams: yes\n", /probe_streams/],
    ["lan_frce: true\n", /unknown setup answer: lan_frce/],
    ["lan_force: 1\n", /lan_force/],
    ["port: 99999\n", /port/],
    ["cameras: [DOOR]\n", /cameras/],
    [`#${"x".repeat(1024 * 1024)}\n`, /setup answers exceeds 1 MiB/],
  ]) {
    await writeFile(answers, text);
    const result = await run(["setup", "--answers", answers, "--json"], { env: { BRIDGE_CONFIG: config } });
    assert.equal(result.code, 1);
    assert.match(JSON.parse(result.err).error, error);
    await assert.rejects(readFile(config), { code: "ENOENT" });
  }
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
  assert.ok(JSON.parse(doctor.out).diagnostics.every((d) => d.severity === "error" && d.remedy));
});

test("doctor finds an executable go2rtc on PATH", async (t) => {
  const dir = await mkdtemp(join(tmpdir(), "ewb-doctor-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const binary = join(dir, "fake-go2rtc"), config = join(dir, "bridge.yaml");
  await writeFile(binary, "#!/bin/sh\nexit 0\n", { mode: 0o755 });
  await chmod(binary, 0o755);
  await writeFile(config, "schema_version: 2\ngo2rtc_bin: fake-go2rtc\n");
  const doctor = await run(["doctor", "--json"], { env: { BRIDGE_CONFIG: config, PATH: `${dir}:${process.env.PATH}` } });
  const check = JSON.parse(doctor.out).checks.find((c) => c.code === "GO2RTC");
  assert.equal(check.ok, true);
  assert.equal(check.detail, binary);
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
  const oldSecrets = 'EUFY_EMAIL="old@example.com"\nEUFY_PASSWORD="old-password"\n';
  await writeFile(environment, oldSecrets, { mode: 0o644 });
  await writeFile(answers, `email: wall@example.com\npassword: test-password\nport: ${srv.address().port}\nlan_force: false\nhost: 0.0.0.0\nclient_host: bridge.example\nprobe_streams: true\n`, { mode: 0o600 });
  const systemctl = join(dir, "systemctl");
  await writeFile(systemctl, "#!/bin/sh\nif [ \"$1\" = is-enabled ]; then exit 1; fi\nprintf '%s ' \"$1\" >> \"$RESTART_LOG\"\nif [ -f \"$BRIDGE_CONFIG\" ]; then awk '/^host:/ { print $2 }' \"$BRIDGE_CONFIG\" >> \"$RESTART_LOG\"; else echo missing >> \"$RESTART_LOG\"; fi\nexit 0\n", { mode: 0o755 });
  await chmod(systemctl, 0o755);
  const result = await run(["setup", "--answers", answers, "--json"], { env: { BRIDGE_CONFIG: config, BRIDGE_ENV: environment, RESTART_LOG: restartLog, PATH: `${dir}:${process.env.PATH}` } });
  assert.equal(result.code, 1);
  assert.match(JSON.parse(result.err).error, /no RTSP stream key/);
  assert.deepEqual((await readFile(restartLog, "utf8")).trim().split("\n"), ["restart 127.0.0.1", "restart 0.0.0.0", "restart missing", "disable missing"]);
  await assert.rejects(readFile(config), { code: "ENOENT" });
  assert.equal(await readFile(environment, "utf8"), oldSecrets);
  assert.equal((await stat(environment)).mode & 0o777, 0o600);
  const backups = (await readdir(dir)).filter((name) => name.startsWith("bridge.env.bak-"));
  assert.equal(backups.length, 1);
  assert.equal((await stat(join(dir, backups[0]))).mode & 0o777, 0o600);
});

test("inventory refuses a camera without a stream key instead of exporting /null", async (t) => {
  const srv = http.createServer((req, res) => {
    res.setHeader("content-type", "application/json");
    res.end(JSON.stringify([{ sn: "DOOR", name: "Door", enabled: true, streamKey: null }]));
  });
  await new Promise((resolve) => srv.listen(0, "127.0.0.1", resolve));
  t.after(() => srv.close());
  const dir = await mkdtemp(join(tmpdir(), "ewb-inventory-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const config = join(dir, "bridge.yaml"), output = join(dir, "cameras.json");
  await writeFile(config, `schema_version: 2\nhost: 127.0.0.1\nport: ${srv.address().port}\n`);
  const result = await run(["inventory", "export", output, "--host", "bridge.example", "--json"], { env: { BRIDGE_CONFIG: config } });
  assert.equal(result.code, 1);
  assert.match(JSON.parse(result.err).error, /DOOR has no RTSP stream key/);
  await assert.rejects(readFile(output), { code: "ENOENT" });
});
