// The vendored auth module makes one automatic attempt after a kicked session. If cloud login throws
// during a network outage, it leaves sessionLost set and its guard blocks every later attempt. Retry
// transient failures here, outside vendor/, while preserving human-driven 2FA and captcha states.
export function installAuthRetry(ctx, { minDelayMs = 1_000, maxDelayMs = 60_000, random = Math.random, setTimer = setTimeout, clearTimer = clearTimeout } = {}) {
  const { flags } = ctx.state;
  const recoverOnce = ctx.onSessionExpired;
  const applyLogin = ctx.applyLogin;
  let timer;
  let failures = 0;
  let stopped = false;
  const needsAnswer = () => ["require_2fa", "require_captcha"].includes(ctx.authStatus().state);
  const cancel = () => { if (timer) clearTimer(timer); timer = undefined; };

  function schedule() {
    if (stopped || timer || !flags.ready || !flags.sessionLost || needsAnswer()) return;
    const base = Math.min(maxDelayMs, minDelayMs * 2 ** Math.min(failures++, 16));
    const delay = Math.max(1, Math.round(base * (0.8 + random() * 0.4)));
    console.error(`[bridge] session recovery will retry in ${delay} ms`);
    timer = setTimer(() => { timer = undefined; if (flags.sessionLost) return ctx.onSessionExpired(); }, delay);
    timer?.unref?.();
  }

  ctx.onSessionExpired = async function retrySessionRecovery() {
    if (stopped || !flags.ready) return;
    if (flags.recovering) { schedule(); return; }
    try { await recoverOnce(); }
    catch (error) { console.error(`[bridge] session recovery wrapper failed: ${error?.message ?? error}`); }
    if (!flags.sessionLost || needsAnswer()) { cancel(); failures = 0; }
    else schedule();
  };

  ctx.applyLogin = async function applyLoginAndResetRetry(result) {
    await applyLogin(result);
    if (!flags.sessionLost || needsAnswer()) { cancel(); failures = 0; }
  };

  ctx.stopAuthRetry = () => { stopped = true; cancel(); };
  return ctx;
}
