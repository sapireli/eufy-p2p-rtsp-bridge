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
  const ctx = ctxWith({ peer: "203.0.113.9" });
  const g = createLanGuard(ctx);
  const client = new EventEmitter();
  g.attachLanGuard(client, "CAM1");
  client.emit("p2pConnect", "STATION1");
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(ctx.closed, ["STATION1"]);
  assert.equal(g.isBlocked("CAM1"), true);
  assert.match(ctx.state.blocked.get("CAM1"), /wan-path 203\.0\.113\.9/);

  const ctx2 = ctxWith({ peer: "192.168.1.50" });
  const g2 = createLanGuard(ctx2);
  const c2 = new EventEmitter();
  g2.attachLanGuard(c2, "CAM1");
  c2.emit("p2pConnect", "STATION1");
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(ctx2.closed, []);
  assert.equal(g2.isBlocked("CAM1"), false);
});

test("force off → WAN peer only warns; unknown address → unknown, not blocked", async () => {
  const ctx = ctxWith({ peer: "203.0.113.9", force: false });
  const g = createLanGuard(ctx);
  assert.equal(await g.checkSession({}, "S", "CAM1"), "ok");
  const ctx2 = ctxWith({ peer: undefined });
  assert.equal(await createLanGuard(ctx2).checkSession({}, "S", "CAM1"), "unknown");
  assert.equal(ctx2.state.blocked.size, 0);
});

test("a later LAN connect clears the block", async () => {
  let peer = "203.0.113.9";
  const ctx = ctxWith({ peer });
  ctx.sdk.sessionPeerHost = () => peer;
  const g = createLanGuard(ctx);
  await g.checkSession({}, "S", "CAM1");
  assert.equal(g.isBlocked("CAM1"), true);
  peer = "192.168.1.9";
  await g.checkSession({}, "S", "CAM1");
  assert.equal(g.isBlocked("CAM1"), false);
});
