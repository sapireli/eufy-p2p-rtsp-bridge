// Camera registry: SDK device list ∩ config → the set of cameras this bridge serves, plus the /api shape.
// Phase 1 rule: wired cameras are in unless `enabled: false`; battery cameras are out unless `enabled: true`.

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

/**
 * Camera models the SDK tags with a `battery` capability although they are mains-powered (mirrors the
 * SDK's own MAINS_CAMERA_MODELS plus the Floodlight Cam 2 Pro, which reports no battery params at all).
 */
export const MAINS_MODELS = ["T8425", "T8419", "T8423"];

/**
 * Is this camera safe to stream 24/7? Evidence beats the capability flag: no `battery` capability, a
 * known mains model, no reported battery level, or a battery that is currently charging (hardwired
 * doorbell) all mean "powered". `cameras.<sn>.enabled` in config still overrides the answer.
 */
export function isPowered(m) {
  if (!m.battery) return true;
  const model = String(m.model ?? "").toUpperCase();
  if (MAINS_MODELS.some((p) => model.startsWith(p))) return true;
  if (m.batteryLevel == null) return true;
  if (m.charging === true) return true;
  return false;
}

export function createCameras(ctx) {
  let cache = [];

  async function refreshCameras() {
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
      const c = ctx.cfg.cameras[m.sn] ?? {};
      const powered = isPowered(m);
      // A battery camera is enabled now, but it does not stream continuously: its mode decides when.
      // Phase 1 skipped them outright because always-on is the only thing it could do with one.
      const enabled = c.enabled ?? true;
      const mode = c.mode ?? (powered ? "always" : "on_motion");
      const modelKey = String(m.model ?? "").slice(0, 5).toUpperCase();
      const isDual = modelKey in DUAL_MODELS;
      if (!powered && mode === "always")
        console.warn(`[bridge] ${m.sn} (${m.name}) is battery-powered but mode=always — a continuous stream keeps it awake and will flatten it; use on_motion or on_demand`);
      // Parent station (HomeBase) serial; equals the camera's own sn for a standalone camera. Used to
      // serialise per-HomeBase P2P session opens (so their level-2 E2E keys don't race) and to decide
      // which cameras need the local-port sweep (HomeBase-attached only).
      const stationSn = d.stationSn ?? d.raw?.station_sn ?? m.sn;
      out.push({
        sn: m.sn,
        name: c.name ?? m.name,
        model: m.model,
        modelName: m.modelName,
        stationSn,
        standalone: stationSn === m.sn,
        battery: m.battery,
        powered,
        enabled,
        mode,
        holdSeconds: c.holdSeconds ?? ctx.cfg.defaults.holdSeconds,
        quality: c.quality ?? ctx.cfg.defaults.quality ?? null,
        isDual,
        viewModeCmd: isDual ? DUAL_MODELS[modelKey] : null,
        dualView: isDual ? (c.dualView ?? ctx.cfg.defaults.dualView) : null,
      });
    }
    cache = out;
    return out;
  }

  const listCameras = () => cache;
  const getCamera = (sn) => cache.find((c) => c.sn === sn);

  /** What GET /api/cameras returns per camera. `host` is the address clients should dial for RTSP. */
  function apiShape(cam, host) {
    const st = ctx.streamStatus?.(cam.sn) ?? {};
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
      dual: cam.isDual,
      dualView: cam.dualView,
      quality: cam.quality,
      codec: st.codec,
      width: st.width,
      height: st.height,
      streaming: Boolean(st.streaming),
      stalls: st.stalls ?? 0,
      blocked: ctx.state.blocked.get(cam.sn) ?? null,
      rtsp: `rtsp://${host}:8554/${cam.sn}`,
      stream: `/stream/${cam.sn}`,
    };
  }

  return { refreshCameras, listCameras, getCamera, apiShape };
}
