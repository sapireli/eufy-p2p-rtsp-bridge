import { test } from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { LoginStatus } from "@mega-yfue/eufy-sdk";
import { createBridgeRuntime } from "../server.mjs";
import { loadConfig } from "../src/config.mjs";

async function fixture(t, { login, devices = [], authRetryOptions } = {}) {
  const dir = await mkdtemp(join(tmpdir(), "ewb-runtime-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const config = loadConfig({ env: { EUFY_EMAIL: "test@example.com", EUFY_PASSWORD: "secret" }, rawText: "schema_version: 2\nhost: 127.0.0.1\n" });
  config.cfg.port = 0;
  config.cfg.dataDir = dir;
  class FakeEufy extends EventEmitter {
    pollIntervalMs = 600_000;
    disconnected = 0;
    async login() { if (login instanceof Error) throw login; return login ?? { status: LoginStatus.Ok }; }
    async getDevices() { return devices; }
    async disconnect() { this.disconnected++; }
    getP2pSessions() { return new Map(); }
    setPollInterval() {}
  }
  const eufy = new FakeEufy();
  let go2rtcWrites = 0, go2rtcStarts = 0, closedClients = 0;
  const sdk = {
    LoginStatus,
    describe: async (sn) => ({ sn, name: "Door", model: "T8214", modelName: "Door", isCamera: true, battery: true, powerTier: "battery" }),
    snapshotStored: async () => Buffer.from("jpeg"),
    closeStreamClients: async () => { closedClients++; },
  };
  const runtime = await createBridgeRuntime({
    config,
    sdkFactory: () => ({ eufy, sdk }),
    go2rtcFactory: () => ({ writeGo2rtc: async () => { go2rtcWrites++; }, startGo2rtc: () => { go2rtcStarts++; }, stopGo2rtc: () => {} }),
    lanPreflight: async () => [],
    authRetryOptions,
  });
  t.after(() => runtime.stop());
  return { runtime, eufy, sdk, config, stats: () => ({ go2rtcWrites, go2rtcStarts, closedClients }) };
}

test("runtime starts an authenticated HTTP bridge, handles motion, and shuts down cleanly", async (t) => {
  const { runtime, eufy, stats } = await fixture(t, { devices: [{ sn: "BAT", raw: { parent_sn: "STA" } }] });
  const opened = [];
  runtime.ctx.ensureWarm = async (sn) => { opened.push(sn); };
  await runtime.start();
  const base = `http://127.0.0.1:${runtime.address().port}`;
  const health = await (await fetch(`${base}/healthz`)).json();
  assert.equal(health.auth.state, "ok");
  assert.equal(health.cameras, 1);
  assert.equal(health.go2rtc, "stopped", "fake media runner does not claim a live RTSP process");
  const cameras = await (await fetch(`${base}/api/cameras`)).json();
  assert.equal(cameras[0].sn, "BAT");
  assert.equal(cameras[0].mode, "on_motion");
  assert.deepEqual(stats(), { go2rtcWrites: 1, go2rtcStarts: 1, closedClients: 0 });
  eufy.emit("motion", { sn: "BAT" });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(runtime.ctx.holds.isHeld("BAT"), true);
  assert.deepEqual(opened, ["BAT"]);
  eufy.emit("pushConnect");
  assert.equal(runtime.ctx.state.flags.pushConnected, true);
  eufy.emit("pushDisconnect");
  assert.equal(runtime.ctx.state.flags.pushConnected, false);
  const pinned = [];
  runtime.ctx.applyPins = async (sn) => { pinned.push(sn); };
  eufy.emit("p2pConnect", "STA");
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(pinned, ["BAT"], "only cameras on the connected station are re-pinned");
  await assert.rejects(runtime.start(), /already started/);
  await runtime.stop();
  await runtime.stop();
  assert.equal(stats().closedClients, 1);
  assert.equal(eufy.disconnected, 1);
});

test("auth broadcast logs omit captcha image data", async (t) => {
  const { runtime } = await fixture(t);
  const logs = [];
  const original = console.log;
  console.log = (line) => logs.push(line);
  try { runtime.ctx.broadcast({ event: "auth", state: "require_captcha", image: "SECRET_IMAGE" }); }
  finally { console.log = original; }
  assert.ok(logs.some((line) => line.includes("require_captcha")));
  assert.ok(logs.every((line) => !line.includes("SECRET_IMAGE")));
});

test("login failure leaves health and auth recovery endpoints available", async (t) => {
  const { runtime } = await fixture(t, { login: new Error("cloud unavailable") });
  await runtime.start();
  const base = `http://127.0.0.1:${runtime.address().port}`;
  const health = await (await fetch(`${base}/healthz`)).json();
  assert.equal(health.auth.state, "pending");
  assert.equal((await fetch(`${base}/api/cameras`)).status, 503);
  assert.equal((await fetch(`${base}/auth/status`)).status, 200);
});

test("offline boot retries login and becomes ready when the network returns", async (t) => {
  const timers = [];
  const { runtime, eufy } = await fixture(t, { login: new Error("cloud unavailable"), authRetryOptions: {
    minDelayMs: 10, random: () => 0.5,
    setTimer: (fn, delay) => { const timer = { fn, delay }; timers.push(timer); return timer; },
    clearTimer: () => {},
  } });
  await runtime.start();
  assert.equal(runtime.ctx.authStatus().state, "pending");
  assert.equal(timers[0].delay, 10);
  eufy.login = async () => ({ status: LoginStatus.Ok });
  await timers[0].fn();
  assert.equal(runtime.ctx.authStatus().state, "ok");
  assert.equal(runtime.ctx.state.flags.ready, true);
  assert.equal(timers.length, 1);
});

test("a transient camera list failure at boot retries without duplicate activity listeners", async (t) => {
  const timers = [];
  const { runtime, eufy, stats } = await fixture(t, { devices: [{ sn: "BAT", raw: { parent_sn: "STA" } }], authRetryOptions: {
    minDelayMs: 10, random: () => 0.5,
    setTimer: (fn) => { const timer = { fn }; timers.push(timer); return timer; },
    clearTimer: () => {},
  } });
  const getDevices = eufy.getDevices.bind(eufy);
  let calls = 0;
  eufy.getDevices = async () => { if (++calls === 1) throw new Error("list timed out"); return getDevices(); };
  await runtime.start();
  assert.equal(runtime.ctx.state.flags.ready, false);
  assert.equal(timers.length, 1);
  await timers[0].fn();
  assert.equal(runtime.ctx.state.flags.ready, true);
  assert.equal(runtime.ctx.listCameras().length, 1);
  assert.equal(eufy.listenerCount("deviceState"), 1);
  assert.deepEqual(stats().go2rtcWrites, 1);
});

test("an invalid listener setting rejects start before login", async (t) => {
  const { runtime, config } = await fixture(t);
  config.cfg.port = -1;
  await assert.rejects(runtime.start(), /port/i);
  assert.equal(runtime.server.listening, false);
});
