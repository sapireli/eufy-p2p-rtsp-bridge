import { urlHost } from "./network.mjs";
// go2rtc lifecycle: write its yaml from the enabled camera list (vendored generator) and run the binary
// as a child, restarting it with a short delay if it dies. Not fatal when the binary is missing (dev).
import { spawn } from "node:child_process";
import { readFile, writeFile, rename, rm } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { randomUUID } from "node:crypto";
import { parseDocument } from "yaml";
import { writeGo2rtcConfig } from "./vendor/ha-bridge/go2rtc-config.mjs";

/** Choose passthrough or transcoding without changing the camera source codec.
 * Linux transcodes use the hardware-first launcher with software fallback. Other
 * platforms retain go2rtc hardware presets (and the resized macOS encoder preset).
 */
export function egressFor(codec, mode, maxHeight = 0, platform = process.platform, vaapiDevice = "") {
  if (mode === "never") return "#video=copy";
  const transcode = mode === "always" || codec === "h265";
  if (!transcode) return "#video=copy";
  if (platform === "linux") return "#input=ewb_dynamic_http#video=tvh264";
  const video = platform === "darwin" && maxHeight > 0 ? "tvh264" : "h264#hardware";
  return `#video=${video}${maxHeight > 0 ? `#height=${maxHeight}` : ""}`;
}

/**
 * Harden the generated go2rtc.yaml: upstream (an HA add-on behind its own auth) opens the go2rtc API
 * and WebRTC on every interface, unauthenticated. The wall's clients only pull RTSP on :8554, so the
 * API is pinned to loopback and WebRTC is disabled (go2rtc enables it by default even without a
 * `webrtc:` block, so the block must exist with an empty listen). Done as a post-pass so the vendored generator
 * stays verbatim. Comments in the generated file are preserved (yaml Document round-trip).
 */
export function hardenGo2rtcYaml(text, { rtspPort = 8554, apiPort = 1984 } = {}) {
  const doc = parseDocument(text);
  doc.setIn(["api", "listen"], `127.0.0.1:${apiPort}`);
  doc.setIn(["rtsp", "listen"], `:${rtspPort}`);
  doc.setIn(["webrtc", "listen"], "");
  return doc.toString();
}

/**
 * Turn a camera's display name into a stream key: lowercase, words joined by `_`, nothing that would
 * need URL-escaping in an RTSP path.
 */
export function streamSlug(name) {
  return String(name ?? "")
    .normalize("NFKD")
    .replace(/[^\p{L}\p{N}]+/gu, "_")
    .replace(/^_+|_+$/g, "")
    .toLowerCase();
}

/**
 * The go2rtc stream key for each camera: its name, slugged. Serial numbers tell an operator nothing about
 * which camera they are looking at, in the web UI or in an RTSP URL.
 *
 * The serial remains the fallback, and deliberately so: a camera whose name slugs to nothing, or whose
 * name another camera already took, must still be reachable rather than disappear or — worse — answer for
 * the wrong camera. Order is stable (the camera list order), so a key does not migrate between cameras
 * across restarts unless the names themselves change.
 *
 * Returns a Map of sn -> key. This is the single source of truth for the key: the bridge reports it on
 * /api/cameras and over /ws so the wall uses the same one rather than deriving its own.
 */
export function streamKeys(cameras) {
  const keys = new Map();
  const taken = new Set();
  for (const cam of cameras) {
    const slug = streamSlug(cam.name);
    const key = slug && !taken.has(slug) ? slug : cam.sn;
    taken.add(key);
    keys.set(cam.sn, key);
  }
  return keys;
}

/** Reserve native aliases against every compatible stream key, including similarly named cameras. */
export function nativeStreamKeys(cameras) {
  const compatible = streamKeys(cameras), taken = new Set(compatible.values()), native = new Map();
  for (const [sn, key] of compatible) {
    const base = `${key}_native`;
    let candidate = base, suffix = 2;
    while (taken.has(candidate)) candidate = `${base}_${suffix++}`;
    taken.add(candidate); native.set(sn, candidate);
  }
  return native;
}

