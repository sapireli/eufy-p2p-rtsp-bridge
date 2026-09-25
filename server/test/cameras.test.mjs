import { test } from "node:test";
import assert from "node:assert/strict";
import { createCameras, DUAL_MODELS } from "../src/cameras.mjs";
import { createState } from "../src/state.mjs";

function ctxWith(devices, cfgCams = {}, defaults = { quality: "Full HD (1080P)", dualView: "split", holdSeconds: 60 }) {
  const byName = Object.fromEntries(devices.map((d) => [d.sn, d]));
  return {
    cfg: { cameras: cfgCams, defaults, port: 3000 },
    state: createState(),
    eufy: { getDevices: async () => devices.map((d) => ({ sn: d.sn })) },
    sdk: { describe: async (sn) => byName[sn] },
    streamStatus: () => ({ streaming: false, stalls: 0, codec: undefined, width: undefined, height: undefined }),
  };
}

const wired = { sn: "T8410A", name: "Garage", model: "T8410", modelName: "Indoor Cam", isCamera: true, battery: false, powerTier: "wired" };
const batt = { sn: "T8113B", name: "Yard", model: "T8113", modelName: "eufyCam 2C", isCamera: true, battery: true, powerTier: "battery" };
const door = { sn: "T8214C", name: "Door", model: "T8214", modelName: "Doorbell E340", isCamera: true, battery: true, powerTier: "wired" };
const hub = { sn: "T8010D", name: "HomeBase", model: "T8010", modelName: "HomeBase 2", isCamera: false, battery: false };

// Every camera is enabled; the power source decides HOW it streams, not WHETHER it appears. Non-cameras
// (a HomeBase) are still dropped.
test("all cameras enabled, mode follows the power source, non-cameras dropped", async () => {
  const c = createCameras(ctxWith([wired, batt, door, hub]));
  const cams = await c.refreshCameras();
  assert.deepEqual(
    cams.map((x) => [x.sn, x.enabled, x.mode]),
    [["T8410A", true, "always"], ["T8113B", true, "on_motion"], ["T8214C", true, "always"]],
  );
  assert.equal(c.getCamera("T8010D"), undefined);
});

test("config overrides name/enabled/quality/dual view; dual models flagged with command id", async () => {
  const c = createCameras(ctxWith([wired, batt, door], {
    T8113B: { enabled: true, quality: "HD (720P)" },
    T8214C: { name: "Front", dualView: "pip-br" },
  }));
  await c.refreshCameras();
  assert.equal(c.getCamera("T8113B").enabled, true);
  assert.equal(c.getCamera("T8113B").quality, "HD (720P)");
  assert.equal(c.getCamera("T8410A").quality, "Full HD (1080P)");
  assert.equal(c.getCamera("T8214C").name, "Front");
  assert.equal(c.getCamera("T8214C").isDual, true);
  assert.equal(c.getCamera("T8214C").viewModeCmd, 6243);
  assert.equal(c.getCamera("T8214C").dualView, "pip-br");
  assert.equal(c.getCamera("T8410A").isDual, false);
});

test("apiShape merges stream status and rtsp url", async () => {
  const ctx = ctxWith([wired]);
  ctx.streamStatus = () => ({ streaming: true, stalls: 2, codec: "h264", width: 1920, height: 1080 });
  ctx.state.blocked.set("T8410A", "wan-path 203.0.113.9");
  const c = createCameras(ctx);
  await c.refreshCameras();
  const s = c.apiShape(c.getCamera("T8410A"), "192.168.1.10");
  assert.deepEqual(s, {
    sn: "T8410A", name: "Garage", model: "T8410", modelName: "Indoor Cam", enabled: true, powered: true, powerOverride: "auto",
    mode: "always", holdSeconds: 60, held: false, streamKey: "garage",
    dual: false, dualView: null, quality: "Full HD (1080P)", codec: "h264", width: 1920, height: 1080,
    streaming: true, stalls: 2, blocked: "wan-path 203.0.113.9", rtsp: "rtsp://192.168.1.10:8554/garage",
    stream: "/stream/T8410A",
  });
});

