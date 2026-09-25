import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { WebSocket } from "ws";
import { createWsHub } from "../src/ws-hub.mjs";
import { createHolds } from "../src/holds.mjs";

/**
 * The event channel is how a tile learns that something moved. A poll would always be too late, so these
 * cover the properties a tile depends on: it is told the current state on connect (not just future
 * events), every event reaches every client, and a client going away never affects the others.
 */
async function withHub(t, cameras = [], options = {}) {
  const ctx = {
    cfg: { defaults: { holdSeconds: 60 } },
    state: { streaming: new Set(), timers: {} },
    listCameras: () => cameras,
    getCamera: (sn) => cameras.find((c) => c.sn === sn),
    ensureWarm: () => {},
    stopCamera: () => {},
  };
  ctx.holds = createHolds(ctx);
  const hub = createWsHub(ctx, options);
  ctx.broadcastEvent = (e) => hub.broadcast(e);
  const server = http.createServer((_, res) => res.end("ok"));
  hub.attach(server);
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const url = `ws://127.0.0.1:${server.address().port}/ws`;
  t.after(() => {
    hub.close();
    server.close();
  });
  return { ctx, hub, url };
}

/** Connect and collect messages; resolves once `want` have arrived (or the wait elapses). */
function collect(url, want, waitMs = 1500) {
  return new Promise((resolve, reject) => {
    const msgs = [];
    const ws = new WebSocket(url);
    const done = () => {
      ws.close();
      resolve(msgs);
    };
    const timer = setTimeout(done, waitMs);
    ws.on("message", (raw) => {
      msgs.push(JSON.parse(raw.toString()));
      if (msgs.length >= want) {
        clearTimeout(timer);
        done();
      }
    });
    ws.on("error", reject);
  });
}

const battery = { sn: "BATT", name: "Yard", enabled: true, mode: "on_motion", holdSeconds: 60 };

test("a joining client is told the current state, not just future events", async (t) => {
  const { ctx, url } = await withHub(t, [battery]);
  ctx.state.streaming.add("BATT");
  const [hello] = await collect(url, 1);
  assert.equal(hello.type, "hello");
  assert.deepEqual(hello.cameras, [{ sn: "BATT", name: "Yard", mode: "on_motion", codec: null, streamKey: "BATT", state: "live", still: false }]);
  assert.ok(hello.at > 0, "every message is timestamped so a replay is distinguishable from a live event");
});

test("motion, hold and streamState all reach a connected client", async (t) => {
  const { ctx, url } = await withHub(t, [battery]);
  const got = collect(url, 4);
  await new Promise((r) => setTimeout(r, 100)); // let the socket finish connecting
  ctx.broadcastEvent({ type: "motion", sn: "BATT", event: "motion" });
  ctx.holds.hold("BATT", "motion", 60); // emits a hold event
  ctx.broadcastEvent({ type: "streamState", sn: "BATT", state: "live" });
  const msgs = await got;
  assert.deepEqual(msgs.map((m) => m.type), ["hello", "motion", "hold", "streamState"]);
  assert.equal(msgs[2].owners[0], "motion");
});

test("every client gets every event", async (t) => {
  const { ctx, url } = await withHub(t, [battery]);
  const a = collect(url, 2);
  const b = collect(url, 2);
  await new Promise((r) => setTimeout(r, 100));
  ctx.broadcastEvent({ type: "motion", sn: "BATT", event: "motion" });
  const [ma, mb] = await Promise.all([a, b]);
  assert.equal(ma.at(-1).type, "motion");
  assert.equal(mb.at(-1).type, "motion");
});

test("a client that goes away is dropped and does not break broadcasting", async (t) => {
  const { ctx, hub, url } = await withHub(t, [battery]);
  const ws = new WebSocket(url);
  await new Promise((r) => ws.on("open", r));
  assert.equal(hub.clientCount(), 1);
  ws.terminate();
  await new Promise((r) => setTimeout(r, 100));
  assert.equal(hub.clientCount(), 0);
  assert.doesNotThrow(() => ctx.broadcastEvent({ type: "motion", sn: "BATT", event: "motion" }));
});

