import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { PassThrough } from "node:stream";
import { createStreamManager } from "../src/stream-manager.mjs";
import { createState } from "../src/state.mjs";
import { createFreshness } from "../src/freshness.mjs";

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

test("warm feed sniffs codec/geometry, starts a late consumer at the next live keyframe", async () => {
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
    assert.equal(res.chunks.length, 0, "stale keyframe is not replayed");
    feed.write(DELTA);
    await new Promise((r) => setImmediate(r));
    assert.equal(res.chunks.length, 0, "dependent frames before a fresh keyframe are skipped");
    feed.write(KEY);
    feed.write(DELTA);
    await new Promise((r) => setImmediate(r));
    assert.deepEqual(res.chunks, [KEY, DELTA]);
    assert.equal(ctx.state.streaming.has("A"), true);
  } finally {
    await sm.stopAll();
  }
});

function tsPts(buffer) {
  for (let offset = 0; offset < buffer.length; offset += 188) {
    const packet = buffer.subarray(offset, offset + 188);
    if ((((packet[1] & 31) << 8) | packet[2]) !== 0x100 || !(packet[1] & 0x40)) continue;
    const start = 4 + ((packet[3] & 0x20) ? packet[4] + 1 : 0);
    const b = packet.subarray(start + 9, start + 14);
    return ((b[0] >> 1) & 7) * 2 ** 30 + b[1] * 2 ** 22 + (b[2] >> 1) * 2 ** 15 + b[3] * 2 ** 7 + (b[4] >> 1);
  }
  throw new Error("no video PES start");
}

test("object-mode feeds preserve the source clock for TS while raw consumers receive unchanged bytes", async () => {
  const feed = new PassThrough({ objectMode: true });
  const ctx = ctxWith({ feeds: [() => feed] });
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    const raw = fakeRes(), ts = fakeRes();
    sm.attachConsumer("A", raw);
    sm.attachConsumer("A", ts, { format: "mpegts" });
    feed.write({ data: DELTA, sourceTimestamp: 900, keyframe: false });
    assert.equal(ts.chunks.length, 0);
    feed.write({ data: KEY, sourceTimestamp: 1000, keyframe: true });
    // An arrival burst must not turn these regular source timestamps into equal PTS.
    feed.write({ data: DELTA, sourceTimestamp: 1067, keyframe: false });
    feed.write({ data: DELTA, sourceTimestamp: 1134, keyframe: false });
    assert.deepEqual(raw.chunks, [KEY, DELTA, DELTA]);
    assert.equal(ts.chunks.length, 3);
    assert.equal(tsPts(ts.chunks[1]) - tsPts(ts.chunks[0]), 67 * 90);
    assert.equal(tsPts(ts.chunks[2]) - tsPts(ts.chunks[1]), 67 * 90);
    feed.write({ data: DELTA, sourceTimestamp: 10, keyframe: false });
    assert.equal(ts.destroyed, true, "a backwards source clock closes only its timestamped consumer");
    assert.equal(raw.destroyed, false);
    assert.equal(raw.chunks.length, 4);
  } finally { await sm.stopAll(); }
});

test("SDK envelopes preserve receipt age and replay identity through consumer fanout", async () => {
  const feed = new PassThrough({ objectMode: true });
  const ctx = ctxWith({ feeds: [() => feed] });
  let now = 1_000_000n;
  ctx.freshness = createFreshness({ cfg: { freshness: {} } }, { clock: () => now });
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    const raw = fakeRes(), timed = fakeRes(), stamps = [];
    sm.attachConsumer("A", raw);
    sm.attachConsumer("A", timed, { onFrame: (frame, stamp) => stamps.push({ frame, stamp }) });
    const frame = { data: KEY, sourceTimestamp: 1000, keyframe: true, codec: "h264" };
    const receipt = Object.freeze({ sourceId: "pull-A", sequence: 1n, monotonicUs: now });
    feed.write(Object.freeze({ frame, receipt }));
    now = 6_000_000n;
    feed.write(Object.freeze({ frame, receipt }));
    assert.deepEqual(raw.chunks, [KEY, KEY]);
    assert.equal(stamps.length, 2);
    assert.equal(stamps[0].frame, frame);
    assert.equal(stamps[1].stamp, stamps[0].stamp);
    assert.equal(stamps[1].stamp.ingressMonoUs, 1_000_000n);
    assert.equal(sm.streamStatus("A").sourceReceipt.ageMs, 5000);
    assert.equal(sm.streamStatus("A").sourceReceipt.frameId, String(stamps[0].stamp.frameId));
  } finally { await sm.stopAll(); }
});

test("invalid SDK envelopes close their feed without escaping its error handler", async () => {
  const feed = new PassThrough({ objectMode: true });
  const ctx = ctxWith({ feeds: [() => feed] });
  ctx.freshness = createFreshness({ cfg: { freshness: {} } }, { clock: () => 100n });
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    feed.write({ frame: { data: KEY, sourceTimestamp: 1 }, receipt: { sourceId: "A", sequence: 1n, monotonicUs: 101n } });
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(feed.destroyed, true);
    assert.equal(ctx.state.streaming.has("A"), false);
  } finally { await sm.stopAll(); }
});

