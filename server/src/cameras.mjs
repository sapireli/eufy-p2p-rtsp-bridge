// Camera registry: SDK device list ∩ config → the set of cameras this bridge serves, plus the /api shape.

import { streamKeys, nativeRenditionFor } from "./go2rtc.mjs";

/** Dual-lens models → the SET_PAYLOAD sub-command that sets their composed view (from bropat's client). */
export const DUAL_MODELS = {
  T8214: 6243, // Battery Doorbell E340
  T8425: 6243, // Floodlight Cam E340
  T8170: 6243, // SoloCam S340
  T8171: 6243, // SoloCam S340 variant
  T8416: 6243, // Indoor Cam S350
  T8213: 2700, // Battery Doorbell 2K Dual
  T8203: 2700, // Wired Doorbell Dual
};

/** Config names → wire `video_type` values (bropat DualCamWatchViewMode states). */
export const DUAL_VIEW_VALUES = { "pip-tl": 2, "pip-tr": 3, "pip-bl": 4, "pip-br": 5, split: 12, single: 0 };

export function createCameras(ctx) {
  let cache = [];

  async function refreshCameras({ config = ctx.cfg, commit = true } = {}) {
    const devices = await ctx.eufy.getDevices();
    const out = [];
    for (const d of devices) {
      let m;
      try {
        m = await ctx.sdk.describe(d.sn);
      } catch (e) {
        console.error(`[bridge] ${d.sn}: describe failed: ${e?.message ?? e}`);
        continue;
      }
      if (!m.isCamera) continue;
      const c = config.cameras[m.sn] ?? {};
      const claim = c.powerOverride ?? (m.automaticPowerTier !== undefined ? "auto" : m.powerOverride ?? "auto");
      const powered = claim === "auto" ? (m.automaticPowerTier ?? m.powerTier) === "wired" : claim === "always-on";
      // A battery camera is enabled now, but it does not stream continuously: its mode decides when.
      // Phase 1 skipped them outright because always-on is the only thing it could do with one.
      const enabled = c.enabled ?? true;
      const mode = c.mode ?? (powered ? "always" : "on_motion");
      const modelKey = String(m.model ?? "").slice(0, 5).toUpperCase();
      const isDual = modelKey in DUAL_MODELS;
      if (!powered && mode === "always")
        throw new Error(`cameras.${m.sn}.mode=always requires power_override: always-on for a battery-budgeted camera`);
      // Parent station (HomeBase) serial; equals the camera's own sn for a standalone camera. Used to
      // serialise per-HomeBase P2P session opens (so their level-2 E2E keys don't race) and to decide
      // which cameras need the local-port sweep (HomeBase-attached only).
      const stationSn = d.raw?.parent_sn || d.stationSn || d.raw?.station_sn || m.sn;
      out.push({
        sn: m.sn,
        name: c.name ?? m.name,
        model: m.model,
        modelName: m.modelName,
        stationSn,
        standalone: stationSn === m.sn,
        battery: m.battery,
        powered,
        powerOverride: claim,
        enabled,
        mode,
        holdSeconds: c.holdSeconds ?? config.defaults.holdSeconds,
        quality: c.quality ?? config.defaults.quality ?? null,
        // Declared codec, if the operator set one. go2rtc prefers what a live feed actually reported and
        // falls back to this, so declaring it only removes the cold-start probe — it cannot be wrong for
        // long if it disagrees with the device.
        codec: c.codec,
        transcode: c.transcode,
        vaapiDecode: c.vaapiDecode,
        isDual,
        viewModeCmd: isDual ? DUAL_MODELS[modelKey] : null,
        dualView: isDual ? (ctx.viewModes?.get(m.sn) ?? c.dualView ?? config.defaults.dualView) : null,
      });
    }
    for (const key of Object.keys(config.go2rtcImported?.streams ?? {})) {
      if (out.some((camera) => camera.sn === key)) throw new Error(`imported camera id conflicts with SDK camera: ${key}`);
      const c = config.cameras[key] ?? {};
      if (c.mode && c.mode !== "always") throw new Error(`imported camera ${key} supports always mode only`);
      if (c.transcode && c.transcode !== "never") throw new Error(`imported camera ${key} uses direct passthrough`);
      out.push({ sn: key, name: c.name ?? key, model: "external", modelName: "go2rtc camera",
        external: true, enabled: c.enabled ?? true, powered: true, powerOverride: "auto", mode: "always",
        holdSeconds: config.defaults.holdSeconds, quality: null, codec: c.codec, transcode: "never",
        isDual: false, dualView: null });
    }
    if (commit) cache = out;
    return out;
  }

  const listCameras = () => cache;
  const getCamera = (sn) => cache.find((c) => c.sn === sn);

  /** What GET /api/cameras returns per camera. `host` is the address clients should dial for RTSP. */
  /** This camera's go2rtc stream key, resolved against the whole enabled list so collisions are stable. */
  function streamKeyFor(sn) {
    return streamKeys(listCameras().filter((c) => c.enabled)).get(sn) ?? sn;
  }

  function apiShape(cam, host) {
    const st = ctx.streamStatus?.(cam.sn) ?? {};
    const freshness = ctx.freshness?.capability(cam, host);
    return {
      sn: cam.sn,
      name: cam.name,
      model: cam.model,
      modelName: cam.modelName,
      enabled: cam.enabled,
      // Phase 2: a camera is not simply on or off any more. `mode` says whether it streams continuously,
      // only while something holds it, or only when asked; `holdSeconds` is how long one hold lasts.
      mode: cam.mode,
      holdSeconds: cam.holdSeconds,
      held: ctx.holds?.isHeld?.(cam.sn) ?? false,
      powered: cam.powered,
      powerOverride: cam.powerOverride,
      dual: cam.isDual,
      dualView: cam.dualView,
      quality: cam.quality,
      codec: st.codec ?? cam.codec,
      width: st.width,
      height: st.height,
      streaming: Boolean(st.streaming),
      stalls: st.stalls ?? 0,
      blocked: ctx.state.blocked.get(cam.sn) ?? null,
      // The go2rtc stream is keyed by the camera's name, not its serial — see streamKeys(). Reported
      // here so a client uses the bridge's key rather than deriving its own and getting it subtly wrong.
      streamKey: streamKeyFor(cam.sn),
      rtsp: `rtsp://${host}:${ctx.cfg.rtspPort ?? 8554}/${streamKeyFor(cam.sn)}`,
      // go2rtc can repacketize this TCP consumer without changing encoded video.
      // Larger RTP payloads avoid per-packet CPU saturation on small Linux clients.
      // Keep the regular URL unchanged: this is not suitable for UDP/MTU-sized RTP.
      rtspTcpPacketSize: 8192,
      ...(freshness ? { freshness } : {}),
      ...(nativeRenditionFor(ctx, cam, host) ? { nativeRendition: nativeRenditionFor(ctx, cam, host) } : {}),
      ...(st.freshness ? { freshnessStatus: st.freshness } : {}),
      ...(st.sourceReceipt ? { sourceReceipt: st.sourceReceipt } : {}),
      stream: cam.external ? null : `/stream/${cam.sn}`,
    };
  }

  return { refreshCameras, listCameras, getCamera, apiShape, streamKeyFor };
}
