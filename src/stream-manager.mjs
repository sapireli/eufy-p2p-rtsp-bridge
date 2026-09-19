// Always-on media core. One Slot per enabled camera holds the SDK's Annex-B Readable ("feed"), fans its
// chunks out to HTTP consumers (go2rtc), and is watched for stalls. Watchdog thresholds are ported from
// eufy-frigate-bridge (12 s stall / 45 s gap / 300 s exit), which were calibrated over a 19 h run.
import { newSlot } from "./state.mjs";

export function createStreamManager(ctx) {
  const { state, cfg } = ctx;
  const exit = (code) => (ctx.exit ?? process.exit)(code);
  const slotFor = (sn) => state.slots.get(sn) ?? state.slots.set(sn, newSlot(sn)).get(sn);

  function onChunk(slot, chunk) {
    const now = Date.now();
    slot.lastBytesAt = now;
    slot.firstFailureAt = 0;
    slot.failures = 0;
    slot.backoffIdx = 0;
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
    if (slot.restartTimer) return;
    const delay = cfg.stall.backoffMs[Math.min(slot.backoffIdx, cfg.stall.backoffMs.length - 1)];
    slot.backoffIdx++;
    console.log(`[bridge] ${slot.sn}: ${why} — reopening in ${delay} ms`);
    slot.restartTimer = setTimeout(() => {
      slot.restartTimer = null;
      void ensureWarm(slot.sn);
    }, delay);
  }

  function closeFeed(slot) {
    const f = slot.feed;
    slot.feed = undefined;
    if (state.streaming.delete(slot.sn)) console.log(`[bridge] ${slot.sn}: stopped`);
    if (f) { f.removeAllListeners(); f.destroy(); }
  }

  /** Open the camera's feed if it is not open. Idempotent; failures schedule a backoff retry. */
  async function ensureWarm(sn) {
    const cam = ctx.getCamera?.(sn);
    if (cam && !cam.enabled) return;
    if (ctx.isBlocked?.(sn)) return;
    const slot = slotFor(sn);
    if (slot.feed || slot.opening) return;
    slot.opening = true;
    try {
      const client = await ctx.sdk.streamClientFor(sn);
      slot.client = client;
      ctx.attachLanGuard?.(client, sn);
      const feed = await ctx.sdk.openFeed(client, sn);
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
    } finally {
      slot.opening = false;
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
      if (slot.feed && silent >= cfg.stall.stallMs) {
        slot.stalls++;
        console.warn(`[bridge] ${slot.sn}: no bytes for ${Math.round(silent / 1000)} s — restarting feed (stall #${slot.stalls})`);
        closeFeed(slot);
        noteFailure(slot);
        scheduleReopen(slot, "stalled");
      }
      if (silent >= cfg.stall.gapMs && slot.consumers.size) {
        console.warn(`[bridge] ${slot.sn}: ${Math.round(silent / 1000)} s gap — disconnecting ${slot.consumers.size} consumer(s)`);
        for (const c of slot.consumers) c.end();
        slot.consumers.clear();
      }
      if (slot.firstFailureAt && now - slot.firstFailureAt >= cfg.stall.exitAfterMs) {
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
