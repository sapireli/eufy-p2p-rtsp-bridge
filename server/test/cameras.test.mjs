import { test } from "node:test";
import assert from "node:assert/strict";
import { createCameras, DUAL_MODELS, isPowered } from "../src/cameras.mjs";
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

const wired = { sn: "T8410A", name: "Garage", model: "T8410", modelName: "Indoor Cam", isCamera: true, battery: false };
const batt = { sn: "T8113B", name: "Yard", model: "T8113", modelName: "eufyCam 2C", isCamera: true, battery: true, batteryLevel: 40, charging: false };
const door = { sn: "T8214C", name: "Door", model: "T8214", modelName: "Doorbell E340", isCamera: true, battery: false };
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
    sn: "T8410A", name: "Garage", model: "T8410", modelName: "Indoor Cam", enabled: true, powered: true,
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

// Asking for always-on on a battery camera is legal but self-defeating, so it is called out.
test("battery camera forced to always warns that it will flatten", async () => {
  const warns = [];
  const origWarn = console.warn;
  console.warn = (...args) => warns.push(args.join(" "));
  try {
    const c = createCameras(ctxWith([batt], { T8113B: { mode: "always" } }));
    await c.refreshCameras();
    assert.equal(warns.some((l) => l.includes("T8113B") && l.includes("flatten")), true);
  } finally {
    console.warn = origWarn;
  }
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

test("isPowered: evidence beats the battery capability flag", () => {
  const base = { battery: true, model: "T8113", batteryLevel: 40, charging: false };
  assert.equal(isPowered(base), false, "real battery camera");
  assert.equal(isPowered({ ...base, battery: false }), true, "no battery capability");
  assert.equal(isPowered({ ...base, model: "T8425" }), true, "Floodlight E340 is mains despite reporting a level");
  assert.equal(isPowered({ ...base, model: "T8423", batteryLevel: undefined }), true, "Floodlight 2 Pro reports no level");
  assert.equal(isPowered({ ...base, model: "T8214", charging: true }), true, "hardwired doorbell is charging");
  assert.equal(isPowered({ ...base, model: "T8214", charging: false }), false, "battery doorbell not charging");
});
