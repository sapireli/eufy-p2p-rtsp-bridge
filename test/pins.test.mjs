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
