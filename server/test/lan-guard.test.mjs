import { test } from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { createLanGuard, inCidr } from "../src/lan-guard.mjs";
import { createState } from "../src/state.mjs";

test("inCidr", () => {
  assert.equal(inCidr("192.168.1.77", "192.168.1.0/24"), true);
  assert.equal(inCidr("192.168.2.1", "192.168.1.0/24"), false);
  assert.equal(inCidr("10.9.8.7", "10.0.0.0/8"), true);
  assert.equal(inCidr("203.0.113.9", "10.0.0.0/8"), false);
  assert.equal(inCidr("garbage", "10.0.0.0/8"), false);
});

function ctxWith({ peer, force = true, cidr = "192.168.1.0/24" }) {
  const closed = [];
  return {
    closed,
    cfg: { lan: { cidr, force, stationAddresses: {} } },
    state: createState(),
    sdk: { sessionPeerHost: () => peer, closeSession: async (c, st) => closed.push(st) },
  };
}

test("WAN peer with force → session closed and camera blocked; LAN peer → ok", async () => {
  const logs = [];
  const origError = console.error;
  const origLog = console.log;
  console.error = (...args) => logs.push(args.join(" "));
  console.log = (...args) => logs.push(args.join(" ")); // capture the "P2P peer … (LAN)" line too
  try {
    const ctx = ctxWith({ peer: "203.0.113.9" });
    const g = createLanGuard(ctx);
    const client = new EventEmitter();
    g.attachLanGuard(client, "CAM1");
    await g.checkSession(client, "STATION1", "CAM1");
    assert.deepEqual(ctx.closed, ["STATION1"]);
    assert.equal(g.isBlocked("CAM1"), true);
    assert.match(ctx.state.blocked.get("CAM1"), /wan-path 203\.0\.113\.9/);
    assert.equal(logs.some((l) => l.includes("closing session")), true);

    const ctx2 = ctxWith({ peer: "192.168.1.50" });
    const g2 = createLanGuard(ctx2);
    const c2 = new EventEmitter();
    g2.attachLanGuard(c2, "CAM1");
    await g2.checkSession(c2, "STATION1", "CAM1");
    assert.deepEqual(ctx2.closed, []);
    assert.equal(g2.isBlocked("CAM1"), false);
  } finally {
    console.error = origError;
    console.log = origLog;
  }
});

test("force off → WAN peer only warns; unknown address → unknown, not blocked", async () => {
  const logs = [];
  const origWarn = console.warn;
  console.warn = (...args) => logs.push(args.join(" "));
  try {
    const ctx = ctxWith({ peer: "203.0.113.9", force: false });
    const g = createLanGuard(ctx);
    assert.equal(await g.checkSession({}, "S", "CAM1"), "ok");
    const ctx2 = ctxWith({ peer: undefined });
    assert.equal(await createLanGuard(ctx2).checkSession({}, "S", "CAM1"), "unknown");
    assert.equal(ctx2.state.blocked.size, 0);
  } finally {
    console.warn = origWarn;
  }
});

test("a later LAN connect clears the block", async () => {
  const logs = [];
  const origLog = console.log, origError = console.error;
  console.log = (...args) => logs.push(args.join(" "));
  console.error = () => {}; // the initial WAN block message is asserted in the first test
  try {
    let peer = "203.0.113.9";
    const ctx = ctxWith({ peer });
    ctx.sdk.sessionPeerHost = () => peer;
    const g = createLanGuard(ctx);
    await g.checkSession({}, "S", "CAM1");
    assert.equal(g.isBlocked("CAM1"), true);
    peer = "192.168.1.9";
    await g.checkSession({}, "S", "CAM1");
    assert.equal(g.isBlocked("CAM1"), false);
    assert.equal(logs.some((l) => l.includes("LAN path restored")), true);
  } finally {
    console.log = origLog;
    console.error = origError;
  }
});

