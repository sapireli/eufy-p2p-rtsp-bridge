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
      const enabled = c.enabled ?? !m.battery;
      const modelKey = String(m.model ?? "").slice(0, 5).toUpperCase();
      const isDual = modelKey in DUAL_MODELS;
      if (m.battery && c.enabled == null) console.log(`[bridge] ${m.sn} (${m.name}) is battery-powered — skipped in phase 1 (set cameras.${m.sn}.enabled: true to force)`);
      out.push({
        sn: m.sn,
        name: c.name ?? m.name,
        model: m.model,
        modelName: m.modelName,
        battery: m.battery,
        enabled,
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
      powered: !cam.battery,
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
