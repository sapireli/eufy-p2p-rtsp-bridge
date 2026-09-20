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
export function egressFor(codec, mode) {
  if (mode === "never") return "#video=copy";
  if (mode === "always") return "#video=h264#hardware";
  return codec === "h265" ? "#video=h264#hardware" : "#video=copy"; // "auto"
}

/**
 * Harden the generated go2rtc.yaml: upstream (an HA add-on behind its own auth) opens the go2rtc API
 * and WebRTC on every interface, unauthenticated. The wall's clients only pull RTSP on :8554, so the
 * API is pinned to loopback and WebRTC is disabled (go2rtc enables it by default even without a
 * `webrtc:` block, so the block must exist with an empty listen). Done as a post-pass so the vendored generator
 * stays verbatim. Comments in the generated file are preserved (yaml Document round-trip).
 */
export function hardenGo2rtcYaml(text) {
  const doc = parseDocument(text);
  doc.setIn(["api", "listen"], "127.0.0.1:1984");
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
 * Add a named stream per camera alongside the serial-keyed one, so go2rtc's web UI lists "front_door"
 * rather than only "T8214510242321E6".
 *
 * The alias is a PROXY of the serial stream (go2rtc pulling its own RTSP), not a second source. That
 * matters: a second `ffmpeg:` source would open a second producer and therefore a second P2P session to
 * the camera. Pulled this way the alias is lazy — it costs nothing until somebody views it — and when
 * they do it attaches to the one upstream producer the serial stream already has.
 *
 * The serial keys stay exactly as they were, so existing RTSP URLs, the wall's config and the docs keep
 * working; the name is an addition, not a rename. A name that slugs to nothing, collides with a serial,
 * or collides with another camera's name is skipped rather than silently pointed at the wrong camera.
 */
export function withNamedAliases(text, cameras, rtspPort = 8554) {
  const doc = parseDocument(text);
  const serials = new Set(cameras.map((c) => c.sn));
  const taken = new Set(serials);
  for (const cam of cameras) {
    const slug = streamSlug(cam.name);
    if (!slug || taken.has(slug)) continue;
    taken.add(slug);
    doc.setIn(["streams", slug], `rtsp://127.0.0.1:${rtspPort}/${cam.sn}`);
  }
  return doc.toString();
}

export function createGo2rtc(ctx) {
  const { cfg, state } = ctx;
  let stopping = false;

  /** Codec we currently believe a camera speaks (learned from its live feed, else whatever /api reported). */
  const codecOf = (sn) => state.slots.get(sn)?.codec ?? ctx.getCamera?.(sn)?.codec;
  /** The egress suffix each enabled camera should get right now, keyed by sn. */
  function egressPlan() {
    const plan = {};
    for (const c of ctx.listCameras().filter((x) => x.enabled)) plan[c.sn] = egressFor(codecOf(c.sn), cfg.go2rtcTranscode);
    return plan;
  }
  let lastPlan = {};

  async function writeGo2rtc() {
    const devices = ctx.listCameras().filter((c) => c.enabled).map((c) => ({ sn: c.sn, stream: `/stream/${c.sn}` }));
    const sns = await writeGo2rtcConfig(cfg, devices);
    // The vendored generator always emits "#video=copy"; rewrite each source to this camera's egress mode.
    const plan = egressPlan();
    let text = hardenGo2rtcYaml(await readFile(cfg.go2rtcConfig, "utf8"));
    for (const [sn, suffix] of Object.entries(plan)) text = text.replace(`/stream/${sn}#video=copy`, `/stream/${sn}${suffix}`);
    text = withNamedAliases(text, ctx.listCameras().filter((c) => c.enabled));
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
    console.log(`[bridge] go2rtc started (${cfg.go2rtcBin}) — RTSP on :8554`);
  }

  function stopGo2rtc() {
    stopping = true;
    state.flags.go2rtcProc?.kill();
  }

  return { writeGo2rtc, syncGo2rtc, startGo2rtc, stopGo2rtc };
}
