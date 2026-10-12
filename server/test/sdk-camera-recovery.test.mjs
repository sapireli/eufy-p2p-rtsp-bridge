import test from "node:test";
import assert from "node:assert/strict";
import { createSdk } from "../src/sdk-adapter.mjs";

test("camera recovery delegates exact serial and never closes prefix-matched sibling sessions", async () => {
  const { eufy, sdk } = createSdk({
    cfg: {
      email: "synthetic@example.com",
      password: "synthetic",
      country: "US",
      session: "/tmp/synthetic-sdk-session",
      cameras: {},
      lan: { stationAddresses: {} },
    },
    DEBUG: false,
    hooks: {},
  });
  const calls = [];
  eufy.getP2pSessions = () =>
    new Map([
      [
        "STATION",
        {
          close: () => {
            throw Error("base closed");
          },
        },
      ],
      [
        "STATION#live:2",
        {
          close: () => {
            throw Error("sibling closed");
          },
        },
      ],
    ]);
  eufy.resetCameraStream = async (sn) => {
    calls.push(sn);
    return true;
  };
  assert.equal(await sdk.dropStreamClient("CAMERA", "STATION"), true);
  assert.deepEqual(calls, ["CAMERA"]);
});

test("an old SDK cannot fall back to station-wide recovery", async () => {
  const { eufy, sdk } = createSdk({
    cfg: {
      email: "synthetic@example.com",
      password: "synthetic",
      country: "US",
      session: "/tmp/synthetic-sdk-session",
      cameras: {},
      lan: { stationAddresses: {} },
    },
    DEBUG: false,
    hooks: {},
  });
  eufy.resetCameraStream = undefined;
  await assert.rejects(
    sdk.dropStreamClient("CAMERA", "STATION"),
    /resetCameraStream is required/,
  );
});

import { PassThrough } from "node:stream";
import { createStreamManager } from "../src/stream-manager.mjs";
import { createState, newSlot } from "../src/state.mjs";

test("recovery awaits owner cleanup and coalesces concurrent warm requests before acquisition", async () => {
  let release;
  const cleanup = new Promise((r) => {
    release = r;
  });
  const calls = [];
  const camera = {
    sn: "A",
    stationSn: "STATION",
    enabled: true,
    mode: "always",
  };
  const state = createState();
  const sm = createStreamManager({
    state,
    cfg: {
      stall: {
        recreateClientAfter: 1,
        backoffMs: [1000],
        stallMs: 12000,
        gapMs: 45000,
        exitAfterMs: 0,
      },
    },
    getCamera: () => camera,
    isBlocked: () => false,
    sdk: {
      dropStreamClient: async (sn) => {
        calls.push("reset:" + sn);
        await cleanup;
        calls.push("reset-done");
        return true;
      },
      streamClientFor: async () => {
        calls.push("client");
        return {};
      },
      openFeed: async () => {
        calls.push("open");
        return new PassThrough();
      },
      extractParamSets: () => undefined,
      codedGeometry: () => undefined,
    },
  });
  try {
    state.slots.set("A", newSlot("A"));
    state.slots.get("A").failures = 1;
    const warm = sm.ensureWarm("A");
    await new Promise((r) => setImmediate(r));
    await sm.ensureWarm("A");
    assert.deepEqual(calls, ["reset:A"]);
    release();
    await warm;
    assert.deepEqual(calls, ["reset:A", "reset-done", "client", "open"]);
  } finally {
    release();
    await sm.stopAll();
  }
});

test("explicit station policy reset closes that station while preserving unrelated stations", async () => {
  const { eufy, sdk } = createSdk({
    cfg: {
      email: "synthetic@example.com",
      password: "synthetic",
      country: "US",
      session: "/tmp/synthetic-sdk-session",
      cameras: {},
      lan: { stationAddresses: {} },
    },
    DEBUG: false,
    hooks: {},
  });
  const closed = [];
  eufy.getP2pSessions = () =>
    new Map(
      ["STATION", "STATION#live:2", "OTHER"].map((key) => [
        key,
        {
          close: async () => {
            closed.push(key);
          },
        },
      ]),
    );
  assert.equal(await sdk.dropStationClient("STATION"), true);
  assert.deepEqual(closed, ["STATION", "STATION#live:2"]);
});
