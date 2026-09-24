import { test } from "node:test";
import assert from "node:assert/strict";
import { createSdk } from "../src/sdk-adapter.mjs";

test("openFeed delegates warm timing to the pinned SDK", async () => {
  const { sdk } = createSdk({
    cfg: {
      email: "x@example.com",
      password: "x",
      country: "US",
      session: "/dev/null",
      lan: { stationAddresses: {}, force: false, cidr: "" },
    },
    DEBUG: false,
  });
  const calls = [];
  const cam = { openReadable: async (...args) => { calls.push(args); return {}; } };
  const client = { getDevice: async () => ({ camera: () => cam }) };

  await sdk.openFeed(client, "T8000P0000000000");

  assert.deepEqual(calls, [[]]);
});