test("a blocked media peer is closed before its feed is served", async () => {
  const feed = new PassThrough();
  const ctx = ctxWith({ feeds: [() => feed] });
  ctx.checkMediaSessions = async () => "blocked";
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    assert.equal(feed.destroyed, true);
    assert.equal(ctx.state.slots.get("A").feed, undefined);
    assert.equal(ctx.opens(), 1);
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
    assert.equal(res.chunks.length, 0, "delta dropped while draining");
    res.writableNeedDrain = false;
    feed.write(DELTA);
    await new Promise((r) => setImmediate(r));
    assert.equal(res.chunks.length, 0, "still waiting for a keyframe");
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    assert.equal(res.chunks.length, 1, "resumed at keyframe");
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

test("playback stalls after five seconds while reopened feeds retain their first-media window", async () => {
  const f1 = new PassThrough(), f2 = new PassThrough();
  const ctx = ctxWith({ feeds: [() => f1, () => f2] });
  ctx.cfg.stall.stallMs = 5000;
  ctx.cfg.stall.startupStallMs = 30000;
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    const slot = ctx.state.slots.get("A");
    const start = slot.startedAt;
    sm.streamTick(start + 6000);
    assert.equal(f1.destroyed, false, "new feed must not use the playback deadline");
    f1.write(KEY);
    const mediaAt = slot.lastBytesAt;
    sm.streamTick(mediaAt + 4999);
    assert.equal(f1.destroyed, false);
    sm.streamTick(mediaAt + 5000);
    assert.equal(f1.destroyed, true, "established feed must restart after five seconds of silence");
    await waitUntil(() => ctx.opens() === 2);
    assert.equal(slot.feed, f2);
    sm.streamTick(slot.startedAt + 6000);
    assert.equal(f2.destroyed, false, "previous feed media must not shorten the new startup window");
    sm.streamTick(slot.startedAt + 30000);
    assert.equal(f2.destroyed, true, "startup window remains bounded");
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

// Parameter sets are read from a chunk, and a keyframe whose SPS straddles a chunk boundary parses into a
// plausible-looking but WRONG size. Measured on the Balcony camera: 13 "changes" between 1080p and 720p in
// one session, while a capture of that very stream was 1280x720 from end to end. Acting on those tore
// consumers off and re-synced go2rtc over and over, which is what a viewer saw as a frozen picture.
test("a one-off geometry misparse is ignored; a real change is acted on once confirmed", async () => {
  const feed = new PassThrough();
  let geom = { width: 640, height: 480 };
  const ctx = ctxWith({ feeds: [() => feed] });
  ctx.sdk.codedGeometry = () => geom;
  let syncs = 0;
  ctx.syncGo2rtc = async () => { syncs += 1; };
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    assert.equal(sm.streamStatus("A").width, 640, "the first reading is taken as-is");
    const syncsAfterFirst = syncs;

    // A single disagreeing keyframe — a misparse. It must not move the reported geometry.
    geom = { width: 1920, height: 1080 };
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    assert.equal(sm.streamStatus("A").width, 640, "one odd reading is not a resolution change");
    assert.equal(syncs, syncsAfterFirst, "and must not re-sync go2rtc");

    // Back to the real size: the pending reading is refuted and dropped.
    geom = { width: 640, height: 480 };
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    assert.equal(sm.streamStatus("A").width, 640);

    // A genuine change persists across keyframes, so the second agreeing reading is believed.
    geom = { width: 1280, height: 720 };
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    assert.equal(sm.streamStatus("A").width, 640, "not yet — one reading proves nothing");
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    assert.equal(sm.streamStatus("A").width, 1280, "confirmed by the next keyframe");
    assert.ok(syncs > syncsAfterFirst, "a real change does re-sync go2rtc");
  } finally {
    await sm.stopAll();
  }
});

test("dynamic VA-API keeps one consumer across same-aspect input size changes", async () => {
  const feed = new PassThrough();
  let geom = { width: 1280, height: 720 };
  const ctx = ctxWith({ feeds: [() => feed] });
  ctx.platform = "linux";
  ctx.cfg.go2rtcTranscode = "always";
  ctx.cfg.go2rtcVaapiDevice = "/dev/dri/renderD128";
  ctx.cfg.go2rtcMaxHeight = 720;
  ctx.sdk.extractParamSets = () => ({ codec: "h265", sps: [], pps: [] });
  ctx.sdk.codedGeometry = () => geom;
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    const res = fakeRes();
    sm.attachConsumer("A", res);
    geom = { width: 960, height: 540 };
    feed.write(KEY);
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    assert.equal(sm.streamStatus("A").width, 960);
    assert.equal(res.destroyed, false);
    assert.ok(res.chunks.length >= 2);

    geom = { width: 1024, height: 768 };
    feed.write(KEY);
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    assert.equal(res.destroyed, true, "different output aspect ratio needs a fresh SDP");
  } finally {
    await sm.stopAll();
  }
});

test("passthrough still reconnects on a confirmed input size change", async () => {
  const feed = new PassThrough();
  let geom = { width: 1280, height: 720 };
  const ctx = ctxWith({ feeds: [() => feed] });
  ctx.platform = "linux";
  ctx.cfg.go2rtcTranscode = "always";
  ctx.cfg.go2rtcVaapiDevice = "/dev/dri/renderD128";
  ctx.cfg.go2rtcMaxHeight = 720;
  ctx.getCamera = () => ({ enabled: true, transcode: "never" });
  ctx.sdk.extractParamSets = () => ({ codec: "h265", sps: [], pps: [] });
  ctx.sdk.codedGeometry = () => geom;
  const sm = createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    const res = fakeRes();
    sm.attachConsumer("A", res);
    geom = { width: 960, height: 540 };
    feed.write(KEY);
    feed.write(KEY);
    await new Promise((r) => setImmediate(r));
    assert.equal(res.destroyed, true);
  } finally {
    await sm.stopAll();
  }
});


test("learned HEVC PTL is cached and metadata changes refresh clients without opening another source", async () => {
  const feed=new PassThrough(),ctx=ctxWith({feeds:[()=>feed]});
  let sps=Buffer.from("QgEBIUAAAAMAkAAAAwAAAwCWoAKAgC0WeluRKFzcBAQEEAAAAwAQAAADAZCA","base64"),refreshes=0;
  ctx.sdk.extractParamSets=()=>({codec:"h265",sps:[sps],pps:[],vps:[]});
  ctx.sdk.codedGeometry=()=>({width:1920,height:1080});
  ctx.syncGo2rtc=async()=>{};
  ctx.ws={broadcastSnapshot:async()=>{refreshes++;}};
  const sm=createStreamManager(ctx);
  try {
    await sm.ensureWarm("A");feed.write(KEY);
    await new Promise(resolve=>setImmediate(resolve));
    assert.deepEqual(ctx.state.slots.get("A").nativeCodecInfo,{profile:1,level:150,tier:1});
    feed.write(KEY);assert.equal(refreshes,0,"unchanged SPS does not repeatedly broadcast");
    sps=Buffer.from(sps);sps[3]&=~32;feed.write(KEY);
    await new Promise(resolve=>setImmediate(resolve));
    assert.deepEqual(ctx.state.slots.get("A").nativeCodecInfo,{profile:1,level:150,tier:0});
    assert.equal(refreshes,1);assert.equal(ctx.opens(),1);
    sps=Buffer.from([0x42,1]);feed.write(KEY);
    assert.equal(ctx.state.slots.get("A").nativeCodecInfo,undefined,"malformed replacement clears stale capability");
  } finally {await sm.stopAll();}
});


test("opt-in consumer clocks separate bridge arrival from synchronous parameter processing without receipt rebirth", async () => {
  const feed=new PassThrough({objectMode:true});
  const ctx=ctxWith({feeds:[()=>feed]});
  let now=2000000n, extraClockReads=0;
  ctx.freshness=createFreshness(ctx,{epoch:Buffer.alloc(8,3),clock:()=>now});
  const realNow=ctx.freshness.nowUs;
  ctx.freshness.nowUs=()=>{extraClockReads++;return realNow();};
  const extract=ctx.sdk.extractParamSets;
  ctx.sdk.extractParamSets=b=>{now+=90000n;return extract(b);};
  const sm=createStreamManager(ctx);const res=fakeRes();res._ewbTimingEnabled=true;
  const calls=[];
  try {
    sm.attachConsumer("A",res,{onFrame:(...args)=>calls.push(args)});
    await sm.ensureWarm("A");
    const sourceReceipt={sourceId:"sdk-source",sequence:1n,monotonicUs:1500000n};
    feed.write({frame:{data:KEY,codec:"h264",sourceTimestamp:1000},receipt:sourceReceipt});
    await new Promise(resolve=>setImmediate(resolve));
    assert.equal(calls.length,1);
    const [,stamp,timing]=calls[0];
    assert.equal(stamp.ingressMonoUs,1500000n);
    assert.equal(timing.bridgeEntryUs,2000000n);
    assert.equal(timing.consumerDispatchUs,2090000n);
    assert.equal(timing.sourceId,"sdk-source");
    assert.equal(timing.sourceSequence,1n);
    assert.equal(extraClockReads,2);
    res._ewbTimingEnabled=false;
    feed.write({frame:{data:DELTA,codec:"h264",sourceTimestamp:1067},receipt:{...sourceReceipt,sequence:2n,monotonicUs:2000000n}});
    await new Promise(resolve=>setImmediate(resolve));
    assert.equal(calls[1][2],undefined);
    assert.equal(extraClockReads,2,"disabled diagnostics add no bridge clock reads");
  } finally {await sm.stopAll();}
});