test("broadcasting with nobody connected is harmless", async (t) => {
  const { ctx } = await withHub(t, [battery]);
  const msg = ctx.broadcastEvent({ type: "motion", sn: "BATT", event: "motion" });
  assert.equal(msg.type, "motion");
  assert.ok(msg.at > 0);
});

// A wall renders a still only where one exists; without this it would point a pipeline at a 404 and the
// supervisor would restart it forever.
test("hello says which cameras have a retained thumbnail", async (t) => {
  const { ctx, url } = await withHub(t, [battery, { sn: "GAR", name: "Garage", enabled: true, mode: "always" }]);
  ctx.sdk = { snapshotStored: async (sn) => (sn === "BATT" ? Buffer.from("jpeg") : undefined) };
  const [hello] = await collect(url, 1);
  const still = Object.fromEntries(hello.cameras.map((c) => [c.sn, c.still]));
  assert.deepEqual(still, { BATT: true, GAR: false });
});

test("hello uses live codec and falls back to configured codec", async (t) => {
  const { ctx, url } = await withHub(t, [{ ...battery, codec: "h265" }, { sn: "GAR", name: "Garage", enabled: true, codec: "h264" }]);
  ctx.streamStatus = (sn) => sn === "GAR" ? { codec: "h265" } : {};
  const [hello] = await collect(url, 1);
  assert.deepEqual(hello.cameras.map((c) => c.codec), ["h265", "h265"]);
});

test("quiet connections receive application heartbeats and remain connected", async (t) => {
  const { hub, url } = await withHub(t, [], { heartbeatMs: 20 });
  const ws = new WebSocket(url);
  const messages = await new Promise((resolve, reject) => {
    const got = [];
    const timer = setTimeout(() => reject(new Error("heartbeat timeout")), 500);
    ws.on("message", (raw) => {
      got.push(JSON.parse(raw.toString()));
      if (got.length === 3) { clearTimeout(timer); resolve(got); }
    });
    ws.on("error", reject);
  });
  assert.deepEqual(messages.map((m) => m.type), ["hello", "heartbeat", "heartbeat"]);
  assert.ok(messages[2].at >= messages[1].at);
  assert.equal(ws.readyState, WebSocket.OPEN);
  assert.equal(hub.clientCount(), 1, "the socket remains live while the client answers control pings");
  ws.close();
});

test("a peer that stops answering pings is evicted within two heartbeat ticks", async (t) => {
  const { hub, url } = await withHub(t, [], { heartbeatMs: 20 });
  const ws = new WebSocket(url, { autoPong: false });
  await new Promise((resolve) => ws.on("open", resolve));
  await new Promise((resolve) => ws.on("close", resolve));
  assert.equal(hub.clientCount(), 0);
});

test("events arriving while hello is built are replayed after hello", async (t) => {
  const { ctx, url } = await withHub(t, [battery]);
  let release;
  ctx.sdk = { snapshotStored: () => new Promise((resolve) => { release = resolve; }) };
  const received = collect(url, 2, 1000);
  for (let i = 0; i < 50 && !release; i++) await new Promise((r) => setTimeout(r, 2));
  assert.ok(release, "hello snapshot is in progress");
  ctx.broadcastEvent({ type: "motion", sn: "BATT", event: "motion" });
  release(undefined);
  const messages = await received;
  assert.deepEqual(messages.map((m) => m.type), ["hello", "motion"]);
});

test("a failed hello snapshot closes only that connection", async (t) => {
  const { ctx, hub, url } = await withHub(t, []);
  ctx.listCameras = () => { throw new Error("inventory unavailable"); };
  const ws = new WebSocket(url);
  const code = await new Promise((resolve) => ws.on("close", resolve));
  assert.equal(code, 1011);
  assert.equal(hub.clientCount(), 0);
});
