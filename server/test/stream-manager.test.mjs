import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { PassThrough } from "node:stream";
import { createStreamManager } from "../src/stream-manager.mjs";
import { createState } from "../src/state.mjs";

// Keep test output pristine: capture the manager's console.log/warn/error for the whole file.
const originals = {};
before(() => {
  for (const k of ["log", "warn", "error"]) {
    originals[k] = console[k];
    console[k] = () => {};
  }
});
after(() => {
  for (const k of ["log", "warn", "error"]) console[k] = originals[k];
});

// SPS+PPS+IDR (keyframe) and a delta-frame chunk, both Annex-B.
const KEY = Buffer.from([0,0,0,1,0x67,0x42,0xc0,0x1e,0xda,0x02,0x80,0xf6,0x80,0x6d,0x0a,0x13,0x50, 0,0,0,1,0x68,0xce,0x38,0x80, 0,0,0,1,0x65,0x88,0x84,0x00]);
const DELTA = Buffer.from([0,0,0,1,0x41,0x9a,0x00,0x11]);

function fakeRes() {
  const chunks = [];
  return { chunks, writableNeedDrain: false, destroyed: false, write(b) { chunks.push(b); return true; }, end() { this.destroyed = true; } };
}

function ctxWith({ feeds, exit }) {
  const state = createState();
  let opens = 0;
  return {
    state,
    cfg: { stall: { stallMs: 12000, gapMs: 45000, exitAfterMs: 300000, backoffMs: [10, 20, 40] } },
    exit: exit ?? (() => {}),
    getCamera: () => ({ enabled: true }),
    isBlocked: () => false,
    applyPins: async () => {},
    sdk: {
      streamClientFor: async () => ({}),
      openFeed: async () => { opens++; return feeds[Math.min(opens, feeds.length) - 1](); },
      extractParamSets: (b) => (b[4] === 0x67 ? { codec: "h264", sps: [b.subarray(4, 17)], pps: [] } : undefined),
      codedGeometry: () => ({ width: 640, height: 480 }),
    },
    opens: () => opens,
  };
}

// Poll until `predicate()` is true or the cap elapses, instead of a bare fixed sleep.
async function waitUntil(predicate, capMs = 500) {
  const start = Date.now();
  while (!predicate()) {
    if (Date.now() - start > capMs) break;
    await new Promise((r) => setTimeout(r, 5));
  }
}

test("warm feed sniffs codec/geometry, primes late consumer with last keyframe, streams deltas", async () => {
  const feed = new PassThrough();
  const ctx = ctxWith({ feeds: [() => feed] });
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    feed.write(KEY);
    feed.write(DELTA);
    await new Promise((r) => setImmediate(r));
    const st = sm.streamStatus("A");
    assert.equal(st.streaming, true);
    assert.equal(st.codec, "h264");
    assert.equal(st.width, 640);
    const res = fakeRes();
    sm.attachConsumer("A", res);
    assert.equal(res.chunks[0], KEY, "primed with last keyframe");
    feed.write(DELTA);
    await new Promise((r) => setImmediate(r));
    assert.equal(res.chunks.length, 2);
    assert.equal(ctx.state.streaming.has("A"), true);
  } finally {
    await sm.stopAll();
  }
});

test("consumer under backpressure drops until next keyframe", async () => {
  const feed = new PassThrough();
  const ctx = ctxWith({ feeds: [() => feed] });
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    const res = fakeRes();
    sm.attachConsumer("A", res);
    res.writableNeedDrain = true;
    feed.write(DELTA);
    await new Promise((r) => setImmediate(r));
    assert.equal(res.chunks.length, 1, "delta dropped while draining");
    res.writableNeedDrain = false;
    feed.write(DELTA);
    await new Promise((r) => setImmediate(r));
    assert.equal(res.chunks.length, 1, "still waiting for a keyframe");
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    assert.equal(res.chunks.length, 2, "resumed at keyframe");
  } finally {
    await sm.stopAll();
  }
});

