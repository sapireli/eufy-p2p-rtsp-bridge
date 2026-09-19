import { test } from "node:test";
import assert from "node:assert/strict";
import * as sdk from "@mega-yfue/eufy-sdk";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

// @mega-yfue/eufy-sdk's package.json declares an "exports" map with only "."
// (no "./package.json" subpath), so require("@mega-yfue/eufy-sdk/package.json")
// throws ERR_PACKAGE_PATH_NOT_EXPORTED. Resolve the package root from the
// entry file's URL instead (dist/index.js -> package root) and read it directly.
const entryPath = fileURLToPath(import.meta.resolve("@mega-yfue/eufy-sdk"));
const pkgRoot = dirname(dirname(entryPath)); // dist/index.js -> dist -> root
const version = JSON.parse(readFileSync(join(pkgRoot, "package.json"), "utf8")).version;

test("pinned sdk version", () => assert.equal(version, "0.1.1"));

test("exports used by the bridge exist", () => {
  for (const name of ["EufyMega", "FileSessionStore", "LoginStatus", "ConsoleLogger", "extractParamSets", "codedGeometry"])
    assert.equal(typeof sdk[name], name === "LoginStatus" ? "object" : "function", name);
  assert.ok(sdk.LoginStatus.Ok && sdk.LoginStatus.Captcha && sdk.LoginStatus.TwoFactor);
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
