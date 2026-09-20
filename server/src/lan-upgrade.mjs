// LAN-first path preference (direct-P2P-preferred with relay fallback).
//
// The SDK connects a station to whichever peer answers first and then stops looking, so once a session
// lands on the cloud relay it never migrates to a direct-LAN path on its own. This driver makes the
// bridge PREFER a direct LAN peer without giving up the relay safety net:
//
//   1. LAN-FIRST at boot — each station starts "forced" (guard closes any WAN/relay peer) with TURN
//      rendezvous disabled for it, and gets a generous window to complete a direct-LAN connect. The
//      first stream is direct whenever the LAN path is reachable in that window.
//   2. RELAY FALLBACK — if the window expires with no LAN bytes, the station is un-forced (TURN back on)
//      and reopened, so it streams via relay/WAN rather than staying dark.
//   3. KEEP CLIMBING — a station on relay is periodically re-attempted (exponential backoff, capped), so
//      it moves to direct LAN as soon as the network cooperates. A locked-LAN station that loses its
//      direct path falls back to relay and re-enters the climb.
//
// Levers (both scoped per station, so one HomeBase attempting LAN never disturbs another):
//   - guard force: lan-guard.mjs calls isForced(stationSn); forced ⇒ WAN peers are closed.
//   - force-LAN: the SDK's `lanOnlyForStation` option, asked per session, names the CIDR a pinned
//     station's peer must be inside; the SDK refuses (and END's) anything outside it and keeps looking.
//
// Only meaningful when lan.force is false and a lan.cidr is set (lan.force=true is already LAN-only).

const TICK_MS = 2000;

