import { test } from "node:test";
import assert from "node:assert/strict";
import { createCameras, DUAL_MODELS } from "../src/cameras.mjs";
import { createState } from "../src/state.mjs";

function ctxWith(devices, cfgCams = {}, defaults = { quality: "Full HD (1080P)", dualView: "split" }) {
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
const batt = { sn: "T8113B", name: "Yard", model: "T8113", modelName: "eufyCam 2C", isCamera: true, battery: true };
const door = { sn: "T8214C", name: "Door", model: "T8214", modelName: "Doorbell E340", isCamera: true, battery: false };
const hub = { sn: "T8010D", name: "HomeBase", model: "T8010", modelName: "HomeBase 2", isCamera: false, battery: false };

test("wired cameras enabled by default, battery excluded, non-cameras dropped", async () => {
  const c = createCameras(ctxWith([wired, batt, door, hub]));
  const cams = await c.refreshCameras();
  assert.deepEqual(cams.map((x) => [x.sn, x.enabled]), [["T8410A", true], ["T8113B", false], ["T8214C", true]]);
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
    dual: false, dualView: null, quality: "Full HD (1080P)", codec: "h264", width: 1920, height: 1080,
    streaming: true, stalls: 2, blocked: "wan-path 203.0.113.9", rtsp: "rtsp://192.168.1.10:8554/T8410A",
    stream: "/stream/T8410A",
  });
});

test("DUAL_MODELS map", () => {
  assert.equal(DUAL_MODELS.T8425, 6243);
  assert.equal(DUAL_MODELS.T8213, 2700);
});

test("battery camera with name override (no enabled key) logs exclusion", async () => {
  const logs = [];
  const origLog = console.log;
  console.log = (...args) => logs.push(args.join(" "));
  try {
    const c = createCameras(ctxWith([batt], { T8113B: { name: "Custom Yard" } }));
    await c.refreshCameras();
    assert.equal(c.getCamera("T8113B").enabled, false);
    assert.equal(c.getCamera("T8113B").name, "Custom Yard");
    assert.equal(logs.some((l) => l.includes("[bridge] T8113B") && l.includes("battery-powered")), true);
  } finally {
    console.log = origLog;
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
