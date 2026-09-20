import { test } from "node:test";
import assert from "node:assert/strict";
import { createPins } from "../src/pins.mjs";

function ctxWith({ cam, setPropertyImpl }) {
  const sent = [];
  const props = [];
  return {
    sent, props,
    getCamera: () => cam,
    listCameras: () => [cam],
    sdk: {
      streamClientFor: async () => ({ id: "client" }),
      sendSetPayload: async (client, sn, cmd, payload) => sent.push({ sn, cmd, payload }),
    },
    eufy: { setProperty: async (sn, name, value) => { props.push({ sn, name, value }); return setPropertyImpl?.(); } },
  };
}

test("dual-lens camera gets view mode command; quality set when supported", async () => {
  const logs = [];
  const origLog = console.log;
  console.log = (...args) => logs.push(args.join(" "));
  try {
    const ctx = ctxWith({ cam: { sn: "T8214C", isDual: true, viewModeCmd: 6243, dualView: "split", quality: "Full HD (1080P)", enabled: true } });
    const r = await createPins(ctx).applyPins("T8214C");
    assert.deepEqual(ctx.sent, [{ sn: "T8214C", cmd: 6243, payload: { restore: 1, video_type: 12 } }]);
    assert.deepEqual(ctx.props, [{ sn: "T8214C", name: "streamingQuality", value: "Full HD (1080P)" }]);
    assert.deepEqual(r, { dualView: "set", quality: "set" });
  } finally {
    console.log = origLog;
  }
});

test("quality setter unsupported → reported, not thrown; single-lens sends no view command", async () => {
  const logs = [];
  const origWarn = console.warn;
  console.warn = (...args) => logs.push(args.join(" "));
  try {
    const ctx = ctxWith({
      cam: { sn: "T8410A", isDual: false, viewModeCmd: null, dualView: null, quality: "HD (720P)", enabled: true },
      setPropertyImpl: () => { throw new Error("wire unverified"); },
    });
    const r = await createPins(ctx).applyPins("T8410A");
    assert.deepEqual(ctx.sent, []);
    assert.deepEqual(r, { dualView: "skip", quality: "unsupported" });
  } finally {
    console.warn = origWarn;
  }
});

test("no quality configured → skip; disabled camera → nothing", async () => {
  const ctx = ctxWith({ cam: { sn: "T8410A", isDual: false, quality: null, enabled: false } });
  const r = await createPins(ctx).applyPins("T8410A");
  assert.deepEqual(r, { dualView: "skip", quality: "skip" });
  assert.deepEqual(ctx.props, []);
});

// Pinning sends a SET_PAYLOAD to a camera that may be MID-STREAM. A station opening several media
// sessions fires a connect event for each, so without a gap one camera coming up re-pokes its siblings
// repeatedly — measured as 4 re-pins of a streaming dual camera inside five minutes.
test("a camera is not re-pinned again moments later", async () => {
  const cam = { sn: "DUAL", isDual: true, viewModeCmd: 6243, dualView: "split", enabled: true };
  const ctx = ctxWith({ cam });
  const pins = createPins(ctx);

  const first = await pins.applyPins("DUAL");
  assert.equal(first.dualView, "set", "the first pass pins");
  const second = await pins.applyPins("DUAL");
  assert.equal(second.dualView, "recent", "a second pass moments later is skipped");
  assert.equal(ctx.sent.length, 1, "exactly one SET_PAYLOAD reached the camera");
});

// A boot or post-re-login pass is deliberate, not incidental, so it must not be swallowed by the gap.
test("applyAllPins pins even right after a scoped pass", async () => {
  const cam = { sn: "DUAL", isDual: true, viewModeCmd: 6243, dualView: "split", enabled: true };
  const ctx = ctxWith({ cam });
  const pins = createPins(ctx);
  await pins.applyPins("DUAL");
  await pins.applyAllPins();
  assert.equal(ctx.sent.length, 2, "the deliberate pass is forced through");
});