/** Only advertise learned native HEVC; explicit compatible URLs and their policy remain intact. */
export function nativeRenditionFor(ctx, cam, host) {
  const slot = ctx.state.slots?.get(cam.sn);
  if (!cam.enabled || slot?.codec !== "h265" || !(slot.width > 0 && slot.height > 0)) return undefined;
  const freshness = ctx.freshness?.capability({ ...cam, transcode: "never" }, host);
  if (!freshness) return undefined;
  const key = nativeStreamKeys(ctx.listCameras().filter((c) => c.enabled)).get(cam.sn);
  return { rtsp: `rtsp://${urlHost(host)}:${ctx.cfg.rtspPort ?? 8554}/${key}`, codec: "h265", width: slot.width, height: slot.height, ...slot.nativeCodecInfo, freshness };
}

/** Rewrite the generated config's serial-keyed streams to {@link streamKeys}, in place and one for one. */
export function withNamedStreams(text, cameras) {
  const doc = parseDocument(text);
  const streams = doc.getIn(["streams"]);
  if (!streams) return doc.toString();
  const keys = streamKeys(cameras);
  for (const [sn, key] of keys) {
    if (key === sn) continue;
    const source = doc.getIn(["streams", sn]);
    if (source === undefined) continue;
    doc.deleteIn(["streams", sn]);
    doc.setIn(["streams", key], source);
  }
  return doc.toString();
}