export function createLanUpgrade(ctx) {
  const { cfg, state } = ctx;
  const U = cfg.lan.upgrade;
  const active = Boolean(cfg.lan.cidr) && !cfg.lan.force && U.enabled;

  // Per-station machine. mode: "trying" (forcing LAN, awaiting direct bytes) | "lan" (locked direct) |
  // "relay" (on fallback, waiting to re-attempt).
  const st = new Map(); // stationSn -> { mode, attempts, deadline, nextTryAt, sawLan }
  const forced = new Set(); // stations currently forced LAN-only
  state.peerPath = new Map(); // stationSn -> "lan" | "wan" (last observed), for /healthz + decisions
  // Force-LAN is enforced INSIDE the SDK, via the `lanOnlyForStation` option it asks for every session, so
  // it covers the media #live sessions too — those look up independently, and a guard that only sees the
  // control session's p2pConnect cannot pin them.
  // Multi-socket punch applies to EVERY station. Measured: with it, both the HomeBase and the standalone
  // camera land a direct-LAN peer and decode 300/300 frames; restricting it to HomeBases dropped the
  // standalone to relay (or no connect at all, which is what a frozen single-frame stream looks like).

  const now = () => Date.now();
  const machine = (station) => st.get(station) ?? st.set(station, { mode: "relay", attempts: 0, deadline: 0, nextTryAt: 0, sawLan: false }).get(station);
  const stationsOf = () => {
    const m = new Map(); // stationSn -> [camSn] (enabled cams only)
    for (const c of ctx.listCameras?.() ?? []) if (c.enabled) (m.get(c.stationSn) ?? m.set(c.stationSn, []).get(c.stationSn)).push(c.sn);
    return m;
  };
  const anyStreaming = (cams) => cams.some((sn) => state.streaming.has(sn));

  function setForce(station, on) {
    if (on) forced.add(station);
    else forced.delete(station);
  }

  /** SDK hook: the CIDR this station's peer must be inside right now, or undefined to accept any peer. */
  function cidrFor(station) {
    return isForced(station) ? (cfg.lan.cidr ?? undefined) : undefined;
  }

  /** Guard hook: is this station currently pinned to LAN-only? (cfg.lan.force is the global override.) */
  function isForced(station) { return cfg.lan.force || forced.has(station); }

  /** Guard hook: called on every connect decision with the observed peer path. */
  function onPeer(station, path /* "lan"|"wan" */) {
    state.peerPath.set(station, path);
    if (!active) return;
    const m = machine(station);
    if (path === "lan" && m.mode !== "lan") {
      m.mode = "lan"; m.attempts = 0; m.sawLan = true; m.deadline = 0;
      setForce(station, true); // keep it pinned so it stays direct
      console.log(`[lan-upgrade] ${station}: DIRECT LAN established — pinned`);
    }
  }

  /** Begin (or re-begin) a LAN-only attempt for a station: force it, drop the session, reopen its cams. */
  function attempt(station, cams, initial) {
    const m = machine(station);
    m.mode = "trying";
    m.deadline = now() + (initial ? U.initialWindowMs : U.windowMs);
    setForce(station, true);
    console.log(`[lan-upgrade] ${station}: trying direct LAN (${initial ? "boot" : `attempt ${m.attempts + 1}`}, ${Math.round((m.deadline - now()) / 1000)}s window)`);
    // Tear the whole station session (control + media) so the reconnect re-runs the lookup under the
    // force/TURN-off flags, then reopen each camera.
    void Promise.resolve(ctx.sdk.dropStreamClient?.(station, station)).catch(() => {}).then(() => {
      for (const sn of cams) ctx.restartCamera?.(sn);
    });
  }

  /** Give up on LAN for now: un-force, reopen on relay, schedule the next climb with backoff. */
  function fallback(station, cams, why) {
    const m = machine(station);
    m.mode = "relay";
    m.attempts++;
    const backoff = Math.min(U.intervalMs * 2 ** (m.attempts - 1), U.maxBackoffMs);
    m.nextTryAt = now() + backoff;
    m.deadline = 0;
    setForce(station, false);
    console.log(`[lan-upgrade] ${station}: ${why} — relay fallback, next LAN attempt in ${Math.round(backoff / 1000)}s`);
    void Promise.resolve(ctx.sdk.dropStreamClient?.(station, station)).catch(() => {}).then(() => {
      for (const sn of cams) ctx.restartCamera?.(sn);
    });
  }

  function tick() {
    if (!active) return;
    const stations = stationsOf();
    for (const [station, cams] of stations) {
      const m = machine(station);
      const streaming = anyStreaming(cams);
      const path = state.peerPath.get(station);

      if (m.mode === "trying") {
        if (streaming && path === "lan") { onPeer(station, "lan"); continue; } // promoted in onPeer
        if (now() >= m.deadline) fallback(station, cams, "no direct LAN in window");
        continue;
      }
      if (m.mode === "lan") {
        // Locked direct. If the LAN path is gone (streaming stopped or peer went WAN) for a grace window,
        // drop back to relay and re-enter the climb.
        if (!streaming || path === "wan") {
          if (!m.deadline) m.deadline = now() + U.windowMs; // start grace
          else if (now() >= m.deadline) fallback(station, cams, "direct LAN lost");
        } else {
          m.deadline = 0;
        }
        continue;
      }
      // mode === "relay": climb back to LAN once the backoff elapses and the station is actually up.
      if (streaming && now() >= m.nextTryAt) attempt(station, cams, false);
    }
  }

  /** Called once after cameras are known (completeBoot), before/around the first opens: pin every station
   *  LAN-first so the initial connect prefers direct. */
  function start() {
    if (!active) { console.log(`[lan-upgrade] disabled (${!cfg.lan.cidr ? "no lan.cidr" : cfg.lan.force ? "lan.force=true (P2P-only)" : "upgrade.enabled=false"})`); return; }
    for (const [station, cams] of stationsOf()) attempt(station, cams, true);
    state.timers.lanUpgrade ??= setInterval(tick, TICK_MS);
    console.log(`[lan-upgrade] LAN-first enabled for ${stationsOf().size} station(s); relay fallback after ${Math.round(U.initialWindowMs / 1000)}s`);
  }

  function status() {
    const out = {};
    for (const [station, m] of st) out[station] = { mode: m.mode, attempts: m.attempts, peer: state.peerPath.get(station) ?? null };
    return out;
  }

  return { isForced, cidrFor, onPeer, start, tick, status };
}
