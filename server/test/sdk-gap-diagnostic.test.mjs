import test from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { initializeSdkGapDiagnostic } from "../src/sdk-gap-diagnostic.mjs";

function harness({ rows = 1200, source = true } = {}) {
  const session = new EventEmitter(), owner = new EventEmitter();
  Object.assign(owner, { session, channel: 2, mediaChannel: 2, awaitingKeyframeAfterGap: false,
    units: { push(value) { return value; } } });
  session.on("videoGap", () => { owner.awaitingKeyframeAfterGap = true; });
  const src = { stream: owner, receiptSourceId: "source-id" };
  const publicSessions = new Map([["PRIVATE-STATION#live:2", session]]);
  const ctx = { eufy: { p2p: { liveSources: new Map(source ? [["PRIVATE:2", src]] : []), liveSessionKeys: new Map() },
    getP2pSessions: () => publicSessions }, state: { slots: new Map([["PRIVATE-CAMERA", {
      lastSourceReceipt: { sourceId: "source-id", sequence: 2n, monotonicUs: 100n }, lastReceipt: { frameId: 3n },
      freshness: { inputFrames: 2, outputFrames: 1, staleFrames: 0, unmappedFrames: 0 },
    }]]) }, getCamera: () => ({ name: "Garage" }), freshness: { clock: () => ({ serverEpoch: "epoch" }) } };
  let tick, end, closed = 0, output = [], clock = 0;
  const options = { env: { EWB_GAP_DIAGNOSTIC_FILE: "/private/proof", EWB_GAP_DIAGNOSTIC_SECONDS: "300" },
    io: { openSync(_p, _flags, mode) { assert.equal(mode, 0o600); return 1; },
      writeSync(_fd, bytes) { output.push(JSON.parse(bytes.toString())); return Buffer.byteLength(bytes); }, closeSync() { closed++; } },
    setIntervalFn(fn, ms) { assert.equal(ms, 1000); tick = fn; return {}; }, clearIntervalFn() { tick = null; },
    setTimer(fn, ms) { assert.equal(ms, 300000); end = fn; return {}; }, clearTimer() { end = null; },
    nowUs: () => BigInt(++clock), wallMs: () => 10, maxRows: rows, warn: () => {} };
  const original = owner.units.push;
  const stop = initializeSdkGapDiagnostic(ctx, options);
  return { ctx, options, session, owner, src, original, stop, output, tick: () => tick?.(), end: () => end?.(),
    closed: () => closed, activeTimer: () => end !== null };
}

test("disabled default does not inspect SDK or open files", () => {
  let opened = false;
  initializeSdkGapDiagnostic(new Proxy({}, { get() { throw Error("inspected"); } }), {
    env: {}, io: { openSync() { opened = true; } },
  })();
  assert.equal(opened, false);
});
test("fails diagnostic closed when D5 maps absent", () => {
  let opened = false;
  initializeSdkGapDiagnostic({}, { env: { EWB_GAP_DIAGNOSTIC_FILE: "/private/proof" },
    io: { openSync() { opened = true; } }, warn() {} })();
  assert.equal(opened, false);
});
test("inventory opaque IDs, exact source link, and independent boundaries", () => {
  const h = harness(); const sample = h.output.find(x => x.kind === "sample");
  assert.equal(sample.inventory[0].keyKind, "dedicated-media");
  assert.equal(sample.streams[0].camera, "Garage");
  assert.equal(sample.streams[0].ingress.frameId, "3");
  assert.equal(sample.streams[0].encodedHttp.outputFrames, 1);
  assert.equal(JSON.stringify(h.output).includes("PRIVATE"), false); h.stop();
});
test("assembler preserves this, arguments and exact result; virtual gating follows first keyframe", () => {
  const h = harness(); h.session.emit("videoGap");
  const units = [{ keyframe: false }, { keyframe: true }, { keyframe: false }];
  assert.strictEqual(h.owner.units.push(units), units);
  h.tick(); const info = h.output.at(-1).streams[0];
  assert.equal(info.assembled, 3); assert.equal(info.gatedCompleteUnits, 1);
  assert.equal(h.output.find(x => x.kind === "video-gap").members[0].waitingKeyframe, true);
  h.stop();
});
test("borrowed receiver and original exception are preserved", () => {
  const h = harness({ source: false }); const sentinel = {}, receiver = {}, result = [];
  h.owner.units.push = function (...args) { assert.strictEqual(this, receiver); assert.deepEqual(args, [1, 2]); return result; };
  h.ctx.eufy.p2p.liveSources.set("PRIVATE:2", h.src); h.tick();
  assert.strictEqual(h.owner.units.push.call(receiver, 1, 2), result); h.stop();
  const x = harness({ source: false });
  x.owner.units.push = function () { throw sentinel; };
  x.ctx.eufy.p2p.liveSources.set("PRIVATE:2", x.src); x.tick();
  assert.throws(() => x.owner.units.push([]), e => e === sentinel); x.stop();
});
test("known data channels and delivered keyframes counted without retaining payload", () => {
  const h = harness(); h.session.emit("data", { commandId: 1300, channel: 2, data: Buffer.from("PRIVATE-PIXELS") });
  h.session.emit("data", { commandId: 1300, channel: 3 });
  h.owner.emit("video", { keyframe: true, sourceTimestamp: 99, data: Buffer.from("PRIVATE-PIXELS") }); h.tick();
  const row = h.output.at(-1); assert.equal(row.streams[0].acceptedData, 1); assert.equal(row.streams[0].delivered, 1);
  assert.equal(row.streams[0].keyframes, 1); assert.equal(row.inventory[0].data, 2);
  assert.equal(JSON.stringify(h.output).includes("PIXELS"), false); h.stop();
});
test("duration restores wrappers/listeners and stops sampling", () => {
  const h = harness(); h.end();
  assert.strictEqual(h.owner.units.push, h.original); assert.equal(h.owner.listenerCount("video"), 0);
  assert.equal(h.session.listenerCount("data"), 0); assert.equal(h.session.listenerCount("videoGap"), 1);
  assert.equal(h.closed(), 1); const count = h.output.length; h.tick(); h.stop(); assert.equal(h.output.length, count);
});
test("row cap stops all hooks without an extra row", () => {
  const h = harness({ rows: 3 }); h.tick();
  assert.equal(h.output.length, 3); assert.equal(h.closed(), 1); assert.strictEqual(h.owner.units.push, h.original);
});
test("cleanup preserves a later third-party method replacement", () => {
  const h = harness(), later = () => [];
  h.owner.units.push = later; h.stop(); assert.strictEqual(h.owner.units.push, later);
});
test("new stream listener ordering stays after SDK gate handler", () => {
  const h = harness({ source: false }); h.owner.awaitingKeyframeAfterGap = false;
  // Simulates stream starting after an already inventoried control session.
  h.session.on("videoGap", () => { h.owner.awaitingKeyframeAfterGap = true; });
  h.ctx.eufy.p2p.liveSources.set("PRIVATE:2", h.src); h.tick(); h.session.emit("videoGap");
  assert.equal(h.output.at(-1).members[0].waitingKeyframe, true); h.stop();
});
test("failed write unhooks only diagnostic and leaves SDK intact", () => {
  const h = harness(); h.options.io.writeSync = () => { throw Error("disk full"); }; h.tick();
  assert.strictEqual(h.owner.units.push, h.original); assert.equal(h.closed(), 1); assert.equal(h.activeTimer(), false);
});
