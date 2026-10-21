import test from "node:test";
import assert from "node:assert/strict";
import { createFreshness } from "../src/freshness.mjs";

const ctx = (freshness = {}) => ({ cfg: { port: 3000, go2rtcTranscode: "always", freshness }, platform: "linux" });
test("new bridges default to observation and require explicit display enforcement", () => {
  const observed = createFreshness(ctx()).capability({}, "bridge.test");
  assert.equal(observed.mode, "observe");
  // Version 1 clients enforce every advertised capability. They must reject
  // this version rather than treating observation as display enforcement.
  assert.equal(observed.version, 2);
  assert.equal(observed.captureClockValidated, false);
  assert.equal(observed.clockUrl, "http://bridge.test:3000/api/clock");
  assert.equal(createFreshness(ctx({ mode: "enforce" })).capability({}, "bridge.test").mode, "enforce");
  assert.throws(() => createFreshness(ctx({ mode: "buffer" })), /mode/);
});
test("disabled or unsupported encoder paths cannot advertise supported freshness", () => {
  assert.equal(createFreshness(ctx({ enabled: false })).capability({}, "bridge.test"), undefined);
  const other = { ...ctx(), platform: "darwin" };
  assert.equal(createFreshness(other).capability({}, "bridge.test"), undefined);
  assert.equal(createFreshness(other).capability({ transcode: "never" }, "bridge.test").mode, "observe");
});
test("receipt IDs and monotonic clocks preserve source words without inventing capture time", () => {
  let now = 123456789n;
  const fresh = createFreshness(ctx(), { epoch: Buffer.alloc(8, 3), clock: () => now });
  const first = fresh.record({ sourceTimestamp: 0xffffffff });
  now += 1000n;
  const second = fresh.record({ sourceTimestamp: 0 });
  assert.equal(first.sourceTimestamp, 0xffffffff);
  assert.equal(second.sourceTimestamp, 0);
  assert.equal(first.ingressMonoUs, 123456789n);
  assert.equal(second.ingressMonoUs, 123457789n);
  assert.equal(second.frameId, first.frameId + 1n);
  assert.equal(fresh.clock().captureClockValidated, false);
});

test("retained and queued SDK pictures keep their first receipt and bridge identity", () => {
  let now = 1_000_000n;
  const fresh = createFreshness(ctx(), { clock: () => now });
  const receipt = Object.freeze({ sourceId: "pull-A", sequence: 1n, monotonicUs: now });
  const frame = { sourceTimestamp: 1234 };
  const first = fresh.record(frame, receipt);
  now = 6_000_000n;
  const replayed = fresh.record(frame, receipt);
  assert.equal(replayed, first);
  assert.equal(now - replayed.ingressMonoUs, 5_000_000n);
  const queued = fresh.record({ sourceTimestamp: 1301 }, Object.freeze({ sourceId: "pull-A", sequence: 2n, monotonicUs: 2_000_000n }));
  assert.equal(queued.ingressMonoUs, 2_000_000n);
  assert.equal(queued.frameId, first.frameId + 1n);
  const rebuilt = fresh.record(frame, Object.freeze({ sourceId: "pull-B", sequence: 1n, monotonicUs: now }));
  assert.notEqual(rebuilt.frameId, first.frameId);
});

test("invalid source receipts cannot mint trusted freshness", () => {
  const fresh = createFreshness(ctx(), { clock: () => 100n });
  const valid = { sourceId: "pull-A", sequence: 1n, monotonicUs: 90n };
  for (const receipt of [null, {}, { ...valid, sourceId: "" }, { ...valid, sequence: 0n },
    { ...valid, monotonicUs: -1n }, { ...valid, monotonicUs: 101n }, { ...valid, monotonicUs: 90 }]) {
    assert.throws(() => fresh.record({ sourceTimestamp: 1 }, receipt), /invalid SDK/);
  }
});


test("SDK wall-clock arrival keeps queued age and repeated-picture identity", () => {
  let mono = 10_000_000n, wall = 100_000;
  const fresh = createFreshness(ctx(), { clock: () => mono, wallClock: () => wall });
  const frame = { sourceTimestamp: 50 };
  const receipt = fresh.sourceReceipt({ frame, timestampMs: 97_000 });
  const stamp = fresh.record(frame, receipt);
  assert.equal(stamp.ingressMonoUs, 7_000_000n);
  mono += 2_000_000n; wall += 2000;
  assert.equal(fresh.sourceReceipt({ frame, timestampMs: 97_000 }), receipt);
  assert.equal(fresh.record(frame, receipt), stamp);
  assert.equal(mono - stamp.ingressMonoUs, 5_000_000n);
  const next = fresh.sourceReceipt({ frame: {}, timestampMs: wall });
  assert.equal(next.monotonicUs, mono);
  assert.equal(next.sequence, receipt.sequence + 1n);
  assert.throws(() => fresh.sourceReceipt({ frame, timestampMs: wall }), /arrival time changed/);
});

test("missing, invalid and future SDK arrivals cannot become fresh pictures", () => {
  const fresh = createFreshness(ctx(), { clock: () => 10_000_000n, wallClock: () => 100_000 });
  for (const timestampMs of [undefined, null, NaN, Infinity, -1, 1.5, 100_001, 1]) {
    assert.throws(() => fresh.sourceReceipt({ frame: {}, timestampMs }), /SDK source arrival/);
  }
});


test("wall-clock steps beyond the freshness budget fail instead of freshening queued video", () => {
  let mono = 10_000_000n, wall = 100_000;
  const fresh = createFreshness(ctx(), { clock: () => mono, wallClock: () => wall });
  fresh.sourceReceipt({ frame: {}, timestampMs: wall });
  mono += 1_000_000n;
  wall -= 2000;
  assert.throws(() => fresh.sourceReceipt({ frame: {}, timestampMs: wall - 100 }), /wall clock changed/);
});
