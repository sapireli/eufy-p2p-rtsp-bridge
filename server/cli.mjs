#!/usr/bin/env node
// Local setup and configuration command for the Node bridge. It never accepts network config writes.
import fs from "node:fs/promises";
import { existsSync, readFileSync } from "node:fs";
import os from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { createInterface } from "node:readline/promises";
import { stdin, stdout, stderr } from "node:process";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { parse, stringify } from "yaml";
import { loadConfig } from "./src/config.mjs";
import { applyConfig, applyStatus, recoverInterruptedApply } from "./src/config-apply.mjs";
import { chooseCameraPolicies } from "./src/setup-cameras.mjs";
import { bridgeBase } from "./src/cli-host.mjs";
import { validateSetupNetwork, bootstrapConfig, localProbeHost, probeCameraRtsp } from "./src/setup-network.mjs";

const exec = promisify(execFile);
const __dirname = dirname(fileURLToPath(import.meta.url));
const service = "eufy-wall-bridge.service";
const target = process.env.BRIDGE_CONFIG || (existsSync("/etc/eufy-wall-bridge.yaml") ? "/etc/eufy-wall-bridge.yaml" : "./config.yaml");
const envPath = process.env.BRIDGE_ENV || (existsSync("/etc/eufy-wall-bridge.env") || target.startsWith("/etc/") ? "/etc/eufy-wall-bridge.env" : "./local.env");
if (existsSync(envPath)) process.loadEnvFile(envPath);