export function createGo2rtc(ctx) {
  const { cfg, state } = ctx;
  const platform = ctx.platform ?? process.platform;
  let stopping = false;
  // Discovery for different cameras may complete concurrently. Serialize the
  // whole generation/check/restart transaction, not only the final write.
  let configWork = Promise.resolve();
  function enqueueConfig(work) {
    const result = configWork.then(work);
    configWork = result.catch(() => {});
    return result;
  }
  // Watchdog and fatal paths call process.exit() directly. A child left behind keeps the
  // RTSP/API ports bound, so the next bridge instance can look healthy while serving stale video.
  process.once("exit", () => state.flags.go2rtcProc?.kill("SIGKILL"));

  /** Codec we currently believe a camera speaks (learned from its live feed, else whatever /api reported). */
  const codecOf = (sn) => state.slots.get(sn)?.codec ?? ctx.getCamera?.(sn)?.codec;
  /** Stable timed source or codec-selected FFmpeg suffix for each enabled SDK camera. */
  function egressPlan(cameras = ctx.listCameras().filter((x) => x.enabled)) {
    const plan = {};
    for (const c of [...cameras].sort((a, b) => a.sn.localeCompare(b.sn))) if (!c.external) plan[c.sn] = ctx.freshness?.enabledFor(c)
      ? `http://${urlHost(cfg.selfHost)}:${cfg.port}/timed-stream/${c.sn}`
      : egressFor(codecOf(c.sn), c.transcode ?? cfg.go2rtcTranscode, cfg.go2rtcMaxHeight, platform, cfg.go2rtcVaapiDevice);
    return plan;
  }
  let lastPlan = {}, lastNativePlan = "";
  const nativePlan = (cameras = ctx.listCameras()) => cameras.filter((c) => c.enabled && !c.external && ctx.freshness?.enabledFor({ ...c, transcode: "never" })).map((c) => c.sn).sort().join("|");

  async function generateGo2rtc() {
    const stagingPath = `${cfg.go2rtcConfig}.${randomUUID()}.tmp`;
    try {
    const cameras = ctx.listCameras().filter((c) => c.enabled);
    const devices = cameras.filter((c) => !c.external).map((c) => ({ sn: c.sn, stream: `/stream/${c.sn}` }));
    const sns = await writeGo2rtcConfig({ ...cfg, selfHost: urlHost(cfg.selfHost), go2rtcConfig: stagingPath }, devices);
    // The vendored generator always emits "#video=copy"; rewrite each source to this camera's egress mode.
    const plan = egressPlan(cameras), nativeSnapshot = nativePlan(cameras);
    let text = hardenGo2rtcYaml(await readFile(stagingPath, "utf8"), { rtspPort: cfg.rtspPort, apiPort: cfg.go2rtcApiPort });
    if (platform === "darwin" && cfg.go2rtcMaxHeight > 0 && Object.values(plan).some((x) => x.includes("tvh264"))) {
      const doc = parseDocument(text);
      doc.setIn(["ffmpeg", "tvh264"], "-codec:v h264_videotoolbox -g:v 30 -bf:v 0");
      text = doc.toString();
    }
    if (platform === "linux" && Object.values(plan).some((x) => x.includes("tvh264"))) {
      const doc = parseDocument(text);
      const wrapper = fileURLToPath(new URL("./ffmpeg-hardware.mjs", import.meta.url));
      doc.setIn(["ffmpeg", "bin"], wrapper);
      // Each producer tests its actual input: GPU decode/scale/encode first,
      // CPU decode/scale + GPU encode second, software encode last. An explicit
      // decode override can skip the first stage for operator troubleshooting.
      const device = cfg.go2rtcVaapiDevice || "auto";
      const preference = cfg.go2rtcVaapiDecode === false ? "encode" : "auto";
      const height = cfg.go2rtcMaxHeight || 0;
      // MPEG-TS carries the camera frame clock. Restamping at arrival would turn
      // network bursts back into irregular presentation timestamps.
      doc.setIn(["ffmpeg", "ewb_dynamic_http"], `--eufy-vaapi ${device} ${height} ${preference} -reinit_filter 0 -f mpegts -analyzeduration 100000 -probesize 262144 -i {input}`);
      // The launcher substitutes these private tokens. Main profile is accepted
      // by the Pi V4L2 decoder; passthrough timestamps avoid nominal-FPS backlog.
      doc.setIn(["ffmpeg", "tvh264"], "-vf ewb_scale -codec:v ewb_encoder -profile:v main -g:v 30 -bf:v 0 -fps_mode:v passthrough -enc_time_base:v 1:90000");
      // go2rtc normally SIGKILLs only its immediate child. Allow the launcher to
      // terminate FFmpeg and suppress fallback when a consumer disconnects or
      // the 30-second producer startup timeout expires.
      doc.setIn(["ffmpeg", "output"], "-user_agent ffmpeg/go2rtc -rtsp_transport tcp -f rtsp {output}#killsignal=15#killtimeout=2");
      text = doc.toString();
    }
    {
      const doc = parseDocument(text);
      // Finite PES fragments avoid waiting for the next frame to delimit large pictures.
      // FFmpeg joins those fragments; go2rtc's direct TS reader treats each PES as a picture.
      doc.setIn(["ffmpeg", "ewb_timestamped_http"], "-f mpegts -analyzeduration 100000 -probesize 262144 -i {input}");
      text = doc.toString();
    }
    for (const [sn, suffix] of Object.entries(plan)) {
      const rawUrl = `http://${urlHost(cfg.selfHost)}:${cfg.port}/stream/${sn}`;
      if (ctx.freshness?.enabledFor(ctx.getCamera(sn))) {
        text = text.replace(`ffmpeg:${rawUrl}#video=copy`, `http://${urlHost(cfg.selfHost)}:${cfg.port}/timed-stream/${sn}`);
        continue;
      }
      const url = `${rawUrl}?format=mpegts&pes=fragmented`;
      const input = suffix.includes("#input=") ? "" : "#input=ewb_timestamped_http";
      const source = `ffmpeg:${url}${input}${suffix}`;
      text = text.replace(`ffmpeg:${rawUrl}#video=copy`, source);
    }
    text = withNamedStreams(text, cameras);
    {
      const doc = parseDocument(text);
      const imported = cfg.go2rtcImported ?? {};
      const namedKeys = streamKeys(cameras);
      for (const camera of cameras.filter((c) => c.external)) {
        doc.setIn(["streams", namedKeys.get(camera.sn)], imported.streams[camera.sn]);
      }
      const keys = nativeStreamKeys(cameras);
      for (const sn of nativeSnapshot.split("|").filter(Boolean)) {
        doc.setIn(["streams", keys.get(sn)], `http://${urlHost(cfg.selfHost)}:${cfg.port}/timed-stream/${sn}?rendition=native`);
      }
      text = doc.toString();
    }
    await writeFile(stagingPath, text, { encoding: "utf8", mode: 0o600 });
    await rename(stagingPath, cfg.go2rtcConfig);
    lastNativePlan = nativeSnapshot;
    lastPlan = plan;
    const t = Object.entries(plan).filter(([, v]) => v.includes("h264#")).map(([k]) => k);
    console.log(`[bridge] go2rtc config written (${sns.length} stream(s)${t.length ? `, transcoding ${t.join(", ")}` : ""}) → ${cfg.go2rtcConfig}`);
    return sns;
    } finally { await rm(stagingPath, { force: true }); }
  }

  const writeGo2rtc = () => enqueueConfig(generateGo2rtc);

  /** Reconcile changed producer routes; codec metadata alone preserves stable timed-stream routes. */
  const syncGo2rtc = () => enqueueConfig(async () => {
    if (!state.flags.ready) return;
    const plan = egressPlan();
    if (JSON.stringify(plan) === JSON.stringify(lastPlan) && nativePlan() === lastNativePlan) {
      await ctx.ws?.broadcastSnapshot?.();
      return;
    }
    console.log("[bridge] go2rtc egress changed — rewriting config and restarting go2rtc");
    await generateGo2rtc();
    state.flags.go2rtcProc?.kill(); // exit handler restarts it
    await ctx.ws?.broadcastSnapshot?.();
  });

  const importedStatus = new Map();
  let importedPollRunning = false;

  /** Direct-source liveness is measured from go2rtc bytes, independently of SDK media state. */
  async function refreshExternalStreams() {
    const cameras = ctx.listCameras().filter((c) => c.enabled && c.external);
    if (!cameras.length || importedPollRunning) return;
    importedPollRunning = true;
    try {
      const response = await fetch(`http://127.0.0.1:${cfg.go2rtcApiPort ?? 1984}/api/streams`, { signal: AbortSignal.timeout(1500) });
      if (!response.ok) throw new Error("go2rtc stream status unavailable");
      const streams = await response.json();
      const keys = streamKeys(ctx.listCameras().filter((c) => c.enabled));
      for (const camera of cameras) {
        const previous = importedStatus.get(camera.sn);
        const producers = streams[keys.get(camera.sn)]?.producers ?? [];
        const video = producers.flatMap((p) => p.receivers ?? []).find((r) => r.codec?.codec_type === "video");
        const bytes = producers.flatMap((p) => p.receivers ?? []).filter((r) => r.codec?.codec_type === "video")
          .reduce((total, receiver) => total + (receiver.bytes ?? 0), 0);
        const now = Date.now();
        const lastProgressAt = bytes > 0 && bytes !== previous?.bytes ? now : (previous?.lastProgressAt ?? 0);
        const streaming = Boolean(video) && lastProgressAt > 0 && now - lastProgressAt < 6000;
        importedStatus.set(camera.sn, { bytes, lastProgressAt, streaming, codec: video?.codec?.codec_name ?? camera.codec });
        if (streaming) state.streaming.add(camera.sn);
        else state.streaming.delete(camera.sn);
        if (streaming !== Boolean(previous?.streaming)) ctx.broadcastEvent?.({ type: "streamState", sn: camera.sn, state: streaming ? "live" : "idle" });
      }
    } catch {
      for (const camera of cameras) {
        const previous = importedStatus.get(camera.sn);
        if (previous?.streaming) ctx.broadcastEvent?.({ type: "streamState", sn: camera.sn, state: "idle" });
        importedStatus.set(camera.sn, { ...previous, streaming: false });
        state.streaming.delete(camera.sn);
      }
    } finally {
      importedPollRunning = false;
    }
  }

  /** Public status omits source URLs and transport credentials. */
  function go2rtcStreamStatus(sn) {
    const status = importedStatus.get(sn);
    return { streaming: status?.streaming ?? false, codec: status?.codec, stalls: 0 };
  }

  function startGo2rtc() {
    if (state.flags.go2rtcProc || stopping) return;
    const proc = spawn(cfg.go2rtcBin, ["-config", cfg.go2rtcConfig], { stdio: ["ignore", "inherit", "inherit"] });
    state.flags.go2rtcProc = proc;
    proc.on("error", (e) => {
      console.error(`[bridge] go2rtc failed to start (${e.message}) — RTSP unavailable; HTTP /stream still works`);
      state.flags.go2rtcProc = undefined;
    });
    proc.on("exit", (code, sig) => {
      state.flags.go2rtcProc = undefined;
      if (stopping) return;
      console.error(`[bridge] go2rtc exited (${code ?? sig}) — restarting in 3 s`);
      setTimeout(startGo2rtc, 3000);
    });
    console.log(`[bridge] go2rtc started (${cfg.go2rtcBin}) — RTSP on :${cfg.rtspPort}`);
  }

  function stopGo2rtc() {
    stopping = true;
    state.flags.go2rtcProc?.kill();
  }

  return { writeGo2rtc, syncGo2rtc, startGo2rtc, stopGo2rtc, refreshExternalStreams, go2rtcStreamStatus };
}
