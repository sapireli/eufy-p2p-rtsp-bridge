// Camera registry: SDK device list ∩ config → the set of cameras this bridge serves, plus the /api shape.

import { streamKeys } from "./go2rtc.mjs";
import { setTimeout as sleep } from "node:timers/promises";
import { urlHost } from "./url-host.mjs";

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

export function createCameras(ctx, { describeAttempts = 3, describeRetryMs = 200, wait = sleep } = {}) {
  let cache = [];
  let missing = new Map();
  let discovering;
  const discovery = new AbortController();

  function cameraEntry(d, m) {
    const c = ctx.cfg.cameras[m.sn] ?? {};
    const powered = m.powerTier === "wired";
    const enabled = c.enabled ?? true;
    const mode = c.mode ?? (powered ? "always" : "on_motion");
    const modelKey = String(m.model ?? "").slice(0, 5).toUpperCase();
    const isDual = modelKey in DUAL_MODELS;
    if (!powered && mode === "always")
      throw new Error(`cameras.${m.sn}.mode=always requires power_override: always-on for a battery-budgeted camera`);
    const stationSn = d.raw?.parent_sn || d.stationSn || d.raw?.station_sn || m.sn;
    return {
      sn: m.sn,
      name: c.name ?? m.name,
      model: m.model,
      modelName: m.modelName,
      stationSn,
      standalone: stationSn === m.sn,
      battery: m.battery,
      powered,
      powerOverride: m.powerOverride ?? "auto",
      enabled,
      mode,
      holdSeconds: c.holdSeconds ?? ctx.cfg.defaults.holdSeconds,
      quality: c.quality ?? ctx.cfg.defaults.quality ?? null,
      codec: c.codec,
      isDual,
      viewModeCmd: isDual ? DUAL_MODELS[modelKey] : null,
      dualView: isDual ? (c.dualView ?? ctx.cfg.defaults.dualView) : null,
    };
  }

  async function describeCamera(sn) {
    for (let attempt = 1; attempt <= describeAttempts && !discovery.signal.aborted; attempt++) {
      try {
        const result = await ctx.sdk.describe(sn);
        if (!result || typeof result !== "object") throw new Error("SDK returned no device description");
        if (result.sn !== sn) throw new Error(`SDK returned serial ${result.sn ?? "missing"} for ${sn}`);
        return result;
      } catch (error) {
        if (discovery.signal.aborted) return null;
        console.error(`[bridge] ${sn}: describe attempt ${attempt}/${describeAttempts} failed: ${error?.message ?? error}`);
        if (attempt === describeAttempts) break;
        try { await wait(describeRetryMs * 2 ** (attempt - 1), undefined, { signal: discovery.signal }); }
        catch (waitError) { if (discovery.signal.aborted) return null; throw waitError; }
      }
    }
    if (!discovery.signal.aborted) console.error(`[bridge] ${sn}: camera omitted after ${describeAttempts} describe attempts; periodic discovery will retry`);
    return null;
  }

  async function refreshCameras() {
    if (discovery.signal.aborted) return cache;
    const devices = await ctx.eufy.getDevices();
    const out = [];
    const failed = new Map();
    for (const d of devices) {
      if (discovery.signal.aborted) return cache;
      const m = await describeCamera(d.sn);
      if (!m) { failed.set(d.sn, d); continue; }
      if (!m.isCamera) continue;
      out.push(cameraEntry(d, m));
    }
    if (!discovery.signal.aborted) { cache = out; missing = failed; }
    return out;
  }

  function retryMissingCameras(onFound) {
    if (discovering) return discovering;
    discovering = (async () => {
      const added = [];
      for (const [sn, d] of missing) {
        if (discovery.signal.aborted) break;
        try {
          const m = await ctx.sdk.describe(sn);
          if (!m || m.sn !== sn) throw new Error("SDK returned an invalid device description");
          if (!m.isCamera) { missing.delete(sn); continue; }
          const cam = cameraEntry(d, m);
          const key = cam.enabled ? streamKeys([...cache.filter((c) => c.enabled), cam]).get(sn) : sn;
          await onFound(cam, key, discovery.signal);
          if (discovery.signal.aborted) break;
          cache = [...cache, cam];
          missing.delete(sn);
          added.push(cam);
        } catch (error) {
          if (!discovery.signal.aborted) console.error(`[bridge] ${sn}: rediscovery failed: ${error?.message ?? error}; will retry`);
        }
      }
      return added;
    })();
    return discovering.finally(() => { discovering = undefined; });
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
      codec: st.codec ?? cam.codec ?? null,
      width: st.width,
      height: st.height,
      streaming: Boolean(st.streaming),
      stalls: st.stalls ?? 0,
      blocked: ctx.state.blocked.get(cam.sn) ?? null,
      // The go2rtc stream is keyed by the camera's name, not its serial — see streamKeys(). Reported
      // here so a client uses the bridge's key rather than deriving its own and getting it subtly wrong.
      streamKey: streamKeyFor(cam.sn),
      rtsp: `rtsp://${urlHost(host)}:8554/${streamKeyFor(cam.sn)}`,
      stream: `/stream/${cam.sn}`,
    };
  }

  return { refreshCameras, retryMissingCameras, missingCameraSerials: () => [...missing.keys()], stopCameraDiscovery: () => discovery.abort(), listCameras, getCamera, apiShape, streamKeyFor };
}
