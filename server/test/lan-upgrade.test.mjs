import { test } from "node:test";
import assert from "node:assert/strict";
import { createLanUpgrade } from "../src/lan-upgrade.mjs";
import { createState } from "../src/state.mjs";

// The climb from relay to direct LAN. What matters to a viewer is that it never tears down a session that
// is about to deliver video — a fallback restarts the camera, so a wrong call here is a visible outage.

function ctxWith({ streaming = [], starting = [], peer = "lan", windowMs = 1 } = {}) {
  const state = createState();
  for (const sn of streaming) state.streaming.add(sn);
  for (const sn of starting) state.starting.add(sn);
  const restarted = [];
  const ctx = {
    cfg: {
      lan: {
        cidr: "192.168.23.0/24",
        force: false,
        upgrade: { enabled: true, intervalMs: 120_000, windowMs, initialWindowMs: 14_000, maxBackoffMs: 600_000 },
      },
    },
    state,
    listCameras: () => [{ sn: "CAM", stationSn: "STA", enabled: true }],
    restartCamera: (sn) => restarted.push(sn),
    sdk: { dropStreamClient: async () => true },
  };
  const lu = createLanUpgrade(ctx);
  state.peerPath.set("STA", peer);
  return { lu, ctx, state, restarted };
}

/** Put the station into locked-LAN mode the way a real direct connect does. */
function lockLan(lu, state) {
  state.streaming.add("CAM");
  lu.onPeer("STA", "lan");
  state.streaming.delete("CAM");
}

/** Tick, let the (1 ms) grace window elapse, tick again — which is how a real timeout is reached. */
async function tickPastGrace(lu) {
  lu.tick(); // arms the grace deadline
  await new Promise((r) => setTimeout(r, 15));
  lu.tick(); // deadline now in the past
}

// The regression: a Floodlight Cam takes longer than the grace window to produce its first keyframe. If
// "not streaming yet" counts as a lost path, we drop the session that was about to deliver, the
// replacement warms from cold and loses the same race, and the camera never comes up at all.
test("a camera still warming does not count as a lost LAN path", async () => {
  const { lu, state, restarted } = ctxWith({ starting: ["CAM"] });
  lockLan(lu, state);
  assert.equal(lu.status().STA.mode, "lan");

  await tickPastGrace(lu);
  assert.equal(lu.status().STA.mode, "lan", "warming is a live session with no frames yet, not a dead path");
  assert.deepEqual(restarted, [], "tearing the camera down is exactly what must not happen here");
});

// The behaviour the grace window exists for must still work: a station with nothing streaming and
// nothing warming has genuinely lost its path.
test("a station that is neither streaming nor warming falls back to relay", async () => {
  const { lu, state, restarted } = ctxWith();
  lockLan(lu, state);
  state.streaming.clear();
  state.starting.clear();

  await tickPastGrace(lu);
  assert.equal(lu.status().STA.mode, "relay", "a dead path must fall back so the camera is not left dark");
  await new Promise((r) => setTimeout(r, 5));
  assert.deepEqual(restarted, ["CAM"], "fallback reopens the camera on the relay path");
});

test("a peer that goes WAN falls back even while it is streaming", async () => {
  const { lu, state } = ctxWith({ streaming: ["CAM"] });
  lockLan(lu, state);
  state.streaming.add("CAM");
  state.peerPath.set("STA", "wan");

  await tickPastGrace(lu);
  assert.equal(lu.status().STA.mode, "relay", "bytes over the relay are still a lost DIRECT path");
});