// A battery camera's whole behaviour is its mode and whether something is holding it right now; an API
// that reports neither cannot explain why a camera is or is not streaming.
test("apiShape reports the mode and live hold state", async () => {
  const ctx = ctxWith([batt]);
  ctx.holds = { isHeld: (sn) => sn === "T8113B" };
  const c = createCameras(ctx);
  await c.refreshCameras();
  const s = c.apiShape(c.getCamera("T8113B"), "192.168.1.10");
  assert.equal(s.mode, "on_motion", "a battery camera defaults to on_motion");
  assert.equal(s.held, true);
});

test("DUAL_MODELS map", () => {
  assert.equal(DUAL_MODELS.T8425, 6243);
  assert.equal(DUAL_MODELS.T8213, 2700);
});

// Phase 2: a battery camera is enabled, but on_motion — it streams when something happens rather than
// continuously, which is the only way to show one without flattening it. Phase 1 excluded them outright.
test("battery camera is enabled on_motion by default", async () => {
  const c = createCameras(ctxWith([batt], { T8113B: { name: "Custom Yard" } }));
  await c.refreshCameras();
  const cam = c.getCamera("T8113B");
  assert.equal(cam.enabled, true);
  assert.equal(cam.mode, "on_motion");
  assert.equal(cam.name, "Custom Yard");
});

test("a wired camera defaults to always-on, and mode can be overridden per camera", async () => {
  const c = createCameras(ctxWith([batt], { T8113B: { mode: "on_demand", holdSeconds: 15 } }));
  await c.refreshCameras();
  assert.equal(c.getCamera("T8113B").mode, "on_demand");
  assert.equal(c.getCamera("T8113B").holdSeconds, 15);
});

test("battery-budgeted camera cannot be continuously reopened after every SDK budget stop", async () => {
  const c = createCameras(ctxWith([batt], { T8113B: { mode: "always" } }));
  await assert.rejects(c.refreshCameras(), /power_override: always-on/);
});

test("battery camera with explicit enabled: false does not log exclusion", async () => {
  const logs = [];
  const origLog = console.log;
  console.log = (...args) => logs.push(args.join(" "));
  try {
    const c = createCameras(ctxWith([batt], { T8113B: { enabled: false } }));
    await c.refreshCameras();
    assert.equal(c.getCamera("T8113B").enabled, false);
    assert.equal(logs.some((l) => l.includes("[bridge] T8113B") && l.includes("battery-powered")), false);
  } finally {
    console.log = origLog;
  }
});

test("camera mode follows the SDK power tier even when a battery capability is present", async () => {
  const c = createCameras(ctxWith([door, batt]));
  await c.refreshCameras();
  assert.equal(c.getCamera(door.sn).mode, "always");
  assert.equal(c.getCamera(batt.sn).mode, "on_motion");
});

test("an explicit local power claim sets the default stream mode and API policy", async () => {
  const camera = { ...batt, powerTier: "wired", powerOverride: "always-on" };
  const c = createCameras(ctxWith([camera]));
  await c.refreshCameras();
  assert.equal(c.getCamera(camera.sn).mode, "always");
  assert.equal(c.apiShape(c.getCamera(camera.sn), "192.0.2.1").powerOverride, "always-on");
});

test("a transient SDK describe error is retried before publishing the camera registry", async () => {
  const ctx = ctxWith([wired, batt]);
  const describe = ctx.sdk.describe;
  const calls = [];
  ctx.sdk.describe = async (sn) => {
    calls.push(sn);
    if (sn === wired.sn && calls.filter((s) => s === sn).length === 1) throw new Error("temporary SDK lookup failure");
    return describe(sn);
  };
  const delays = [];
  const cameras = createCameras(ctx, { wait: async (ms) => { delays.push(ms); } });
  await cameras.refreshCameras();
  assert.deepEqual(cameras.listCameras().map((c) => c.sn), [wired.sn, batt.sn]);
  assert.deepEqual(delays, [200]);
  assert.deepEqual(calls, [wired.sn, wired.sn, batt.sn]);
});

