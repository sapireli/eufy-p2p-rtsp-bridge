import { test } from "node:test";
import assert from "node:assert/strict";
import { createHolds } from "../src/holds.mjs";

/**
 * A camera that is not always-on streams for exactly as long as something holds it. These cover the
 * properties the rest of Phase 2 leans on: holds compose across owners, they always expire, and an
 * always-on camera is never stopped by one going away.
 */
function ctxWith(cameras, { now = () => Date.now() } = {}) {
  const started = [];
  const stopped = [];
  const events = [];
  const ctx = {
    cfg: { defaults: { holdSeconds: 60 } },
    state: { timers: {} },
    getCamera: (sn) => cameras[sn],
    ensureWarm: (sn) => started.push(sn),
    stopCamera: (sn) => stopped.push(sn),
    broadcastEvent: (e) => events.push(e),
    now,
  };
  return { ctx, started, stopped, events, holds: createHolds(ctx) };
}

const battery = { sn: "BATT", enabled: true, mode: "on_motion", holdSeconds: 60 };
const wired = { sn: "WIRED", enabled: true, mode: "always" };

test("a hold starts the stream, and only the first one does", () => {
  const { holds, started } = ctxWith({ BATT: battery });
  holds.hold("BATT", "motion", 60);
  holds.hold("BATT", "client-1", 60);
  assert.deepEqual(started, ["BATT"], "the camera is already streaming for the second holder");
  assert.equal(holds.isHeld("BATT"), true);
  assert.deepEqual(holds.owners("BATT").sort(), ["client-1", "motion"]);
});

test("the stream stops only when the LAST owner releases", () => {
  const { holds, stopped } = ctxWith({ BATT: battery });
  holds.hold("BATT", "motion", 60);
  holds.hold("BATT", "client-1", 60);

  holds.release("BATT", "motion");
  assert.deepEqual(stopped, [], "still held by the client");

  holds.release("BATT", "client-1");
  assert.deepEqual(stopped, ["BATT"]);
  assert.equal(holds.isHeld("BATT"), false);
});

test("holds expire, which stops the camera", () => {
  const { holds, stopped } = ctxWith({ BATT: battery });
  holds.hold("BATT", "motion", 0.01); // 10ms
  assert.equal(holds.isHeld("BATT"), true);
  const until = Date.now() + 25;
  while (Date.now() < until); // busy-wait: the expiry is wall-clock, not a mocked timer
  holds.tick();
  assert.deepEqual(stopped, ["BATT"]);
});

// Repeated motion during one event should keep the camera up for holdSeconds past the LAST movement,
// not accumulate an ever-longer stream.
test("re-holding extends to the later deadline rather than accumulating", () => {
  const { holds } = ctxWith({ BATT: battery });
  const first = holds.hold("BATT", "motion", 1);
  const second = holds.hold("BATT", "motion", 60);
  assert.ok(second > first, "a longer hold pushes the deadline out");
  const third = holds.hold("BATT", "motion", 1);
  assert.equal(third, second, "a shorter one does not pull it back in");
});

test("an always-on camera is never stopped by a hold expiring", () => {
  const { holds, stopped } = ctxWith({ WIRED: wired });
  holds.hold("WIRED", "client-1", 0.01);
  holds.release("WIRED", "client-1");
  assert.deepEqual(stopped, [], "always-on means always-on, whoever was holding it");
});

test("a disabled camera cannot be held", () => {
  const { holds, started } = ctxWith({ OFF: { sn: "OFF", enabled: false, mode: "on_motion" } });
  assert.equal(holds.hold("OFF", "motion", 60), 0);
  assert.deepEqual(started, []);
});

// The SDK bounds a battery camera's continuous stream and warns before auto-stopping it. Extending is
// only right while something still wants the camera; otherwise the notice is correct and we let it stop.
test("the battery budget is extended while held, and not otherwise", () => {
  const { holds } = ctxWith({ BATT: battery });
  let extended = 0;
  const notice = { extend: () => extended++ };

  assert.equal(holds.onBudgetNotice("BATT", notice), false, "nothing holds it");
  assert.equal(extended, 0);

  holds.hold("BATT", "motion", 60);
  assert.equal(holds.onBudgetNotice("BATT", notice), true);
  assert.equal(extended, 1);
});

test("a failing extend is reported rather than pretending the stream continues", () => {
  const { holds } = ctxWith({ BATT: battery });
  holds.hold("BATT", "motion", 60);
  const origErr = console.error;
  console.error = () => {};
  try {
    const ok = holds.onBudgetNotice("BATT", {
      extend: () => {
        throw new Error("session gone");
      },
    });
    assert.equal(ok, false);
  } finally {
    console.error = origErr;
  }
});

test("taking and releasing a hold is broadcast, so a client can follow it", () => {
  const { holds, events } = ctxWith({ BATT: battery });
  holds.hold("BATT", "motion", 60);
  holds.release("BATT", "motion");
  const holdEvents = events.filter((e) => e.type === "hold");
  assert.equal(holdEvents.length, 2);
  assert.deepEqual(holdEvents[0].owners, ["motion"]);
  assert.deepEqual(holdEvents[1].owners, []);
});
