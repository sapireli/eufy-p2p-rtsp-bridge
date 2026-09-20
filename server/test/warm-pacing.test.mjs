import { test } from "node:test";
import assert from "node:assert/strict";
import { createSdk } from "../src/sdk-adapter.mjs";

// A standalone camera has far fewer P2P session slots than a HomeBase. Re-issuing the start every 2s for
// the whole warm deadline (21 times, measured) exhausts one rather than waking it — a Floodlight Cam here
// stayed frozen until every client stopped asking, and recovered the moment the bridge was stopped.

function sdkWith(stall) {
  return createSdk({
    cfg: {
      email: "x@example.com",
      password: "x",
      country: "US",
      session: "/dev/null",
      lan: { stationAddresses: {}, force: false, cidr: "" },
      stall,
    },
    DEBUG: false,
  });
}

function fakeClient(seen) {
  const cam = {
    openReadable: async (opts) => {
      seen.push(opts);
      return {};
    },
  };
  return { getDevice: async () => ({ camera: () => cam }) };
}

test("a standalone camera is warmed more gently than one behind a HomeBase", async () => {
  const seen = [];
  const { sdk } = sdkWith({ warmTimeoutMs: 45_000, warmRetryMs: 2000, standaloneWarmRetryMs: 8000 });
  const client = fakeClient(seen);

  await sdk.openFeed(client, "STA", { powered: true, standalone: true });
  await sdk.openFeed(client, "ATT", { powered: true, standalone: false });

  assert.equal(seen[0].warmRetryMs, 8000, "standalone: ask rarely, it has few session slots to spend");
  assert.equal(seen[1].warmRetryMs, 2000, "attached: a HomeBase has sessions to spare");
  assert.equal(seen[0].warmTimeoutMs, 45_000, "the deadline is unchanged — only how often we re-ask");
  assert.equal(seen[0].powered, "wired", "the powered hint still rides along");
});
