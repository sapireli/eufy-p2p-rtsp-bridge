// Re-apply the camera pins (dual view, quality) after every cloud-session recovery, without editing the
// vendored auth/watchdog modules: wrap the ctx entry points they are driven through.
//   - ctx.applyLogin(result): called by the watchdog after a stall re-login and by the HTTP auth routes
//     (2FA/captcha/retry) after a kicked session. Post-boot + Ok ⇒ a re-login happened ⇒ re-pin.
//   - ctx.onSessionExpired(): the automatic re-login uses auth.mjs's LOCAL applyLogin (not ctx's), so
//     the wrapper looks at the lost→recovered transition when it resolves. maybeRecoverSession is the
//     vendored guard re-stated here so it routes through the wrapped onSessionExpired.
// Best-effort: pin failures are logged, never propagated into the auth flow.
export function installRecoveryRepin(ctx) {
  const { flags } = ctx.state;
  const { applyLogin, onSessionExpired } = ctx;
  const ok = () => ctx.sdk?.LoginStatus?.Ok ?? "ok";

  function repin(why) {
    console.log(`[bridge] ${why} — re-applying camera pins`);
    return Promise.resolve()
      .then(() => ctx.applyAllPins())
      .catch((e) => console.error(`[bridge] re-pin after ${why} failed: ${e?.message ?? e}`));
  }

  ctx.applyLogin = async function applyLoginAndRepin(result) {
    const postBoot = flags.ready;
    await applyLogin(result);
    if (postBoot && result?.status === ok() && !flags.sessionLost) void repin("re-login");
  };

  ctx.onSessionExpired = async function onSessionExpiredAndRepin() {
    await onSessionExpired();
    if (flags.ready && !flags.sessionLost) void repin("session recovered");
  };

  ctx.maybeRecoverSession = function maybeRecoverSession() {
    if (flags.ready && !flags.sessionLost && !flags.recovering) void ctx.onSessionExpired();
  };

  return ctx;
}