function emit(data, asJson) {
  stdout.write(asJson ? `${JSON.stringify(data, null, 2)}\n` : `${typeof data === "string" ? data : JSON.stringify(data, null, 2)}\n`);
}
function fail(message, asJson, code = "ERROR") {
  stderr.write(asJson ? `${JSON.stringify({ ok: false, code, error: message })}\n` : `eufy-bridge: ${message}\n`);
  process.exitCode = 1;
}
async function input(path) {
  if (path === "-") {
    let body = "";
    for await (const chunk of stdin) {
      body += chunk;
      if (Buffer.byteLength(body) > 1024 * 1024) throw new Error("config exceeds 1 MiB");
    }
    return body;
  }
  if (!path) throw new Error("provide a YAML file or '-' for stdin");
  return fs.readFile(path, "utf8");
}
async function restart() { await exec("systemctl", ["restart", service], { timeout: 30_000 }); }
async function enableService() { await exec("systemctl", ["enable", service], { timeout: 30_000 }); }
async function isServiceEnabled() {
  try { await exec("systemctl", ["is-enabled", "--quiet", service], { timeout: 5_000 }); return true; }
  catch { return false; }
}
async function health(cfg, { allowChallenge = false } = {}) {
  const base = bridgeBase(cfg);
  const deadline = Date.now() + 30_000;
  let last = "bridge did not respond";
  do {
    try {
      const res = await fetch(`${base}/healthz`, { signal: AbortSignal.timeout(1500) });
      const body = await res.json();
      if (res.ok && body.ok && (body.auth?.state === "ok" || (allowChallenge && ["pending", "require_2fa", "require_captcha"].includes(body.auth?.state)))) return body;
      last = `bridge auth state ${body.auth?.state ?? "unknown"}`;
    } catch (error) { last = error.message; }
    await new Promise((r) => setTimeout(r, 750));
  } while (Date.now() < deadline);
  throw new Error(`bridge health check failed: ${last}`);
}
async function local(path, options = {}) {
  const cfg = loadConfig().cfg;
  const response = await fetch(`${bridgeBase(cfg)}${path}`, { signal: AbortSignal.timeout(5000), ...options });
  if (!response.ok) throw new Error(`${path}: HTTP ${response.status} ${await response.text()}`);
  return response.json();
}
function interfaces() {
  return Object.entries(os.networkInterfaces()).flatMap(([name, entries]) => entries.filter((e) => e.family === "IPv4" && !e.internal).map((e) => ({ name, address: e.address, cidr: e.cidr })));
}
async function question(rl, label, fallback) {
  const answer = (await rl.question(`${label}${fallback != null ? ` [${fallback}]` : ""}: `)).trim();
  return answer || fallback;
}
async function secret(label) {
  if (!stdin.isTTY || !stdin.setRawMode) throw new Error(`${label} requires a terminal or an environment variable`);
  stdout.write(`${label}: `);
  stdin.setRawMode(true);
  stdin.resume();
  return new Promise((resolve, reject) => {
    let value = "";
    const onData = (buf) => {
      for (const ch of buf.toString()) {
        if (ch === "\r" || ch === "\n") { cleanup(); stdout.write("\n"); resolve(value); return; }
        if (ch === "\u0003") { cleanup(); stdout.write("\n"); reject(new Error("interrupted")); return; }
        if (ch === "\u007f" || ch === "\b") { if (value) { value = value.slice(0, -1); stdout.write("\b \b"); } }
        else { value += ch; stdout.write("*"); }
      }
    };
    const cleanup = () => { stdin.off("data", onData); stdin.setRawMode(false); stdin.pause(); };
    stdin.on("data", onData);
  });
}
function envLine(key, value) {
  if (/[\r\n\0]/.test(value)) throw new Error(`${key} contains a newline or NUL`);
  return `${key}=${JSON.stringify(value)}`;
}
async function saveSecrets({ email, password, country }) {
  const oldEnv = Object.fromEntries(["EUFY_EMAIL", "EUFY_PASSWORD", "EUFY_COUNTRY"].map((key) => [key, process.env[key]]));
  const current = existsSync(envPath) ? await fs.readFile(envPath) : null;
  const previousMode = current ? (await fs.stat(envPath)).mode & 0o777 : 0o600;
  const stage = `${envPath}.new-${process.pid}`;
  await fs.writeFile(stage, [envLine("EUFY_EMAIL", email), envLine("EUFY_PASSWORD", password), envLine("EUFY_COUNTRY", country), ""].join("\n"), { mode: 0o600, flag: "wx" });
  await fs.chmod(stage, 0o600);
  if (current) await fs.writeFile(`${envPath}.bak-${Date.now()}`, current, { mode: previousMode, flag: "wx" });
  await fs.rename(stage, envPath);
  process.env.EUFY_EMAIL = email; process.env.EUFY_PASSWORD = password; process.env.EUFY_COUNTRY = country;
  return { current, previousMode, oldEnv };
}
async function restoreSecrets(saved) {
  if (saved.current) { await fs.writeFile(envPath, saved.current, { mode: saved.previousMode }); await fs.chmod(envPath, saved.previousMode); }
  else await fs.unlink(envPath).catch(() => {});
  for (const [key, value] of Object.entries(saved.oldEnv)) {
    if (value === undefined) delete process.env[key]; else process.env[key] = value;
  }
}
async function answerChallenge(rl, base, state, answers) {
  let prompt = rl;
  let auth = state;
  let attempts = 0;
  const deadline = Date.now() + 120_000;
  while (auth.state !== "ok" && Date.now() < deadline && attempts < 5) {
    if (auth.state === "pending") { await new Promise((r) => setTimeout(r, 1000)); auth = await local("/auth/status"); continue; }
    if (auth.state !== "require_2fa" && auth.state !== "require_captcha") throw new Error(`authentication stopped in state ${auth.state}`);
    attempts++;
    if (auth.state === "require_captcha") {
      const image = await fetch(`${base}/auth/captcha`);
      if (image.ok) {
        const path = join(os.tmpdir(), `eufy-captcha-${process.pid}.${image.headers.get("content-type") === "image/png" ? "png" : "jpg"}`);
        await fs.writeFile(path, Buffer.from(await image.arrayBuffer()), { mode: 0o600 });
        emit(`Captcha image: ${path}`);
      }
    }
    let code = auth.state === "require_2fa" ? answers.tfa_code : answers.captcha_code;
    if (!code) {
      prompt.close();
      code = await secret(auth.state === "require_2fa" ? `2FA code (${auth.method ?? "app"})` : "Captcha answer");
      prompt = createInterface({ input: stdin, output: stdout });
    }
    const path = auth.state === "require_2fa" ? "/auth/tfa" : "/auth/captcha";
    const response = await fetch(`${base}${path}`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ code }), signal: AbortSignal.timeout(30_000) });
    const data = await response.json();
    if (!response.ok) { emit(`${path} failed: ${data.error ?? response.status}`); auth = data.auth ?? await local("/auth/status"); continue; }
    auth = data;
  }
  if (auth.state !== "ok") throw new Error(`authentication did not complete (${auth.state})`);
  return prompt;
}
async function setup(args, asJson) {
  const answerIndex = args.indexOf("--answers");
  if (answerIndex >= 0 && !args[answerIndex + 1]) throw new Error("--answers requires a YAML file");
  const answers = answerIndex >= 0 ? parse(await fs.readFile(args[answerIndex + 1], "utf8")) ?? {} : {};
  if (!answers || typeof answers !== "object" || Array.isArray(answers)) throw new Error("setup answers must be a YAML mapping");
  if (answers.probe_streams != null && typeof answers.probe_streams !== "boolean" && (!Array.isArray(answers.probe_streams) || answers.probe_streams.some((sn) => typeof sn !== "string")))
    throw new Error("probe_streams must be true, false, or a list of camera serials");
  const interactive = stdin.isTTY && stdout.isTTY;
  if (!interactive && answerIndex < 0) throw new Error("setup needs a terminal or --answers <file>");
  let rl = createInterface({ input: stdin, output: stdout });
  try {
    const nics = interfaces();
    if (interactive) emit(`Detected LAN: ${nics.map((n) => `${n.name} ${n.cidr}`).join(", ") || "none"}`);
    const email = answers.email ?? process.env.EUFY_EMAIL ?? (interactive ? await question(rl, "Dedicated Eufy account email") : null);
    const country = answers.country ?? process.env.EUFY_COUNTRY ?? (interactive ? await question(rl, "Country", "US") : "US");
    let password = answers.password ?? process.env.EUFY_PASSWORD;
    if (!password && interactive) { rl.close(); password = await secret("Eufy password"); rl = createInterface({ input: stdin, output: stdout }); }
    if (!email || !password) throw new Error("email and password are required; set EUFY_PASSWORD for noninteractive setup");
    const lanCidr = Object.hasOwn(answers, "lan_cidr") ? answers.lan_cidr : (interactive ? await question(rl, "LAN CIDR", nics[0]?.cidr) : nics[0]?.cidr);
    const forceAnswer = answers.lan_force ?? (interactive ? await question(rl, "Require LAN-only camera peers? (yes/no)", lanCidr ? "yes" : "no") : Boolean(lanCidr));
    if (typeof forceAnswer === "string" && !["yes", "no"].includes(forceAnswer.toLowerCase())) throw new Error("LAN-only choice must be yes or no");
    const force = typeof forceAnswer === "string" ? forceAnswer.toLowerCase() === "yes" : forceAnswer;
    const port = answers.port ?? 3000;
    const host = answers.host ?? (interactive ? await question(rl, "Bridge bind address for clients", nics[0]?.address ?? "127.0.0.1") : nics[0]?.address ?? "127.0.0.1");
    const publicHost = answers.client_host ?? (interactive ? await question(rl, "Address clients should use", host === "0.0.0.0" ? nics[0]?.address : host) : host === "0.0.0.0" ? nics[0]?.address : host);
    const lanPolicy = validateSetupNetwork({ lanCidr, force, host, publicHost, interfaces: nics });
    const raw = { schema_version: 2, host, port, lan: { cidr: lanCidr ?? null, force }, cameras: answers.cameras ?? {} };
    const yamlText = stringify(raw);
    const bootstrapText = stringify(bootstrapConfig(raw));
    loadConfig({ env: { EUFY_EMAIL: email, EUFY_PASSWORD: password, EUFY_COUNTRY: country }, rawText: yamlText });
    if (interactive) {
      emit(`Config: ${target}\nSecrets: ${envPath}\nLogin listener: 127.0.0.1:${port}\nClient listener after login: ${host}:${port}\nClient address: ${publicHost}\nCamera LAN: ${lanCidr ?? "not pinned"} (force=${force})`);
      if ((await question(rl, "Apply these settings? (yes/no)", "no")).toLowerCase() !== "yes") throw new Error("setup cancelled");
    }
    const originalYaml = existsSync(target) ? await fs.readFile(target, "utf8") : null;
    const enabledBefore = await isServiceEnabled();
    const saved = await saveSecrets({ email, password, country });
    let applied;
    try {
      applied = await applyConfig({ target, yamlText: bootstrapText, restart, health: (cfg) => health(cfg, { allowChallenge: true }) });
      if (!applied.changed) { await restart(); await health(loadConfig().cfg, { allowChallenge: true }); }
      const base = bridgeBase({ host: "127.0.0.1", port });
      rl = await answerChallenge(rl, base, (await local("/auth/status")), answers);
      let cams = await local("/api/cameras");
      if (interactive) {
        raw.cameras = await chooseCameraPolicies(cams, raw.cameras, { question: (label, fallback) => question(rl, label, fallback), emit });
        emit(`Camera policy:\n${stringify({ cameras: raw.cameras })}`);
        if ((await question(rl, "Apply camera choices? (yes/no)", "no")).toLowerCase() !== "yes") throw new Error("camera choices cancelled");
      }
      const finalApply = await applyConfig({ target, yamlText: stringify(raw), restart, health });
      cams = await local("/api/cameras");
      const publicBridgeUrl = `http://${publicHost}:${port}`;
      const probes = [];
      for (const cam of cams.filter((c) => c.enabled)) {
        const selection = answers.probe_streams;
        const selected = Array.isArray(selection) ? selection.includes(cam.sn) : typeof selection === "boolean" ? selection : interactive ? (await question(rl, `Probe RTSP for ${cam.name} (${cam.sn})? (yes/no)`, cam.powered ? "yes" : "no")).toLowerCase() === "yes" : false;
        if (!selected) continue;
        emit(`Probing RTSP path and live bytes for ${cam.name} (${cam.sn}) for up to 12 seconds...`);
        const result = await probeCameraRtsp(cam, { bridgeUrl: bridgeBase(raw), host: localProbeHost(host, nics) });
        probes.push({ sn: cam.sn, ...result });
      }
      await enableService();
      const inventory = cams.map((c) => ({ sn: c.sn, name: c.name, mode: c.mode, powered: c.powered, codec: c.codec, streamKey: c.streamKey, rtsp: `rtsp://${publicHost}:8554/${encodeURIComponent(c.streamKey)}` }));
      emit({ ok: true, serviceEnabled: true, applied: finalApply, bootstrapApply: applied, bridgeUrl: publicBridgeUrl, lanPolicy, cameras: inventory, probes, inventoryCommand: `eufy-bridge inventory export cameras.json --host ${publicHost}` }, asJson);
    } catch (error) {
      await restoreSecrets(saved);
      if (originalYaml != null) {
        await applyConfig({ target, yamlText: originalYaml, restart, health: async () => {} }).catch(() => {});
        await restart().catch(() => {});
      } else { await fs.unlink(target).catch(() => {}); await restart().catch(() => {}); }
      if (!enabledBefore) await exec("systemctl", ["disable", service], { timeout: 30_000 }).catch(() => {});
      throw error;
    }
  } finally { rl.close(); }
}

