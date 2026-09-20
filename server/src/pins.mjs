// Per-camera settings the wall depends on, re-applied at boot and after every recovery:
//  - dual-lens view mode (6243/2700 SET_PAYLOAD {restore:1, video_type}) — one composed stream per device
//  - live-view quality tier — selects H.264 (720p/1080p) vs H.265 (2K) on most models. The SDK 0.1.1 has
//    no verified setter for it; when setProperty throws we say so once and leave it to the eufy app.
import { DUAL_VIEW_VALUES } from "./cameras.mjs";

/**
 * Shortest gap between two pin passes for the same camera.
 *
 * Pinning sends a SET_PAYLOAD to a camera that may be MID-STREAM, and a station opening several media
 * sessions at once fires a connect event for each. Without this, one camera coming up re-pokes its
 * siblings several times over. A genuinely new session still gets its pins — just once.
 */
const REPIN_MIN_GAP_MS = 30_000;

export function createPins(ctx) {
  const warnedQuality = new Set();
  const lastPinnedAt = new Map(); // sn -> ms

  async function applyPins(sn, { force = false } = {}) {
    const cam = ctx.getCamera(sn);
    const result = { dualView: "skip", quality: "skip" };
    if (!cam || !cam.enabled) return result;
    const last = lastPinnedAt.get(sn) ?? 0;
    if (!force && Date.now() - last < REPIN_MIN_GAP_MS) return { dualView: "recent", quality: "recent" };
    lastPinnedAt.set(sn, Date.now());

    if (cam.isDual && cam.viewModeCmd && cam.dualView) {
      try {
        const client = await ctx.sdk.streamClientFor(sn, cam.stationSn); // same station session the stream will use
        await ctx.sdk.sendSetPayload(client, sn, cam.viewModeCmd, { restore: 1, video_type: DUAL_VIEW_VALUES[cam.dualView] });
        console.log(`[bridge] ${sn}: dual view pinned to ${cam.dualView} (cmd ${cam.viewModeCmd})`);
        result.dualView = "set";
      } catch (e) {
        console.error(`[bridge] ${sn}: dual view pin failed: ${e?.message ?? e}`);
        result.dualView = "error";
      }
    }

    if (cam.quality) {
      try {
        await ctx.eufy.setProperty(sn, "streamingQuality", cam.quality);
        console.log(`[bridge] ${sn}: streaming quality pinned to ${cam.quality}`);
        result.quality = "set";
      } catch (e) {
        const msg = String(e?.message ?? e);
        if (/unverified|not supported|CapabilityNotSupported/i.test(msg) || e?.name === "CapabilityNotSupportedError") {
          if (!warnedQuality.has(sn))
            console.warn(`[bridge] ${sn}: SDK cannot set streaming quality yet (${msg}) — set "${cam.quality}" in the eufy app: camera → Settings → Video → Streaming quality. The codec check below will tell you if the stream is not H.264.`);
          warnedQuality.add(sn);
          result.quality = "unsupported";
        } else {
          console.error(`[bridge] ${sn}: streaming quality pin failed: ${msg}`);
          result.quality = "error";
        }
      }
    }
    return result;
  }

  async function applyAllPins() {
    const out = {};
    // Boot and post-re-login passes are deliberate, not incidental: they bypass the repin gap.
    for (const cam of ctx.listCameras()) if (cam.enabled) out[cam.sn] = await applyPins(cam.sn, { force: true });
    return out;
  }

  return { applyPins, applyAllPins };
}
