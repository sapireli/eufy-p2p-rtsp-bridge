// Per-camera settings the wall depends on, re-applied at boot and after every recovery:
//  - dual-lens view mode (6243/2700 SET_PAYLOAD {restore:1, video_type}) — one composed stream per device
//  - live-view quality tier — selects H.264 (720p/1080p) vs H.265 (2K) on most models. The SDK 0.1.1 has
//    no verified setter for it; when setProperty throws we say so once and leave it to the eufy app.
import { DUAL_VIEW_VALUES } from "./cameras.mjs";

export function createPins(ctx) {
  const warnedQuality = new Set();

  async function applyPins(sn) {
    const cam = ctx.getCamera(sn);
    const result = { dualView: "skip", quality: "skip" };
    if (!cam || !cam.enabled) return result;

    if (cam.isDual && cam.viewModeCmd && cam.dualView) {
      try {
        const client = await ctx.sdk.streamClientFor(sn); // same session the stream will use
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
    for (const cam of ctx.listCameras()) if (cam.enabled) out[cam.sn] = await applyPins(cam.sn);
    return out;
  }

  return { applyPins, applyAllPins };
}