async function main() {
  const args = process.argv.slice(2);
  const asJson = args.includes("--json");
  const clean = args.filter((a) => a !== "--json");
  const [verb, sub, path] = clean;
  if (verb === "config" && sub === "example") return emit(await fs.readFile(join(__dirname, "config.example.yaml"), "utf8"), false);
  if (verb === "config" && sub === "explain") {
    const map = { schema_version: "2 for strict field checks; omitted for legacy YAML", eufy: "Credentials: EUFY_EMAIL/EUFY_PASSWORD environment variables take precedence", lan: "cidr is the camera LAN; force requires cidr and disallows public peer fallback", cameras: "Camera serial keys; mode is always, on_motion, or on_demand", go2rtc: "transcode is never, auto, or always" };
    return emit(path ? { path, explanation: map[path] ?? "See docs/config-server.md for this field" } : map, asJson);
  }
  if (verb === "config" && sub === "validate") {
    const text = await input(path);
    const { cfg } = loadConfig({ rawText: text });
    return emit({ ok: true, schemaVersion: parse(text)?.schema_version ?? 1, cameras: Object.keys(cfg.cameras).length, path: path ?? null }, asJson);
  }
  if (verb === "config" && sub === "apply") {
    const text = await input(path);
    return emit({ ok: true, ...await applyConfig({ target, yamlText: text, restart, health }) }, asJson);
  }
  if (verb === "config" && sub === "recover") return emit({ ok: true, ...await recoverInterruptedApply(target) }, asJson);
  if (verb === "status") {
    const config = existsSync(target) ? { path: target, ...(await applyStatus(target) ?? {}) } : { path: target, missing: true };
    let live;
    try { const h = await local("/healthz"); live = { ok: h.ok, auth: { state: h.auth?.state }, cameras: h.cameras, go2rtc: h.go2rtc, stalled: h.stalled }; } catch (error) { live = { ok: false, error: error.message }; }
    return emit({ config, live }, asJson);
  }
  if (verb === "doctor") {
    const checks = [];
    checks.push({ code: "NODE_VERSION", ok: Number(process.versions.node.split(".")[0]) >= 24, detail: process.version });
    checks.push({ code: "CONFIG_FILE", ok: existsSync(target), detail: target });
    try { const { cfg } = loadConfig(); checks.push({ code: "CONFIG_VALID", ok: true });
      try { await fs.access(cfg.go2rtcBin); checks.push({ code: "GO2RTC", ok: true, detail: cfg.go2rtcBin }); }
      catch { checks.push({ code: "GO2RTC", ok: false, detail: `${cfg.go2rtcBin} not accessible as a path (may be on PATH)` }); }
    } catch (error) { checks.push({ code: "CONFIG_VALID", ok: false, detail: error.message }); }
    try { const h = await local("/healthz"); checks.push({ code: "HEALTH", ok: h.auth?.state === "ok", detail: h.auth?.state }); }
    catch (error) { checks.push({ code: "HEALTH", ok: false, detail: error.message }); }
    emit({ ok: checks.every((c) => c.ok), checks, interfaces: interfaces() }, asJson);
    if (checks.some((c) => !c.ok)) process.exitCode = 1;
    return;
  }
  if (verb === "inventory" && sub === "export") {
    if (!path) throw new Error("inventory export needs a destination file");
    const hostFlag = clean.indexOf("--host");
    const host = hostFlag >= 0 ? clean[hostFlag + 1] : (process.env.BRIDGE_PUBLIC_HOST || interfaces()[0]?.address);
    if (!host || !/^[a-zA-Z0-9.:-]+$/.test(host)) throw new Error("inventory export needs a valid --host or a detected LAN address");
    const cameras = (await local("/api/cameras")).map(({ sn, name, model, modelName, enabled, mode, powered, powerOverride, dual, codec, streamKey }) => ({ sn, name, model, modelName, enabled, mode, powered, powerOverride, dual, codec, streamKey, rtsp: `rtsp://${host}:8554/${encodeURIComponent(streamKey)}` }));
    const inventory = { schema_version: 1, exported_at: new Date().toISOString(), bridge_url: `http://${host}:${loadConfig().cfg.port}`, cameras };
    await fs.writeFile(path, JSON.stringify(inventory, null, 2) + "\n", { flag: "wx", mode: 0o644 });
    return emit({ ok: true, file: path, cameras: cameras.length }, asJson);
  }
  if (verb === "setup") return setup(clean.slice(1), asJson);
  emit("Usage: eufy-bridge setup [--answers file] | doctor | status | inventory export <file> | config example|explain [path]|validate <file|->|apply <file|-> [--json]", false);
  if (verb) process.exitCode = 2;
}

main().catch((error) => fail(error?.message ?? String(error), process.argv.includes("--json")));
