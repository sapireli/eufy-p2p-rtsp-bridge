// Always-on media core. One Slot per enabled camera holds the SDK's Annex-B Readable ("feed"), fans its
// chunks out to HTTP consumers (go2rtc), and is watched for stalls. Watchdog thresholds are ported from
// eufy-frigate-bridge (12 s stall / 45 s gap / 300 s exit), which were calibrated over a 19 h run.
import { newSlot } from "./state.mjs";

export function createStreamManager(ctx) {
  const { state, cfg } = ctx;
  const exit = (code) => (ctx.exit ?? process.exit)(code);
  const slotFor = (sn) => state.slots.get(sn) ?? state.slots.set(sn, newSlot(sn)).get(sn);
  const stationChains = new Map(); // parentStationSn -> Promise: serialises P2P opens per HomeBase
  const stationRestart = new Map(); // station -> { timer, idx }: coordinated restart backoff for co-located cams

  /** Enabled cameras on a station (a standalone camera is its own station). */
  const camsForStation = (station) =>
    (ctx.listCameras?.() ?? []).filter((c) => c.enabled && (c.stationSn ?? c.sn) === station);

  function onChunk(slot, chunk) {
    const now = Date.now();
    slot.lastBytesAt = now;
    slot.firstFailureAt = 0;
    slot.failures = 0;
    slot.backoffIdx = 0;
    slot.gapFired = false; // bytes are flowing again → allow one fresh gap-disconnect if it stalls later
    const st = stationRestart.get(ctx.getCamera?.(slot.sn)?.stationSn ?? slot.sn);
    if (st) st.idx = 0; // this station is delivering again → reset its coordinated-restart backoff
    if (!state.streaming.has(slot.sn)) {
      state.streaming.add(slot.sn);
      console.log(`[bridge] ${slot.sn}: streaming`);
    }
    const sets = ctx.sdk.extractParamSets(chunk); // non-undefined ⇒ this chunk carries SPS/PPS (keyframe AU)
    if (sets) {
      slot.lastKeyChunk = chunk;
      if (slot.codec !== sets.codec) {
        slot.codec = sets.codec;
        const g = ctx.sdk.codedGeometry(sets);
        slot.width = g?.width;
        slot.height = g?.height;
        console.log(`[bridge] ${slot.sn}: codec ${sets.codec} ${slot.width ?? "?"}x${slot.height ?? "?"}`);
        if (sets.codec !== "h264")
          console.warn(`[bridge] ${slot.sn}: stream is ${sets.codec.toUpperCase()} — Raspberry Pi clients cannot decode it. Lower the camera's streaming quality to 1080p/720p in the eufy app.`);
      }
    }
    for (const c of slot.consumers) {
      if (c.destroyed) { slot.consumers.delete(c); continue; }
      if (c.writableNeedDrain) { c._ewbDropping = true; continue; } // backpressure: drop until next keyframe
      if (c._ewbDropping && !sets) continue;
      c._ewbDropping = false;
      c.write(chunk);
    }
  }

  function scheduleReopen(slot, why) {
    // Co-located cameras (>1 enabled camera on the station) recover as a GROUP: a lone re-warm contends
    // with its still-live sibling for the HomeBase (which serves one camera's start at a time), which
    // cascades both down. Instead, tear the whole station down and bring every camera back together in one
    // clean multi-channel establishment — the state that reliably holds. See scheduleStationResync.
    const station = ctx.getCamera?.(slot.sn)?.stationSn ?? slot.sn;
    if (camsForStation(station).length > 1) return scheduleStationResync(station, `${slot.sn}: ${why}`);

    if (slot.restartTimer) return;
    const delay = cfg.stall.backoffMs[Math.min(slot.backoffIdx, cfg.stall.backoffMs.length - 1)];
    slot.backoffIdx++;
    console.log(`[bridge] ${slot.sn}: ${why} — reopening in ${delay} ms`);
    slot.restartTimer = setTimeout(() => {
      slot.restartTimer = null;
      void ensureWarm(slot.sn);
    }, delay);
  }

  /**
   * Restart EVERY camera on a station together (debounced, backoff per station). Tears down all their feeds
   * and the shared P2P session, then re-opens them all — so they re-establish in one clean multi-channel
   * setup instead of a lone camera fighting its sibling for the station. Backoff resets when bytes flow
   * again (onChunk). Standalone cameras never reach here (their station has one camera).
   */
  function scheduleStationResync(station, why) {
    let st = stationRestart.get(station);
    if (st?.timer) return; // a restart is already scheduled for this station
    if (!st) { st = { timer: null, idx: 0 }; stationRestart.set(station, st); }
    const delay = cfg.stall.backoffMs[Math.min(st.idx, cfg.stall.backoffMs.length - 1)];
    st.idx++;
    const cams = camsForStation(station);
    console.log(`[bridge] station ${station}: ${why} — restarting all ${cams.length} cameras together in ${delay} ms`);
    st.timer = setTimeout(async () => {
      st.timer = null;
      for (const c of cams) {
        const s = slotFor(c.sn);
        if (s.restartTimer) { clearTimeout(s.restartTimer); s.restartTimer = null; }
        closeFeed(s);
      }
      await ctx.sdk.dropStreamClient?.(cams[0]?.sn, station); // fresh session for the whole station
      for (const c of cams) void ensureWarm(c.sn);
    }, delay);
  }

  function closeFeed(slot) {
    const f = slot.feed;
    slot.feed = undefined;
    if (state.streaming.delete(slot.sn)) console.log(`[bridge] ${slot.sn}: stopped`);
    if (f) { f.removeAllListeners(); f.destroy(); }
  }

  /**
   * Open the camera's feed if it is not open. Idempotent; failures schedule a backoff retry.
   * A camera blocked by the LAN guard (WAN path) is still opened: only the resulting p2pConnect lets
   * the guard re-check the peer and clear the block, so refusing to reconnect would be a one-way door.
   */
  async function ensureWarm(sn) {
    const cam = ctx.getCamera?.(sn);
    if (cam && !cam.enabled) return;
    const slot = slotFor(sn);
    if (slot.feed || slot.opening) return;
    slot.opening = true;
    // Serialise opens per parent station: two cameras behind one HomeBase each open their own P2P
    // session (session-per-camera), and if they connect at once they race the station's level-2 E2E
    // key negotiation — the loser fails with "level-2 key not ready". Chaining the opens for a station
    // lets each session settle its key before the next starts. Standalone cams (station === own sn)
    // are their own chain, so different stations still open in parallel.
    const station = cam?.stationSn ?? sn;
    const prev = stationChains.get(station) ?? Promise.resolve();
    const mine = prev.catch(() => {}).then(() => openFeedInto(slot, sn));
    stationChains.set(station, mine);
    try {
      await mine;
    } finally {
      if (stationChains.get(station) === mine) stationChains.delete(station);
      slot.opening = false;
    }
  }

  /** The actual open, run serialised per station by ensureWarm. */
  async function openFeedInto(slot, sn) {
    if (slot.feed) return; // opened while queued
    try {
      const stationSn = ctx.getCamera?.(sn)?.stationSn;
      // After repeated open failures the cached client's P2P session is likely wedged: the device still
      // holds the dropped session and won't open a fresh responder port for the reused one, so every
      // reconnect just times out. Drop the client so the next streamClientFor() builds a brand-new session
      // (fresh cloud lookup → the device brokers a new port). Do it every Nth failure, not once, so a
      // device that needs a moment to release the old session gets more than one fresh attempt.
      if (slot.failures > 0 && slot.failures % cfg.stall.recreateClientAfter === 0) {
        const dropped = await ctx.sdk.dropStreamClient?.(sn, stationSn);
        if (dropped) console.log(`[bridge] ${sn}: ${slot.failures} consecutive failures — recreated stream client (fresh session)`);
      }
      const client = await ctx.sdk.streamClientFor(sn, stationSn);
      slot.client = client;
      ctx.attachLanGuard?.(client, sn);
      const feed = await ctx.sdk.openFeed(client, sn, { powered: ctx.getCamera?.(sn)?.powered });
      slot.feed = feed;
      slot.startedAt = Date.now();
      feed.on("data", (chunk) => onChunk(slot, chunk));
      const onEnd = (why) => () => {
        if (slot.feed !== feed) return;
        closeFeed(slot);
        noteFailure(slot);
        scheduleReopen(slot, why);
      };
      feed.on("error", (e) => { console.error(`[bridge] ${sn}: feed error: ${e?.message ?? e}`); onEnd("feed error")(); });
      feed.on("end", onEnd("feed ended"));
      feed.on("close", onEnd("feed closed"));
    } catch (e) {
      console.error(`[bridge] ${sn}: open failed: ${e?.message ?? e}`);
      noteFailure(slot);
      scheduleReopen(slot, "open failed");
    }
  }

  function noteFailure(slot) {
    slot.failures++;
    if (!slot.firstFailureAt) slot.firstFailureAt = Date.now();
  }

  /** Pipe the live feed into an HTTP response. Returns a detach function. */
  function attachConsumer(sn, res) {
    const slot = slotFor(sn);
    slot.consumers.add(res);
    if (slot.lastKeyChunk) res.write(slot.lastKeyChunk);
    void ensureWarm(sn);
    return () => slot.consumers.delete(res);
  }

  function streamStatus(sn) {
    const s = state.slots.get(sn);
    if (!s) return { streaming: false, stalls: 0, consumers: 0, failures: 0 };
    return {
      streaming: state.streaming.has(sn),
      stalls: s.stalls,
      codec: s.codec,
      width: s.width,
      height: s.height,
      lastBytesAgoMs: s.lastBytesAt ? Date.now() - s.lastBytesAt : null,
      consumers: s.consumers.size,
      failures: s.failures,
    };
  }

  /** Every 2 s: stall → restart; gap → drop consumers so go2rtc reconnects cleanly; long failure → exit. */
  function streamTick(now = Date.now()) {
    for (const slot of state.slots.values()) {
      // A reopened feed gets a fresh stallMs window from startedAt — lastBytesAt from a prior
      // (now-closed) feed must not count as silence against the new one.
      const since = Math.max(slot.lastBytesAt, slot.startedAt);
      const silent = since ? now - since : 0;
      const stallMs = globalThis.__ewStallMs ?? cfg.stall.stallMs; // runtime-tunable for multi-channel tests
      if (slot.feed && silent >= stallMs) {
        slot.stalls++;
        console.warn(`[bridge] ${slot.sn}: no bytes for ${Math.round(silent / 1000)} s — restarting feed (stall #${slot.stalls})`);
        closeFeed(slot);
        noteFailure(slot);
        scheduleReopen(slot, "stalled");
      }
      // Disconnect consumers ONCE per silent episode (not every tick): after they drop, go2rtc
      // reconnects, and without this latch the still-stale `since` would re-fire the disconnect every
      // 2 s — the "NNN s gap" churn. `gapFired` clears in onChunk when bytes flow again.
      if (silent >= cfg.stall.gapMs && slot.consumers.size && !slot.gapFired) {
        console.warn(`[bridge] ${slot.sn}: ${Math.round(silent / 1000)} s gap — disconnecting ${slot.consumers.size} consumer(s)`);
        for (const c of slot.consumers) c.end();
        slot.consumers.clear();
        slot.gapFired = true;
      }
      // A blocked camera (WAN-only station, force-LAN) fails by design; it must not restart the process.
      if (ctx.isBlocked?.(slot.sn)) { slot.firstFailureAt = 0; continue; }
      if (cfg.stall.exitAfterMs > 0 && slot.firstFailureAt && now - slot.firstFailureAt >= cfg.stall.exitAfterMs) {
        console.error(`[bridge] ${slot.sn}: failing continuously for ${Math.round((now - slot.firstFailureAt) / 1000)} s — exiting for a clean restart`);
        exit(1);
        return;
      }
    }
  }

  async function stopAll() {
    for (const slot of state.slots.values()) {
      if (slot.restartTimer) clearTimeout(slot.restartTimer);
      slot.restartTimer = null;
      for (const c of slot.consumers) c.end();
      slot.consumers.clear();
      closeFeed(slot);
    }
  }

  return { ensureWarm, attachConsumer, streamStatus, streamTick, stopAll };
}