test("stall → feed destroyed and reopened with backoff; gap → consumers ended; exit after continuous failure", async () => {
  const f1 = new PassThrough(), f2 = new PassThrough();
  let exited = 0;
  const ctx = ctxWith({ feeds: [() => f1, () => f2], exit: () => exited++ });
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    const slot = ctx.state.slots.get("A");
    const t0 = 1_000_000;
    f1.write(KEY);
    await new Promise((r) => setImmediate(r));
    // Pretend f1 has been up and silent since t0 on both counts — lastBytesAt AND startedAt — so the
    // fixed silence calc (Math.max of the two) measures against our fake clock, not real Date.now().
    slot.lastBytesAt = t0;
    slot.startedAt = t0;
    const res = fakeRes();
    sm.attachConsumer("A", res);
    sm.streamTick(t0 + 13_000); // > stallMs
    assert.equal(sm.streamStatus("A").stalls, 1);
    assert.equal(f1.destroyed, true);
    // slot.feed is now undefined (closeFeed ran synchronously above) and the reopen is only scheduled
    // (setTimeout backoff), not yet fired — check the gap here, before awaiting that reopen, so the
    // stall check (which requires slot.feed) does not also re-fire on the same stale silence.
    sm.streamTick(t0 + 46_000); // > gapMs with no bytes → consumers dropped, no additional stall
    assert.equal(sm.streamStatus("A").stalls, 1, "gap tick alone must not double-count as a stall");
    assert.equal(res.destroyed, true);
    await waitUntil(() => ctx.opens() === 2); // backoff 10ms → reopen (poll instead of a fixed sleep)
    assert.equal(ctx.opens(), 2);
    assert.equal(slot.feed, f2);
    slot.firstFailureAt = t0;
    sm.streamTick(t0 + 301_000);
    assert.equal(exited, 1, "exit(1) requested after exitAfterMs of failure");
  } finally {
    await sm.stopAll();
  }
});

test("reopened feed gets a fresh stall window instead of being re-stalled immediately", async () => {
  const f1 = new PassThrough(), f2 = new PassThrough();
  const ctx = ctxWith({ feeds: [() => f1, () => f2] });
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    const slot = ctx.state.slots.get("A");
    const t0 = 1_000_000;
    f1.write(KEY);
    await new Promise((r) => setImmediate(r));
    slot.lastBytesAt = t0;
    slot.startedAt = t0;
    sm.streamTick(t0 + 13_000); // stall #1
    assert.equal(sm.streamStatus("A").stalls, 1);
    assert.equal(f1.destroyed, true);
    await waitUntil(() => ctx.opens() === 2); // backoff 10ms → reopen
    assert.equal(slot.feed, f2);
    const startedAt = slot.startedAt; // real Date.now(), stamped by ensureWarm on this reopen
    sm.streamTick(startedAt + 2_000); // well within a fresh stallMs window
    assert.equal(sm.streamStatus("A").stalls, 1, "fresh feed must not be re-stalled immediately");
    assert.equal(f2.destroyed, false);
    sm.streamTick(startedAt + 13_000); // genuinely silent for stallMs → stalls normally, like any feed
    assert.equal(sm.streamStatus("A").stalls, 2);
    assert.equal(f2.destroyed, true);
  } finally {
    await sm.stopAll();
  }
});

test("blocked camera is still opened and retried (the p2pConnect re-check is the only way to unblock)", async () => {
  const feed = new PassThrough();
  const ctx = ctxWith({ feeds: [() => feed] });
  ctx.isBlocked = () => true;
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    assert.equal(ctx.opens(), 1, "opened despite being blocked");
    const slot = ctx.state.slots.get("A");
    assert.equal(slot.feed, feed);
    feed.destroy(); // guard closed the WAN session → feed closes → normal backoff retry
    await waitUntil(() => ctx.opens() === 2);
    assert.equal(ctx.opens(), 2, "retried with backoff while blocked");
  } finally {
    await sm.stopAll();
  }
});

