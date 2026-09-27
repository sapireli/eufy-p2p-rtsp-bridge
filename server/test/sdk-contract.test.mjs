import { test } from "node:test";
import assert from "node:assert/strict";
import * as sdk from "@mega-yfue/eufy-sdk";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { createSdk } from "../src/sdk-adapter.mjs";

// @mega-yfue/eufy-sdk's package.json declares an "exports" map with only "."
// (no "./package.json" subpath), so require("@mega-yfue/eufy-sdk/package.json")
// throws ERR_PACKAGE_PATH_NOT_EXPORTED. Resolve the package root from the
// entry file's URL instead (dist/index.js -> package root) and read it directly.
const entryPath = fileURLToPath(import.meta.resolve("@mega-yfue/eufy-sdk"));
const pkgRoot = dirname(dirname(entryPath)); // dist/index.js -> dist -> root
const version = JSON.parse(readFileSync(join(pkgRoot, "package.json"), "utf8")).version;

// The integration branch is based on PR #280 and builds its own dist when installed from git.
test("pinned SDK includes the reviewed P2P fixes and peer acceptance callback", () => {
  const dist = readFileSync(entryPath, "utf8");
  for (const marker of ["PUNCH_PROBE_SOCKETS", "REORDER_WAIT_MS", "acceptP2PPeer"])
    assert.ok(dist.includes(marker), `missing ${marker} from the pinned SDK build`);
  assert.ok(version, "sdk package.json has a version");
});

test("exports used by the bridge exist", () => {
  for (const name of ["EufyMega", "FileSessionStore", "LoginStatus", "ConsoleLogger", "extractParamSets", "codedGeometry"])
    assert.equal(typeof sdk[name], name === "LoginStatus" ? "object" : "function", name);
  assert.ok(sdk.LoginStatus.Ok && sdk.LoginStatus.Captcha && sdk.LoginStatus.TwoFactor);
});

test("automatic display policy keeps a battery doorbell budgeted and a mains floodlight continuous", async () => {
  const sn = "T8000P0000000000";
  const { eufy, sdk: adapter } = createSdk({
    cfg: {
      email: "synthetic@example.com", password: "synthetic", country: "US", session: "/tmp/synthetic-sdk-session",
      cameras: {}, lan: { stationAddresses: {}, cidr: null, force: false },
    },
    DEBUG: false,
  });
  let model = "T8214";
  eufy.getDevice = async () => ({
    describe: () => ({ sn, name: "Synthetic", model, modelName: "Camera", capabilities: ["camera", "battery"] }),
    has: (capability) => capability === "battery",
  });
  assert.equal((await adapter.describe(sn)).powerTier, "battery");
  model = "T8423";
  assert.equal((await adapter.describe(sn)).powerTier, "wired");
});

test("bridge config supplies per-pull power and a per-station peer decision", async () => {
  const sn = "T8000P0000000000";
  let forceLan = true;
  const { eufy, sdk: adapter } = createSdk({
    cfg: {
      email: "synthetic@example.com", password: "synthetic", country: "US", session: "/tmp/synthetic-sdk-session",
      cameras: { [sn]: { powerOverride: "always-on" } }, lan: { stationAddresses: {}, cidr: "192.168.1.0/24", force: false },
    },
    DEBUG: false,
    hooks: { forceLanForStation: () => forceLan },
  });
  const accept = eufy.p2p.deps.acceptP2PPeer;
  assert.equal(accept("STATION", { host: "192.168.1.20", port: 32100 }), true);
  assert.equal(accept("STATION", { host: "203.0.113.20", port: 32100 }), false);
  forceLan = false;
  assert.equal(accept("STATION", { host: "203.0.113.20", port: 32100 }), true);
  eufy.getDevice = async () => ({
    describe: () => ({ sn, name: "Synthetic", model: "T8214", modelName: "Camera", capabilities: ["camera", "battery"] }),
    has: (capability) => capability === "battery",
    camera: () => ({ openReadable: async (opts) => opts }),
  });
  const described = await adapter.describe(sn);
  assert.equal(described.powerOverride, "always-on");
  assert.equal(described.powerTier, "wired");
  assert.deepEqual(await adapter.openFeed(eufy, sn), { powered: "wired" });
});

test("an explicit battery claim stays battery-budgeted on each media pull", async () => {
  const sn = "T8000P0000000000";
  const { eufy, sdk: adapter } = createSdk({
    cfg: {
      email: "synthetic@example.com", password: "synthetic", country: "US", session: "/tmp/synthetic-sdk-session",
      cameras: { [sn]: { powerOverride: "battery" } }, lan: { stationAddresses: {} },
    },
    DEBUG: false,
  });
  eufy.getDevice = async () => ({ camera: () => ({ openReadable: async (opts) => opts }) });
  assert.deepEqual(await adapter.openFeed(eufy, sn), { powered: "battery" });
});

test("EufyMega instance methods used by the bridge", () => {
  const eufy = new sdk.EufyMega({ email: "x@y.z", password: "p", countryCode: "US", autoRealtime: false });
  for (const m of ["login", "solveCaptcha", "submitVerifyCode", "getDevices", "getDevice", "setProperty", "getP2pSessions", "disconnect", "setPollInterval", "on"])
    assert.equal(typeof eufy[m], "function", m);
  assert.equal(typeof eufy.pollIntervalMs, "number");
  // Internal escape hatch used by pins.mjs for the dual-view raw command (no public capability yet).
  assert.equal(typeof eufy.commandSinkFor, "function", "commandSinkFor (private in TS, reachable in JS)");
  assert.equal(typeof eufy.commandContext, "function", "commandContext (private in TS; supplies the device channel for set-payload)");
});

test("annex-b helpers behave", () => {
  // SPS(7) + PPS(8) + IDR(5) H.264 access unit with start codes.
  const au = Buffer.from([0,0,0,1,0x67,0x42,0xc0,0x1e,0xda,0x02,0x80,0xf6,0x80,0x6d,0x0a,0x13,0x50, 0,0,0,1,0x68,0xce,0x38,0x80, 0,0,0,1,0x65,0x88,0x84,0x00]);
  const sets = sdk.extractParamSets(au);
  assert.ok(sets, "param sets found");
  assert.equal(sets.codec, "h264");
  assert.equal(sdk.extractParamSets(Buffer.from([0,0,0,1,0x41,0x9a,0x00])), undefined, "delta frame → no sets");
});

test("P2PSession exposes connectAddress and close (used by lan-guard)", () => {
  // The SDK ships as one bundled dist/index.js (no separate transport/p2p/p2p-session.js), so read the
  // same entry file the version/exports checks above resolve and grep it for the internals sdk-adapter.mjs
  // relies on (sessionPeerHost reads connectAddress; closeSession calls close()).
  const src = readFileSync(entryPath, "utf8");
  assert.match(src, /this\.connectAddress = /, "connectAddress field assigned on connect");
  assert.match(src, /async close\(\)/, "close() method");
});
