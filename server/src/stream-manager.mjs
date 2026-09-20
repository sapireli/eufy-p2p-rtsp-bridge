// Always-on media core. One Slot per enabled camera holds the SDK's Annex-B Readable ("feed"), fans its
// chunks out to HTTP consumers (go2rtc), and is watched for stalls. Watchdog thresholds are ported from
// eufy-frigate-bridge (12 s stall / 45 s gap / 300 s exit), which were calibrated over a 19 h run.
import { newSlot } from "./state.mjs";

export function createStreamManager(ctx) {
  const { state, cfg } = ctx;
  const exit = (code) => (ctx.exit ?? process.exit)(code);
  const slotFor = (sn) => state.slots.get(sn) ?? state.slots.set(sn, newSlot(sn)).get(sn);
  const stationChains = new Map(); // parentStationSn -> Promise: serialises P2P opens per HomeBase

  function onChunk(slot, chunk) {
    const now = Date.now();
    slot.lastBytesAt = now;
    slot.firstFailureAt = 0;
    slot.failures = 0;
    slot.backoffIdx = 0;
    slot.gapFired = false; // bytes are flowing again → allow one fresh gap-disconnect if it stalls later
    if (!state.streaming.has(slot.sn)) {
      state.starting.delete(slot.sn);
      state.streaming.add(slot.sn);
      console.log(`[bridge] ${slot.sn}: streaming`);
      ctx.broadcastEvent?.({ type: "streamState", sn: slot.sn, state: "live" });
    }
    const sets = ctx.sdk.extractParamSets(chunk); // non-undefined ⇒ this chunk carries SPS/PPS (keyframe AU)
    if (sets) {
      slot.lastKeyChunk = chunk;
      const g = ctx.sdk.codedGeometry(sets);
      // Compare GEOMETRY as well as codec. Live-view quality is "Auto" on these cameras (streamingQuality
      // tier 0) and the station re-picks a resolution on its own, so a stream can change size mid-session.
      // An RTSP consumer negotiated its SDP from the first keyframe, so once the size moves under -c:v copy
      // its decoder is set up for the old one and the picture freezes. Dropping the consumers makes go2rtc
      // re-run its source and hand out an SDP that matches what the camera is actually sending now.
      const differs = slot.codec !== sets.codec || slot.width !== g?.width || slot.height !== g?.height;
      // CONFIRM a change before acting on it. Parameter sets are read from a chunk, and a keyframe whose
      // SPS straddles a chunk boundary parses into a plausible-looking but wrong size. Measured on the
      // Balcony camera: 13 "changes" between 1080p and 720p in one session while a capture of the very
      // same stream was 1280x720 from end to end. Acting on those tore consumers off and re-synced go2rtc
      // repeatedly, which is what a viewer saw as a frozen picture.
      //
      // A real change persists into the next keyframe; a misparse does not. So a differing reading is
      // remembered and only believed when the following one agrees with it.
      // The FIRST reading is taken as-is: there is nothing established to contradict and no consumer
      // negotiated against it yet. Only a change away from a known geometry has to be confirmed.
      const unconfirmed =
        differs &&
        slot.codec !== undefined &&
        (slot.pendingGeom?.codec !== sets.codec || slot.pendingGeom?.width !== g?.width || slot.pendingGeom?.height !== g?.height);
      if (unconfirmed) slot.pendingGeom = { codec: sets.codec, width: g?.width, height: g?.height };
      else slot.pendingGeom = undefined;
      const moved = slot.codec !== undefined && differs && !unconfirmed;
      if (differs && !unconfirmed) {
        const from = slot.codec ? `${slot.codec} ${slot.width ?? "?"}x${slot.height ?? "?"} → ` : "";
        slot.codec = sets.codec;
        slot.width = g?.width;
        slot.height = g?.height;
        console.log(`[bridge] ${slot.sn}: codec ${from}${sets.codec} ${slot.width ?? "?"}x${slot.height ?? "?"}`);
        if (sets.codec !== "h264")
          console.warn(`[bridge] ${slot.sn}: stream is ${sets.codec.toUpperCase()} — players without an HEVC decoder (Chrome, the Pi) cannot read it; set a lower streaming quality in the owner's eufy app, or enable go2rtc.transcode.`);
        if (moved && slot.consumers.size) {
          console.warn(`[bridge] ${slot.sn}: stream geometry changed mid-session — dropping ${slot.consumers.size} consumer(s) so RTSP re-negotiates`);
          for (const c of slot.consumers) c.end();
          slot.consumers.clear();
        }
        void ctx.syncGo2rtc?.().catch(() => {}); // codec just became known: go2rtc may need a different egress
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

  /**
   * Whether this camera should be streaming right now.
   *
   * An always-on camera always should — that is Phase 1. Anything else streams only while held, so a
   * battery camera does not get dragged back up by the stall watchdog or a reopen after its hold expired.
   */
  function wanted(sn) {
    const cam = ctx.getCamera?.(sn);
    if (!cam?.enabled) return false;
    if ((cam.mode ?? "always") === "always") return true;
    return ctx.holds?.isHeld?.(sn) === true;
  }

  function scheduleReopen(slot, why) {
    // Solo per-camera recovery. The SDK opens an independent per-camera media session, so one camera's
    // failure never needs to disturb a sibling. openFeedInto tears the station's session down before
    // retrying (see there), so each attempt re-runs the lookup and picks up a fresh, live port.
    if (slot.restartTimer) return;
    if (!wanted(slot.sn)) return; // nothing wants it any more: let it stay down
    const delay = cfg.stall.backoffMs[Math.min(slot.backoffIdx, cfg.stall.backoffMs.length - 1)];
    slot.backoffIdx++;
    console.log(`[bridge] ${slot.sn}: ${why} — reopening in ${delay} ms`);
    slot.restartTimer = setTimeout(() => {
      slot.restartTimer = null;
      void ensureWarm(slot.sn);
    }, delay);
  }

  function closeFeed(slot) {
    state.starting.delete(slot.sn);
    const f = slot.feed;
    slot.feed = undefined;
    if (state.streaming.delete(slot.sn)) {
      console.log(`[bridge] ${slot.sn}: stopped`);
      ctx.broadcastEvent?.({ type: "streamState", sn: slot.sn, state: "idle" });
    }
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
    if (!wanted(sn)) return;
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
    // A battery camera takes a second or two to wake. Saying so lets a tile show a still and a "coming"
    // state instead of a black rectangle, which otherwise looks indistinguishable from a broken camera.
    if (!state.streaming.has(sn)) {
      state.starting.add(sn);
      ctx.broadcastEvent?.({ type: "streamState", sn, state: "starting" });
    }
    try {
      const stationSn = ctx.getCamera?.(sn)?.stationSn;
      // Tear the station's P2P session down before retrying: a reused session keeps CHECK_CAM-ing the same
      // stale looked-up port and never picks up a newer address, so a full teardown makes the next open
      // re-run the lookup and target the station's current live port. Every Nth failure (default 1 = each).
      if (cfg.stall.recreateClientAfter > 0 && slot.failures > 0 && slot.failures % cfg.stall.recreateClientAfter === 0) {
        const dropped = await ctx.sdk.dropStreamClient?.(sn, stationSn);
        if (dropped) console.log(`[bridge] ${sn}: ${slot.failures} failure(s) — tore down station session; next open re-lookups a fresh port`);
      }
      const client = await ctx.sdk.streamClientFor(sn, stationSn);
      slot.client = client;
      ctx.attachLanGuard?.(client, sn);
      const cam = ctx.getCamera?.(sn);
      const feed = await ctx.sdk.openFeed(client, sn, { powered: cam?.powered, standalone: cam?.standalone });
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
      if (slot.feed && silent >= stallMs && !wanted(slot.sn)) {
        closeFeed(slot); // its hold went away mid-stream; stopping is not a stall
        continue;
      }
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

  /** Force a camera to drop its current feed and reopen (used by the LAN-upgrade driver after it flips a
   *  station's force/TURN flags, so the next connect re-evaluates the path). Safe if no feed is open. */
  function restartCamera(sn) {
    const slot = state.slots.get(sn);
    if (slot) {
      if (slot.restartTimer) { clearTimeout(slot.restartTimer); slot.restartTimer = null; }
      slot.backoffIdx = 0;
      closeFeed(slot);
    }
    void ensureWarm(sn);
  }

  /**
   * Stop one camera and leave it down. Used when its last hold expires: unlike a stall or a feed error
   * this is not a failure, so it must not schedule a reopen.
   */
  function stopCamera(sn) {
    const slot = state.slots.get(sn);
    if (!slot) return;
    if (slot.restartTimer) {
      clearTimeout(slot.restartTimer);
      slot.restartTimer = null;
    }
    slot.failures = 0;
    slot.firstFailureAt = 0;
    slot.backoffIdx = 0;
    closeFeed(slot);
  }

  return { ensureWarm, restartCamera, stopCamera, attachConsumer, streamStatus, streamTick, stopAll };
}
