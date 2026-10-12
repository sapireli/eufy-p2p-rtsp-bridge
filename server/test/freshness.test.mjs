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