test("sessionPeerHost throwing → checkSession returns unknown, no rejection", async () => {
  const logs = [];
  const origError = console.error;
  console.error = (...args) => logs.push(args.join(" "));
  try {
    const ctx = ctxWith({ peer: "192.168.1.1" });
    ctx.sdk.sessionPeerHost = () => { throw new Error("client mid-teardown"); };
    const g = createLanGuard(ctx);
    const result = await g.checkSession({}, "S", "CAM1");
    assert.equal(result, "unknown");
    assert.equal(g.isBlocked("CAM1"), false);
    assert.equal(logs.some((l) => l.includes("LAN check failed")), true);
  } finally {
    console.error = origError;
  }
});

test("attachLanGuard judges sessions that connected before it was listening (pins open the session first)", async () => {
  const logs = [];
  const origError = console.error;
  console.error = (...args) => logs.push(args.join(" "));
  try {
    const ctx = ctxWith({ peer: "203.0.113.9" });
    const g = createLanGuard(ctx);
    const client = new EventEmitter();
    client.getP2pSessions = () => new Map([["STATION1", {}]]);
    g.attachLanGuard(client, "CAM1");
    await new Promise((r) => setImmediate(r));
    assert.equal(g.isBlocked("CAM1"), true, "pre-existing WAN session blocked on attach");
    assert.deepEqual(ctx.closed, ["STATION1"]);
    // Attaching is idempotent and a client without getP2pSessions (or with none) is fine.
    g.attachLanGuard(client, "CAM1");
    g.attachLanGuard(new EventEmitter(), "CAM2");
    await new Promise((r) => setImmediate(r));
    assert.deepEqual(ctx.closed, ["STATION1"]);
  } finally {
    console.error = origError;
  }
});

test("forced LAN checks a media session against the configured CIDR before serving its feed", async () => {
  const ctx = ctxWith({ peer: undefined });
  const peers = new Map([["STATION#live:1", "10.0.0.5"]]);
  ctx.sdk.sessionPeerHost = (_client, key) => peers.get(key);
  const client = { getP2pSessions: () => new Map([["STATION#live:1", {}]]) };
  const guard = createLanGuard(ctx);
  const originalError = console.error;
  console.error = () => {};
  try {
    assert.equal(await guard.checkMediaSessions(client, "STATION", "CAM"), "blocked");
    assert.deepEqual(ctx.closed, ["STATION#live:1"]);
    assert.equal(guard.isBlocked("CAM"), true);
    peers.set("STATION#live:1", "192.168.1.5");
    assert.equal(await guard.checkMediaSessions(client, "STATION", "CAM"), "ok");
    assert.equal(guard.isBlocked("CAM"), false);
  } finally {
    console.error = originalError;
  }
});

test("a media session that connects after the feed opens is checked on connection", async () => {
  const ctx = ctxWith({ peer: undefined });
  const session = new EventEmitter();
  let host;
  ctx.sdk.sessionPeerHost = () => host;
  const client = { getP2pSessions: () => new Map([["STATION#live:1", session]]) };
  const guard = createLanGuard(ctx);
  const originalError = console.error;
  console.error = () => {};
  try {
    assert.equal(await guard.checkMediaSessions(client, "STATION", "CAM"), "unknown");
    host = "10.0.0.5";
    session.emit("connect");
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(ctx.closed, ["STATION#live:1"]);
  } finally {
    console.error = originalError;
  }
});

test("a media session present when the guard attaches is checked under its station key", async () => {
  const ctx = ctxWith({ peer: undefined });
  const client = new EventEmitter();
  client.getP2pSessions = () => new Map([["STATION#live:1", {}]]);
  ctx.sdk.sessionPeerHost = (_client, key) => key === "STATION#live:1" ? "10.0.0.5" : undefined;
  const originalError = console.error;
  console.error = () => {};
  try {
    createLanGuard(ctx).attachLanGuard(client, "CAM");
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(ctx.closed, ["STATION#live:1"]);
  } finally {
    console.error = originalError;
  }
});
