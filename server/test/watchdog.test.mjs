import test from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { LoginStatus } from "@mega-yfue/eufy-sdk";
import { createWatchdog } from "../src/vendor/ha-bridge/watchdog.mjs";

function fixture(t, interval = 600_000) {
  let now = 0;
  t.mock.method(Date, "now", () => now);
  t.mock.method(console, "error", () => {});
  t.mock.method(console, "log", () => {});
  const eufy = new EventEmitter();
  eufy.pollIntervalMs = interval;
  let disconnects = 0, logins = 0, rearms = 0;
  eufy.disconnect = async () => { disconnects++; };
  eufy.login = async () => { logins++; return { status: LoginStatus.Ok }; };
  eufy.setPollInterval = (ms) => { assert.equal(ms, interval); rearms++; };
  const flags = { ready: true, recovering: false, lastActivity: 0,
    pushConnected: true, pushSince: 0 };
  const ctx = { eufy, state: { flags }, PUSH_STALL_MS: 900_000,
    applyLogin: async () => {} };
  return { eufy, flags, watchdog: createWatchdog(ctx),
    at: (ms) => { now = ms; }, counts: () => ({ disconnects, logins, rearms }) };
}

test("successful unchanged polls prevent the 30-minute media-disconnecting recovery", async (t) => {
  const f = fixture(t);
  // No device reports are emitted: successful HTTP polling is the proof of life.
  f.at(29 * 60_000);
  f.eufy.emit("pollComplete", { status: "ok" });
  f.at(31 * 60_000);
  await f.watchdog.watchdogTick();
  assert.equal(f.flags.lastActivity, 29 * 60_000);
  assert.deepEqual(f.counts(), { disconnects: 0, logins: 0, rearms: 0 });
});

test("a settled partial or failed poll keeps loop liveness separate from cloud health", async (t) => {
  const f = fixture(t);
  f.at(29 * 60_000);
  f.eufy.emit("pollComplete", { status: "partial" });
  f.at(58 * 60_000);
  f.eufy.emit("pollComplete", { status: "error" });
  f.at(60 * 60_000);
  await f.watchdog.watchdogTick();
  assert.equal(f.flags.lastActivity, 58 * 60_000);
  assert.equal(f.counts().disconnects, 0);
});

test("missing poll completions still triggers the existing recovery", async (t) => {
  const f = fixture(t);
  f.at(31 * 60_000);
  await f.watchdog.watchdogTick();
  assert.deepEqual(f.counts(), { disconnects: 1, logins: 1, rearms: 1 });
  assert.equal(f.flags.recovering, false);
});

test("device reports do not masquerade as completed cloud polls", async (t) => {
  const f = fixture(t);
  f.at(29 * 60_000);
  f.eufy.emit("deviceState", { lastSeenMs: 29 * 60_000 });
  assert.equal(f.flags.lastActivity, 0);
  f.at(31 * 60_000);
  await f.watchdog.watchdogTick();
  assert.equal(f.counts().disconnects, 1);
});

test("disabled polling cannot trigger poll-idle recovery", async (t) => {
  const f = fixture(t, 0);
  f.at(24 * 60 * 60_000);
  await f.watchdog.watchdogTick();
  assert.equal(f.watchdog.stallThresholdMs(), Infinity);
  assert.equal(f.counts().disconnects, 0);
});

test("push-down recovery remains active even when polling is disabled", async (t) => {
  const f = fixture(t, 0);
  f.flags.pushConnected = false;
  f.at(900_000);
  await f.watchdog.watchdogTick();
  assert.equal(f.counts().disconnects, 1);
});
