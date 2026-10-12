// Track cloud poll completion separately from device reports. An unchanged
// last-seen value produces no deviceState event, even when the cloud poll succeeds.
// A settled failure also rearms the SDK poll loop: errors have their own auth/error
// handlers and do not prove that loop has hung. Push has its own connection signal.
// Missing poll completions can trigger recovery;
// ordinary device silence must not disconnect otherwise healthy live media sessions.
import { LoginStatus } from "@mega-yfue/eufy-sdk";

export function createWatchdog(ctx) {
  const { eufy, PUSH_STALL_MS } = ctx;
  const { flags } = ctx.state;

  const bumpActivity = (completion) => {
    flags.lastActivity = Date.now();
    flags.pollCompleteAt = flags.lastActivity;
    flags.pollCompletions = (flags.pollCompletions ?? 0) + 1;
    flags.pollStatus = completion?.status ?? "unknown";
  };
  eufy.on("pollComplete", bumpActivity);

  /** No poll heartbeat for max(3 polls, 30 min) ⇒ treat the realtime/poll channel as stalled. */
  function stallThresholdMs() {
    const interval = eufy.pollIntervalMs ?? 600_000;
    if (interval <= 0) return Infinity; // explicitly disabled polls cannot be overdue
    return Math.max(3 * interval, 30 * 60_000);
  }

  async function watchdogTick() {
    if (!flags.ready || flags.recovering) return;
    const idleMs = Date.now() - flags.lastActivity;
    const pushDeadMs = flags.pushConnected ? 0 : Date.now() - flags.pushSince;
    const pollStalled = idleMs >= stallThresholdMs();
    const pushStalled = pushDeadMs >= PUSH_STALL_MS;
    if (!pollStalled && !pushStalled) return;
    flags.recovering = true;
    const why = pollStalled ? `poll idle ${Math.round(idleMs / 1000)}s` : `push down ${Math.round(pushDeadMs / 1000)}s`;
    console.error(`[bridge] realtime stalled (${why}) — re-establishing`);
    try {
      await eufy.disconnect();
      const result = await eufy.login();
      await ctx.applyLogin(result); // refreshes auth state; completeBoot is a no-op once ready
      if (result.status !== LoginStatus.Ok) {
        console.error(`[bridge] re-login not OK (${result.status}) — exiting for a clean restart`);
        process.exit(1);
      }
      eufy.setPollInterval(eufy.pollIntervalMs); // re-arm the poll loop under the new realtime epoch
      flags.lastActivity = Date.now();
      flags.pushSince = Date.now(); // give push a fresh window to reconnect before flagging it again
      console.log("[bridge] realtime re-established after stall");
    } catch (e) {
      console.error(`[bridge] stall recovery failed (${e?.message ?? e}) — exiting for a clean restart`);
      process.exit(1);
    } finally {
      flags.recovering = false;
    }
  }

  const pollHealth = () => ({
    enabled: (eufy.pollIntervalMs ?? 600_000) > 0,
    intervalMs: eufy.pollIntervalMs ?? 600_000,
    completed: flags.pollCompletions ?? 0,
    lastCompletedAt: flags.pollCompleteAt ?? null,
    lastStatus: flags.pollStatus ?? null,
  });
  return { bumpActivity, pollHealth, stallThresholdMs, watchdogTick };
}
