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
    let key = slug && !taken.has(slug) ? slug : cam.sn;
    // A newly discovered camera must not change an existing URL, even when its serial equals an
    // earlier name's slug. Give the newcomer a unique suffix rather than renaming the live stream.
    for (let suffix = 2; taken.has(key); suffix++) key = `${cam.sn}_${suffix}`;
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
  const entries = [];
  for (const [sn, key] of keys) {
    const source = doc.getIn(["streams", sn]);
    if (source === undefined) continue;
    entries.push([sn, key, source]);
  }
  for (const [sn] of entries) doc.deleteIn(["streams", sn]);
  for (const [, key, source] of entries) {
    doc.setIn(["streams", key], source);
  }
  return doc.toString();
}

export function createGo2rtc(ctx, { spawnImpl = spawn, retryDelayMs = 3000, setTimer = setTimeout, clearTimer = clearTimeout, fetchImpl = fetch } = {}) {
  const { cfg, state } = ctx;
  let stopping = false;
  let retryTimer;
  let syncTimer;
  let mutations = Promise.resolve();
  function serialize(task) {
    const result = mutations.then(task);
    mutations = result.catch(() => {});
    return result;
  }

  /** Codec we currently believe a camera speaks (learned from its live feed, else whatever /api reported). */
  const codecOf = (sn) => state.slots.get(sn)?.codec ?? ctx.getCamera?.(sn)?.codec;
  /** The egress suffix each enabled camera should get right now, keyed by sn. */
  function egressPlan(cameras = ctx.listCameras()) {
    const plan = {};
    for (const c of cameras.filter((x) => x.enabled)) plan[c.sn] = egressFor(codecOf(c.sn) ?? c.codec, cfg.go2rtcTranscode);
    return plan;
  }
  let lastPlan = {};

  async function writeGo2rtc(cameras = ctx.listCameras()) {
    const enabled = cameras.filter((c) => c.enabled);
    const devices = enabled.map((c) => ({ sn: c.sn, stream: `/stream/${c.sn}` }));
    const sns = await writeGo2rtcConfig(cfg, devices);
    // The vendored generator always emits "#video=copy"; rewrite each source to this camera's egress mode.
    const plan = egressPlan(cameras);
    let text = hardenGo2rtcYaml(await readFile(cfg.go2rtcConfig, "utf8"));
    for (const [sn, suffix] of Object.entries(plan)) {
      const url = `http://${cfg.selfHost}:${cfg.port}/stream/${sn}`;
      const source = suffix === "#video=copy" ? url : `ffmpeg:${url}${suffix}`;
      text = text.replace(`ffmpeg:${url}#video=copy`, source);
    }
    text = withNamedStreams(text, enabled);
    await writeFile(cfg.go2rtcConfig, text, "utf8");
    lastPlan = plan;
    const t = Object.entries(plan).filter(([, v]) => v.includes("h264#")).map(([k]) => k);
    console.log(`[bridge] go2rtc config written (${sns.length} stream(s)${t.length ? `, transcoding ${t.join(", ")}` : ""}) → ${cfg.go2rtcConfig}`);
    return sns;
  }

  function sourceFor(sn, suffix) {
    const url = `http://${cfg.selfHost}:${cfg.port}/stream/${sn}`;
    return suffix === "#video=copy" ? url : `ffmpeg:${url}${suffix}`;
  }

  async function putStream(cam, key, suffix, signal) {
    const url = new URL("http://127.0.0.1:1984/api/streams");
    url.searchParams.set("name", key);
    url.searchParams.set("src", sourceFor(cam.sn, suffix));
    const timeout = AbortSignal.timeout(3000);
    const response = await fetchImpl(url, { method: "PUT", signal: signal ? AbortSignal.any([signal, timeout]) : timeout });
    if (!response.ok) throw new Error(`go2rtc stream ${key}: HTTP ${response.status}`);
  }

  /** Add one recovered camera without restarting go2rtc or changing healthy stream URLs. */
  function addGo2rtcCamera(cam, key, signal) {
    return serialize(async () => {
      if (stopping || signal?.aborted) throw new Error("camera discovery stopped");
      if (!cam.enabled) return;
      const suffix = egressFor(codecOf(cam.sn) ?? cam.codec, cfg.go2rtcTranscode);
      if (state.flags.go2rtcProc) {
        await putStream(cam, key, suffix, signal);
        lastPlan = { ...lastPlan, [cam.sn]: suffix };
      } else {
        await writeGo2rtc([...ctx.listCameras(), cam]); // next child start loads the missing stream
      }
    });
  }

  /**
   * A camera's codec is only known once its feed delivers a keyframe, which is after go2rtc was first
   * configured. Update that stream through go2rtc's local API when its egress mode changes, so other
   * cameras keep their active RTSP sessions. The API also persists the changed source in go2rtc.yaml.
   */
  function syncGo2rtc() {
    return serialize(async () => {
      if (!state.flags.ready || stopping) return;
      const cameras = ctx.listCameras().filter((c) => c.enabled);
      const plan = egressPlan(cameras);
      const changed = cameras.filter((cam) => plan[cam.sn] !== lastPlan[cam.sn]);
      if (!changed.length) return;
      try {
        if (state.flags.go2rtcProc) {
          const keys = streamKeys(cameras);
          for (const cam of changed) {
            await putStream(cam, keys.get(cam.sn), plan[cam.sn]);
            lastPlan = { ...lastPlan, [cam.sn]: plan[cam.sn] };
          }
        } else await writeGo2rtc();
        if (syncTimer) { clearTimer(syncTimer); syncTimer = undefined; }
      } catch (error) {
        if (!stopping && !syncTimer) {
          syncTimer = setTimer(() => { syncTimer = undefined; void syncGo2rtc().catch((e) => console.error(`[bridge] go2rtc egress retry failed: ${e?.message ?? e}`)); }, retryDelayMs);
          syncTimer?.unref?.();
        }
        throw error;
      }
    });
  }

  function startGo2rtc() {
    if (state.flags.go2rtcProc || stopping) return;
    if (retryTimer) { clearTimer(retryTimer); retryTimer = undefined; }
    let proc;
    try { proc = spawnImpl(cfg.go2rtcBin, ["-config", cfg.go2rtcConfig], { stdio: ["ignore", "inherit", "inherit"] }); }
    catch (error) { console.error(`[bridge] go2rtc failed to start (${error.message}) — retrying in ${retryDelayMs} ms`); scheduleRetry(); return; }
    state.flags.go2rtcProc = proc;
    let ended = false;
    const onEnd = (reason) => {
      if (ended) return; // spawn failures emit error then close; schedule only one retry.
      ended = true;
      if (state.flags.go2rtcProc === proc) state.flags.go2rtcProc = undefined;
      if (stopping) return;
      console.error(`[bridge] go2rtc ${reason} — RTSP unavailable; retrying in ${retryDelayMs} ms`);
      scheduleRetry();
    };
    proc.on("error", (error) => onEnd(`failed to start (${error.message})`));
    proc.once("exit", (code, sig) => onEnd(`exited (${code ?? sig})`));
    proc.once("close", (code, sig) => onEnd(`closed (${code ?? sig})`));
    proc.once("spawn", () => console.log(`[bridge] go2rtc started (${cfg.go2rtcBin}) — RTSP on :8554`));
  }

  function scheduleRetry() {
    if (stopping || retryTimer) return;
    retryTimer = setTimer(() => { retryTimer = undefined; startGo2rtc(); }, retryDelayMs);
    retryTimer?.unref?.();
  }

  function stopGo2rtc() {
    stopping = true;
    if (retryTimer) { clearTimer(retryTimer); retryTimer = undefined; }
    if (syncTimer) { clearTimer(syncTimer); syncTimer = undefined; }
    state.flags.go2rtcProc?.kill();
  }

  return { writeGo2rtc, addGo2rtcCamera, syncGo2rtc, startGo2rtc, stopGo2rtc };
}
