// go2rtc lifecycle: write its yaml from the enabled camera list (vendored generator) and run the binary
// as a child, restarting it with a short delay if it dies. Not fatal when the binary is missing (dev).
import { spawn } from "node:child_process";
import { readFile, writeFile } from "node:fs/promises";
import { parseDocument } from "yaml";
import { writeGo2rtcConfig } from "./vendor/ha-bridge/go2rtc-config.mjs";

/**
 * go2rtc source suffix for a camera: transcode H.265 to H.264 when asked, ALWAYS hardware-accelerated
 * (`#hardware` — go2rtc selects videotoolbox / vaapi / v4l2m2m for the host). Never emit a CPU transcode:
 * libx264 cannot hold real time for these sources (measured ~1.0-1.2x, degrading), ffmpeg falls behind and
 * go2rtc kills the producer on its exec timeout, which takes the RTSP stream down mid-view. Where hardware
 * accel is unavailable, pass the bitstream through with copy instead.
 */
export function egressFor(codec, mode, maxHeight = 0, platform = process.platform, vaapiDevice = "") {
  if (mode === "never") return "#video=copy";
  const transcode = mode === "always" || codec === "h265";
  if (!transcode) return "#video=copy";
  // go2rtc's VideoToolbox preset yields hardware pixel buffers that its scale filter cannot read.
  // Decode to ordinary frames and use VideoToolbox only for the encoder when resizing on macOS.
  if (platform === "linux" && vaapiDevice && maxHeight > 0) return "#input=ewb_dynamic_http#video=tvh264";
  const video = platform === "darwin" && maxHeight > 0 ? "tvh264" : "h264#hardware";
  return `#video=${video}${maxHeight > 0 ? `#height=${maxHeight}` : ""}`; // "auto"
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
  // Watchdog and fatal paths call process.exit() directly. A child left behind keeps the
  // RTSP/API ports bound, so the next bridge instance can look healthy while serving stale video.
  process.once("exit", () => state.flags.go2rtcProc?.kill("SIGKILL"));

  /** Codec we currently believe a camera speaks (learned from its live feed, else whatever /api reported). */
  const codecOf = (sn) => state.slots.get(sn)?.codec ?? ctx.getCamera?.(sn)?.codec;
  /** The egress suffix each enabled camera should get right now, keyed by sn. */
  function egressPlan() {
    const plan = {};
    for (const c of ctx.listCameras().filter((x) => x.enabled)) plan[c.sn] = egressFor(codecOf(c.sn), c.transcode ?? cfg.go2rtcTranscode, cfg.go2rtcMaxHeight, platform, cfg.go2rtcVaapiDevice);
    return plan;
  }
  let lastPlan = {};

  async function writeGo2rtc() {
    const devices = ctx.listCameras().filter((c) => c.enabled).map((c) => ({ sn: c.sn, stream: `/stream/${c.sn}` }));
    const sns = await writeGo2rtcConfig(cfg, devices);
    // The vendored generator always emits "#video=copy"; rewrite each source to this camera's egress mode.
    const plan = egressPlan();
    let text = hardenGo2rtcYaml(await readFile(cfg.go2rtcConfig, "utf8"), { rtspPort: cfg.rtspPort, apiPort: cfg.go2rtcApiPort });
    if (platform === "darwin" && cfg.go2rtcMaxHeight > 0 && Object.values(plan).some((x) => x.includes("tvh264"))) {
      const doc = parseDocument(text);
      doc.setIn(["ffmpeg", "tvh264"], "-codec:v h264_videotoolbox -g:v 30 -bf:v 0");
      text = doc.toString();
    }
    if (platform === "linux" && cfg.go2rtcVaapiDevice && cfg.go2rtcMaxHeight > 0 && Object.values(plan).some((x) => x.includes("tvh264"))) {
      const doc = parseDocument(text);
      // The source is decoded to ordinary frames first; Ivy Bridge VA-API cannot decode HEVC.
      // Upload the scaled NV12 frames only for the hardware H.264 encoder.
      // A camera can change resolution mid-stream. Keep the filter graph and H.264 encoder output
      // stable when the aspect ratio stays the same; otherwise FFmpeg reinitialization fails at hwupload.
      doc.setIn(["ffmpeg", "ewb_dynamic_http"], "-reinit_filter 0 -i {input}");
      // Main is accepted by the Pi's V4L2 decoder. VA-API's automatic profile emits
      // constrained-high, which GStreamer can reject against the driver's advertised profiles.
      doc.setIn(["ffmpeg", "tvh264"], `-vaapi_device ${cfg.go2rtcVaapiDevice} -vf scale=-2:${cfg.go2rtcMaxHeight}:eval=frame,format=nv12,hwupload -codec:v h264_vaapi -profile:v main -g:v 30 -bf:v 0`);
      text = doc.toString();
    }
    for (const [sn, suffix] of Object.entries(plan)) {
      const url = `http://${cfg.selfHost}:${cfg.port}/stream/${sn}`;
      const source = suffix === "#video=copy" ? url : `ffmpeg:${url}${suffix}`;
      text = text.replace(`ffmpeg:${url}#video=copy`, source);
    }
    text = withNamedStreams(text, ctx.listCameras().filter((c) => c.enabled));
    await writeFile(cfg.go2rtcConfig, text, "utf8");
    lastPlan = plan;
    const t = Object.entries(plan).filter(([, v]) => v.includes("h264#")).map(([k]) => k);
    console.log(`[bridge] go2rtc config written (${sns.length} stream(s)${t.length ? `, transcoding ${t.join(", ")}` : ""}) → ${cfg.go2rtcConfig}`);
    return sns;
  }

  /**
   * A camera's codec is only known once its feed delivers a keyframe, which is after go2rtc was first
   * configured. When that changes the egress mode (H.265 discovered → transcode), rewrite the config and
   * restart go2rtc so the stream is actually served that way.
   */
  async function syncGo2rtc() {
    if (!state.flags.ready) return;
    const plan = egressPlan();
    if (JSON.stringify(plan) === JSON.stringify(lastPlan)) return;
    console.log("[bridge] go2rtc egress changed — rewriting config and restarting go2rtc");
    await writeGo2rtc();
    state.flags.go2rtcProc?.kill(); // exit handler restarts it
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

  return { writeGo2rtc, syncGo2rtc, startGo2rtc, stopGo2rtc };
}
