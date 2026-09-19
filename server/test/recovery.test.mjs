import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { installRecoveryRepin } from "../src/recovery.mjs";
import { createState } from "../src/state.mjs";

const originals = {};
before(() => { for (const k of ["log", "warn", "error"]) { originals[k] = console[k]; console[k] = () => {}; } });
after(() => { for (const k of ["log", "warn", "error"]) console[k] = originals[k]; });

const tick = () => new Promise((r) => setImmediate(r));

function ctxWith() {
  const state = createState();
  const calls = { applyLogin: [], pins: 0, expired: 0 };
  const ctx = {
    state,
    sdk: { LoginStatus: { Ok: "ok", TwoFactor: "2fa" } },
    applyAllPins: async () => { calls.pins++; },
    // Mimics vendored auth.mjs: Ok clears sessionLost; anything else leaves it.
    applyLogin: async (r) => { calls.applyLogin.push(r); if (r.status === "ok") { state.flags.sessionLost = false; } },
    // Mimics vendored onSessionExpired: uses its LOCAL applyLogin, so ctx.applyLogin never sees it.
    onSessionExpired: async () => { calls.expired++; state.flags.sessionLost = true; state.flags.sessionLost = !ctx._recoverOk; },
    _recoverOk: true,
  };
  installRecoveryRepin(ctx);
  return { ctx, calls, flags: state.flags };
}

test("first boot login does not re-pin (completeBoot already pins); post-boot Ok re-login does", async () => {
  const { ctx, calls, flags } = ctxWith();
  await ctx.applyLogin({ status: "ok" });
  await tick();
  assert.equal(calls.pins, 0, "pre-boot: no re-pin");
  flags.ready = true;
  await ctx.applyLogin({ status: "ok" }); // watchdog stall re-login path
  await tick();
  assert.equal(calls.pins, 1);
  assert.deepEqual(calls.applyLogin.map((r) => r.status), ["ok", "ok"], "vendored applyLogin still called");
});

test("2FA-driven re-auth after a kicked session: re-pin only once the login is Ok", async () => {
  const { ctx, calls, flags } = ctxWith();
  flags.ready = true;
  flags.sessionLost = true;
  await ctx.applyLogin({ status: "2fa" });
  await tick();
  assert.equal(calls.pins, 0, "still lost → no re-pin");
  await ctx.applyLogin({ status: "ok" }); // POST /auth/tfa
  await tick();
  assert.equal(calls.pins, 1);
  assert.equal(flags.sessionLost, false);
});

test("automatic recovery via maybeRecoverSession → onSessionExpired re-pins on lost→recovered", async () => {
  const { ctx, calls, flags } = ctxWith();
  flags.ready = true;
  ctx.maybeRecoverSession();
  await tick();
  assert.equal(calls.expired, 1);
  assert.equal(calls.pins, 1);
  // Recovery that still needs user action (2FA) must not re-pin.
  ctx._recoverOk = false;
  ctx.maybeRecoverSession();
  await tick();
  assert.equal(calls.expired, 2);
  assert.equal(flags.sessionLost, true);
  assert.equal(calls.pins, 1);
  // Vendored guard preserved: already lost / recovering / not ready → no new recovery.
  ctx.maybeRecoverSession();
  flags.sessionLost = false; flags.recovering = true; ctx.maybeRecoverSession();
  flags.recovering = false; flags.ready = false; ctx.maybeRecoverSession();
  await tick();
  assert.equal(calls.expired, 2);
});

test("pin failure is logged, never thrown into the auth flow", async () => {
  const { ctx, flags } = ctxWith();
  const logs = [];
  console.error = (...a) => logs.push(a.join(" "));
  flags.ready = true;
  ctx.applyAllPins = async () => { throw new Error("p2p down"); };
  await ctx.applyLogin({ status: "ok" });
  await tick();
  assert.equal(logs.some((l) => l.includes("re-pin after re-login failed: p2p down")), true);
});