test("blocked slot failing for longer than exitAfterMs does not exit the process", async () => {
  let exited = 0;
  const ctx = ctxWith({ feeds: [() => { throw new Error("wan session closed"); }], exit: () => exited++ });
  const blocked = new Set(["A"]);
  ctx.isBlocked = (sn) => blocked.has(sn);
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    const slot = ctx.state.slots.get("A");
    assert.ok(slot.failures >= 1);
    const t0 = 1_000_000;
    slot.firstFailureAt = t0;
    sm.streamTick(t0 + 301_000);
    assert.equal(exited, 0, "blocked slot excluded from the exit-after-stall check");
    assert.equal(slot.firstFailureAt, 0, "failure clock reset while blocked");
    // Once unblocked the normal rule applies again from the next failure onwards.
    blocked.clear();
    slot.firstFailureAt = t0;
    sm.streamTick(t0 + 301_000);
    assert.equal(exited, 1);
  } finally {
    await sm.stopAll();
  }
});

test("repeated open failures recreate the stream client (fresh session) every Nth failure", async () => {
  const state = createState();
  let opens = 0;
  const dropped = [];
  const ctx = {
    state,
    cfg: { stall: { stallMs: 12000, gapMs: 45000, exitAfterMs: 0, recreateClientAfter: 3, backoffMs: [5] } },
    exit: () => {},
    getCamera: () => ({ enabled: true, stationSn: "STN", powered: true }),
    isBlocked: () => false,
    applyPins: async () => {},
    sdk: {
      streamClientFor: async () => ({}),
      dropStreamClient: async (sn, stationSn) => { dropped.push(stationSn); return true; },
      openFeed: async () => { opens++; throw new Error("P2P connect timeout"); }, // always fails
      extractParamSets: () => undefined,
      codedGeometry: () => undefined,
    },
  };
  const sm = createStreamManager(ctx);
  await sm.ensureWarm("A"); // failure #1 → schedules reopen (5ms backoff), repeat
  try {
    await waitUntil(() => dropped.length >= 1 && opens >= 4, 800);
    assert.ok(dropped.length >= 1, "client recreated after repeated failures");
    assert.equal(dropped[0], "STN", "dropped by station key");
    // Not dropped on the very first failure — only every Nth.
    assert.ok(opens >= 3, "several attempts happened before/at the recreate");
  } finally {
    await sm.stopAll();
  }
});

test("solo per-camera recovery: a failing camera tears down its own session and retries, without touching its sibling", async () => {
  const state = createState();
  const opens = { A: 0, B: 0 };
  const dropped = [];
  const cams = [
    { sn: "A", enabled: true, stationSn: "STN", powered: true },
    { sn: "B", enabled: true, stationSn: "STN", powered: true },
  ];
  const ctx = {
    state,
    cfg: { stall: { stallMs: 12000, gapMs: 45000, exitAfterMs: 0, recreateClientAfter: 1, backoffMs: [5] } },
    exit: () => {},
    listCameras: () => cams,
    getCamera: (sn) => cams.find((c) => c.sn === sn),
    isBlocked: () => false,
    applyPins: async () => {},
    sdk: {
      streamClientFor: async () => ({}),
      dropStreamClient: async (sn, station) => { dropped.push(sn); return true; },
      // A always fails to open; B opens fine (a live PassThrough).
      openFeed: async (_c, sn) => { opens[sn]++; if (sn === "A") throw new Error("A won't connect"); return new PassThrough(); },
      extractParamSets: () => undefined,
      codedGeometry: () => undefined,
    },
  };
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("B");           // B up (1 open, no failure → no teardown)
    await sm.ensureWarm("A");           // A fails → schedules solo reopen
    await waitUntil(() => opens.A >= 3, 800);
    assert.ok(opens.A >= 3, "A retried solo");
    assert.equal(opens.B, 1, "sibling B was never re-opened by A's recovery");
    assert.ok(dropped.every((sn) => sn === "A"), "only A's session was torn down, never B's");
    assert.ok(dropped.length >= 1, "A's session torn down before retry (teardown-to-refresh-port)");
  } finally {
    await sm.stopAll();
  }
});
