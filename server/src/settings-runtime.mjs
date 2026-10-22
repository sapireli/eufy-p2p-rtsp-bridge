// Persist desired settings first; apply camera-scoped policies without restarting siblings.
import { loadConfig } from './config.mjs';

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
export function configForSettings(settings, cfg) {
  return loadConfig({ env: {}, raw: {
    ...settings, eufy: { email: cfg.email, password: cfg.password, country: cfg.country },
    data_dir: cfg.dataDir, host: cfg.host, port: cfg.port, rtsp_port: cfg.rtspPort,
    go2rtc_api_port: cfg.go2rtcApiPort, self_host: cfg.selfHost, go2rtc_bin: cfg.go2rtcBin,
  } }).cfg;
}

export function createSettingsRuntime(ctx, store) {
  let applied = store.load();
  let status = { state: 'applied', revision: applied.revision, restartRequired: [] };
  let queue = Promise.resolve();
  const snapshot = () => ({ ...store.load(), apply: { ...structuredClone(status), scope: 'bridge-policy', cameraVideoVerified: false } });
  async function update(operation) {
    // The store validates the full document before writing. Validate device-dependent policy too.
    const preview = operation.preview();
    const nextCfg = configForSettings(preview, ctx.cfg);
    if (ctx.state.flags.ready) await ctx.refreshCameras({ config: nextCfg, commit: false });
    const saved = operation.commit();
    const restartRequired = [];
    for (const key of ['lan', 'go2rtc', 'freshness', 'poll_ms']) {
      if (!same(saved.settings[key], applied.settings[key])) restartRequired.push(key);
    }
    if (!same(saved.settings.defaults?.motion_events, applied.settings.defaults?.motion_events)) restartRequired.push('defaults.motion_events');
    const cameras = new Set([...Object.keys(applied.settings.cameras ?? {}), ...Object.keys(saved.settings.cameras ?? {})]);
    for (const sn of cameras) for (const field of ['name', 'enabled', 'codec', 'transcode', 'vaapi_decode']) {
      if (!same(saved.settings.cameras?.[sn]?.[field], applied.settings.cameras?.[sn]?.[field])) restartRequired.push(`cameras.${sn}.${field}`);
    }
    status = { state: 'applying', revision: saved.revision, restartRequired };
    try {
      // These structures are read at use time; keep the cfg object identity captured by workers.
      const oldCameras = ctx.cfg.cameras;
      const oldDefaults = ctx.cfg.defaults;
      const runtimeCameras = structuredClone(nextCfg.cameras);
      for (const sn of new Set([...Object.keys(oldCameras), ...Object.keys(runtimeCameras)])) {
        runtimeCameras[sn] ??= {};
        for (const field of ['name', 'enabled', 'codec', 'transcode', 'vaapiDecode']) {
          if (Object.hasOwn(oldCameras[sn] ?? {}, field)) runtimeCameras[sn][field] = oldCameras[sn][field];
          else delete runtimeCameras[sn][field];
        }
      }
      ctx.cfg.cameras = runtimeCameras;
      ctx.cfg.defaults = { ...nextCfg.defaults, motionEvents: oldDefaults.motionEvents };
      ctx.cfg.stall = nextCfg.stall;
      if (ctx.state.flags.ready) {
        const old = new Map(ctx.listCameras().map(c => [c.sn, { ...c }]));
        const current = await ctx.refreshCameras();
        for (const cam of current) {
          const previous = old.get(cam.sn);
          if (!previous) continue;
          if (previous.powerOverride !== cam.powerOverride) {
            ctx.stopCamera(cam.sn);
            await ctx.sdk.dropStreamClient(cam.sn);
          }
          if (!cam.enabled || (cam.mode !== 'always' && !ctx.holds.isHeld(cam.sn))) ctx.stopCamera(cam.sn);
          else if (cam.mode === 'always' || ctx.holds.isHeld(cam.sn)) void ctx.ensureWarm(cam.sn);
          if (previous.dualView !== cam.dualView || previous.quality !== cam.quality) {
            const result = await ctx.applyPins(cam.sn, { force: true });
            if (Object.values(result).some(v => v === 'error' || v === 'unsupported')) throw new Error(`Camera ${cam.sn} could not apply its requested view or quality; desired settings remain saved`);
          }
        }
        await ctx.ws?.broadcastSnapshot?.();
      }
      status = { state: restartRequired.length ? 'restart-required' : 'applied', revision: saved.revision, restartRequired };
      if (!restartRequired.length) applied = saved;
    } catch (error) {
      status = { state: 'failed', revision: saved.revision, restartRequired, error: error.message };
    }
    ctx.broadcastEvent?.({ type: 'settingsChanged', revision: saved.revision, apply: status });
    return snapshot();
  }
  function enqueue(operation) {
    const result = queue.catch(() => {}).then(() => update(operation));
    queue = result;
    return result;
  }
  return {
    snapshot,
    patch(input) { return enqueue({ preview: () => store.previewPatch(input), commit: () => store.patch(input) }); },
    import(input) { return enqueue({ preview: () => store.previewImport(input), commit: () => store.import(input) }); },
  };
}
