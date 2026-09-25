import { test } from "node:test";
import assert from "node:assert/strict";
import { LoginStatus } from "@mega-yfue/eufy-sdk";
import { createAuth } from "../src/vendor/ha-bridge/auth.mjs";
import { installRecoveryRepin } from "../src/recovery.mjs";
import { installAuthRetry } from "../src/auth-retry.mjs";
import { createState } from "../src/state.mjs";

function authHarness(login) {
  const state = createState();
  state.flags.ready = true;
  state.flags.lastLogin = { status: LoginStatus.Ok };
  const timers = [];
  let pins = 0;
  const eufy = { login, disconnect: async () => {}, setPollInterval: () => {}, pollIntervalMs: 600_000 };
  const ctx = { state, eufy, sdk: { LoginStatus }, completeBoot: async () => {}, broadcast: () => {}, applyAllPins: async () => { pins++; } };
  Object.assign(ctx, createAuth(ctx));
  installRecoveryRepin(ctx);
  installAuthRetry(ctx, {
    minDelayMs: 10, maxDelayMs: 40, random: () => 0.5,
    setTimer: (fn, delay) => { const timer = { fn, delay, cancelled: false }; timers.push(timer); return timer; },
    clearTimer: (timer) => { timer.cancelled = true; },
  });
  return { ctx, state, timers, get pins() { return pins; } };
}

test("post-boot auth expiry retries transient network failures and re-pins after recovery", async () => {
  let attempts = 0;
  const h = authHarness(async () => { if (++attempts < 3) throw new Error("network partition"); return { status: LoginStatus.Ok }; });
  const oldError = console.error, oldLog = console.log;
  console.error = () => {}; console.log = () => {};
  try {
    await h.ctx.onSessionExpired();
    assert.equal(h.ctx.authStatus().state, "reauth");
    assert.equal(h.timers[0].delay, 10);
    await h.timers[0].fn();
    assert.equal(h.timers[1].delay, 20);
    await h.timers[1].fn();
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(attempts, 3);
    assert.equal(h.ctx.authStatus().state, "ok");
    assert.equal(h.state.flags.sessionLost, false);
    assert.equal(h.pins, 1);
    assert.equal(h.timers.length, 2);
  } finally { h.ctx.stopAuthRetry(); console.error = oldError; console.log = oldLog; }
});

test("2FA pauses automatic retries and a manual answer clears the loss", async () => {
  const h = authHarness(async () => ({ status: LoginStatus.TwoFactor, method: "app" }));
  const oldError = console.error;
  console.error = () => {};
  try {
    await h.ctx.onSessionExpired();
    assert.equal(h.ctx.authStatus().state, "require_2fa");
    assert.equal(h.timers.length, 0);
    await h.ctx.applyLogin({ status: LoginStatus.Ok });
    assert.equal(h.ctx.authStatus().state, "ok");
  } finally { h.ctx.stopAuthRetry(); console.error = oldError; }
});

test("shutdown cancels recovery and a stale timer cannot log in again", async () => {
  let attempts = 0;
  const h = authHarness(async () => { attempts++; throw new Error("offline"); });
  const oldError = console.error;
  console.error = () => {};
  try {
    await h.ctx.onSessionExpired();
    assert.equal(h.timers.length, 1);
    h.ctx.stopAuthRetry();
    assert.equal(h.timers[0].cancelled, true);
    await h.timers[0].fn();
    assert.equal(attempts, 1);
  } finally { console.error = oldError; }
});