test("persistent describe failures have a bounded retry budget and preserve healthy cameras", async () => {
  const ctx = ctxWith([wired, batt]);
  ctx.sdk.describe = async (sn) => { if (sn === wired.sn) throw new Error("broken device"); return batt; };
  const delays = [];
  const cameras = createCameras(ctx, { wait: async (ms) => { delays.push(ms); } });
  await cameras.refreshCameras();
  assert.deepEqual(cameras.listCameras().map((c) => c.sn), [batt.sn]);
  assert.deepEqual(delays, [200, 400]);
});

test("stopping discovery cancels a pending describe retry without publishing a partial registry", async () => {
  const ctx = ctxWith([wired, batt]);
  ctx.sdk.describe = async () => { throw new Error("temporary failure"); };
  let waiting;
  const cameras = createCameras(ctx, { wait: (_ms, _value, { signal }) => new Promise((resolve, reject) => {
    waiting = true;
    signal.addEventListener("abort", () => reject(Object.assign(new Error("aborted"), { name: "AbortError" })), { once: true });
  }) });
  const discovery = cameras.refreshCameras();
  while (!waiting) await new Promise((r) => setImmediate(r));
  cameras.stopCameraDiscovery();
  await discovery;
  assert.deepEqual(cameras.listCameras(), []);
});

test("periodic rediscovery retries one missing camera without replacing healthy registry entries", async () => {
  const ctx = ctxWith([wired, batt]);
  const describe = ctx.sdk.describe;
  let available = false;
  ctx.sdk.describe = async (sn) => {
    if (sn === batt.sn && !available) throw new Error("camera temporarily unavailable");
    return describe(sn);
  };
  const cameras = createCameras(ctx, { wait: async () => {} });
  await cameras.refreshCameras();
  const healthy = cameras.getCamera(wired.sn);
  assert.deepEqual(cameras.missingCameraSerials(), [batt.sn]);
  assert.deepEqual(await cameras.retryMissingCameras(async () => { throw new Error("must not publish"); }), []);
  assert.deepEqual(cameras.missingCameraSerials(), [batt.sn]);
  available = true;
  assert.deepEqual(await cameras.retryMissingCameras(async () => { throw new Error("go2rtc API unavailable"); }), []);
  assert.equal(cameras.getCamera(batt.sn), undefined, "failed media registration keeps the camera pending");
  const added = await cameras.retryMissingCameras(async (cam, key) => {
    assert.equal(cam.sn, batt.sn);
    assert.equal(key, "yard");
    assert.equal(cameras.getCamera(batt.sn), undefined, "camera stays unpublished until media registration succeeds");
  });
  assert.deepEqual(added.map((c) => c.sn), [batt.sn]);
  assert.equal(cameras.getCamera(wired.sn), healthy);
  assert.deepEqual(cameras.missingCameraSerials(), []);
});

test("overlapping rediscovery ticks register a recovered camera once", async () => {
  const ctx = ctxWith([wired]);
  const describe = ctx.sdk.describe;
  let available = false;
  ctx.sdk.describe = async (sn) => { if (!available) throw new Error("unavailable"); return describe(sn); };
  const cameras = createCameras(ctx, { wait: async () => {} });
  await cameras.refreshCameras();
  available = true;
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  let registrations = 0;
  const first = cameras.retryMissingCameras(async () => { registrations++; await gate; });
  const second = cameras.retryMissingCameras(async () => { registrations++; });
  release();
  await Promise.all([first, second]);
  assert.equal(registrations, 1);
  assert.deepEqual(cameras.listCameras().map((c) => c.sn), [wired.sn]);
});

test("shutdown during go2rtc registration does not publish a recovered camera", async () => {
  const ctx = ctxWith([wired]);
  const describe = ctx.sdk.describe;
  let available = false;
  ctx.sdk.describe = async (sn) => { if (!available) throw new Error("unavailable"); return describe(sn); };
  const cameras = createCameras(ctx, { wait: async () => {} });
  await cameras.refreshCameras();
  available = true;
  let registrationStarted;
  const started = new Promise((resolve) => { registrationStarted = resolve; });
  const retry = cameras.retryMissingCameras(async (_cam, _key, signal) => {
    registrationStarted();
    await new Promise((resolve) => signal.addEventListener("abort", resolve, { once: true }));
  });
  await started;
  cameras.stopCameraDiscovery();
  assert.deepEqual(await retry, []);
  assert.deepEqual(cameras.listCameras(), []);
});
