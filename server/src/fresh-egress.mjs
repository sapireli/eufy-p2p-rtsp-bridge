import { spawn } from "node:child_process";
import { transcodeAttempts, hardwareFailure } from "./ffmpeg-hardware.mjs";
import { newVideoFlvMuxer } from "./video-flv.mjs";
import { newVideoMatroskaMuxer } from "./video-matroska.mjs";
import { stampVideoFrame, firstVclNal } from "./frame-stamp.mjs";
import { EncodedFrameReader } from "./encoded-frame-reader.mjs";

/** One input, one final encoded packet, one original receipt stamp. No inferred RTP phase. */
export function freshTranscodeArgs(cfg) {
  return ["-hide_banner", "-v", "error", "-copyts", "--eufy-vaapi", cfg.go2rtcVaapiDevice || "auto", String(cfg.go2rtcMaxHeight || 0), cfg.go2rtcVaapiDecode === false ? "encode" : "auto",
    "-reinit_filter", "0", "-f", "matroska", "-analyzeduration", "100000", "-probesize", "262144", "-i", "pipe:0", "-map", "0:v:0", "-an",
    "-vf", "ewb_scale", "-codec:v", "ewb_encoder", "-profile:v", "main", "-g:v", "30", "-bf:v", "0", "-fps_mode:v", "passthrough", "-enc_time_base:v", "1:90000",
    "-bsf:v", "dump_extra=freq=keyframe", "-stats_mux_pre", "pipe:3", "-stats_mux_pre_fmt", "{n} {pts} {tb} {size}", "-flush_packets", "1", "-f", "data", "pipe:1"];
}

export function createFreshEgress(ctx) {
  const active = new Set();
  function attachFreshConsumer(sn, res) {
    const cam = ctx.getCamera(sn);
    let stopped = false, child, detach, killTimer, sourceCodec, inputMux, outputMux, reader;
    let attemptIndex = 0, attempts, stderr = "", needKeyframe = true;
    const pending = new Map();
    const statistics = { scope: "bridge-ingress", captureClockValidated: false, inputFrames: 0, outputFrames: 0, freshOutputFrames: 0, staleFrames: 0, expiredReferenceFrames: 0, unmappedFrames: 0, maximumEgressAgeMs: 0 };
    const slot = ctx.state.slots.get(sn);
    if (slot) slot.freshness = statistics;
    const source = {
      destroyed: false, _ewbDropping: true,
      // A pipe's high-water mark is a flow-control hint, not transport loss.
      // Large camera IDRs routinely exceed it. Dropping their following input
      // frames here would break decoder references and starve startup probing.
      // This consumer instead bounds the encoded-input queue explicitly below.
      writableNeedDrain: false,
      end: () => stop(),
    };
    function stop(error) {
      if (stopped) return;
      stopped = true; source.destroyed = true;
      detach?.(); active.delete(stop);
      child?.kill("SIGTERM");
      const old = child;
      killTimer = setTimeout(() => old?.kill("SIGKILL"), 1000); killTimer.unref();
      if (error) console.warn(`[bridge-freshness] ${sn}: ${error.message ?? error}`);
      res.destroy();
    }
    function writeFrame(data, stamp, codec) {
      if (stopped) return;
      const ageMs = Number(ctx.freshness.nowUs() - stamp.ingressMonoUs) / 1000;
      const keyframe = firstVclNal(data, codec)?.keyframe === true;
      if (ageMs < 0) return stop(new Error("receipt clock is ahead of bridge clock"));
      const expired = ageMs >= stamp.maxAgeMs;
      if (res.writableNeedDrain) return stop(new Error("timed media consumer stopped draining"));
      if (needKeyframe && !keyframe) return;
      needKeyframe = false;
      outputMux ??= newVideoFlvMuxer({ codec });
      const bytes = outputMux.push({ data: stampVideoFrame(data, codec, stamp), sourceTimestamp: stamp.sourceTimestamp, codec, keyframe });
      statistics.outputFrames++;
      // Retain compressed reference pictures, with their original EXPIRED stamp.
      // Strict clients decode dependencies but suppress expired decoded output.
      // Dropping an encoded P frame would manufacture a hole until the next IDR.
      if (expired) { statistics.staleFrames++; statistics.expiredReferenceFrames++; }
      else statistics.freshOutputFrames++;
      statistics.maximumEgressAgeMs = Math.max(statistics.maximumEgressAgeMs, ageMs);
      res.write(bytes);
    }
    function startAttempt(codec) {
      sourceCodec = codec;
      inputMux = newVideoMatroskaMuxer({ codec });
      outputMux = undefined; pending.clear(); needKeyframe = true; stderr = "";
      attempts ??= transcodeAttempts(freshTranscodeArgs(ctx.cfg));
      const attempt = attempts[attemptIndex];
      console.log(`[bridge-freshness] ${sn}: trying ${attempt.name}${attempt.device ? ` on ${attempt.device}` : ""}`);
      const process = (ctx.spawn ?? spawn)(ctx.cfg.ffmpegBin || "ffmpeg", attempt.args, { stdio: ["pipe", "pipe", "pipe", "pipe"] });
      child = process;
      reader = new EncodedFrameReader({ onFrame: ({ data, pts90k }) => {
        const stamp = pending.get(pts90k.toString());
        if (!stamp) { statistics.unmappedFrames++; return stop(new Error(`encoded PTS ${pts90k} has no source frame`)); }
        pending.delete(pts90k.toString());
        for (const key of pending.keys()) if (BigInt(key) < pts90k) pending.delete(key);
        writeFrame(data, stamp, "h264");
      }});
      const read = (fn) => (chunk) => { if (child !== process || stopped) return; try { fn(chunk); } catch (e) { stop(e); } };
      process.stdout.on("data", read((chunk) => reader.pushData(chunk)));
      process.stdio[3].on("data", read((chunk) => reader.pushStats(chunk)));
      process.stderr.on("data", (chunk) => { stderr = (stderr + chunk.toString()).slice(-32768); });
      process.stdin.on("error", () => {});
      process.on("error", stop);
      process.on("close", (code) => {
        if (child !== process || stopped) return;
        child = undefined;
        if (code !== 0 && hardwareFailure(stderr) && attemptIndex + 1 < attempts.length) {
          console.warn(`[bridge-freshness] ${sn}: ${attempt.name} unavailable; trying ${attempts[attemptIndex + 1].name}`);
          attemptIndex++; source._ewbDropping = true; inputMux = undefined; pending.clear();
          return;
        }
        try { reader.end(); } catch (e) { return stop(e); }
        stop(new Error(`encoder ended (${code}): ${stderr.slice(-1200)}`));
      });
    }
    function onFrame(frame, stamp) {
      if (stopped) return;
      if (!stamp || !Buffer.isBuffer(frame?.data)) return stop(new Error("source receipt metadata unavailable"));
      const currentSlot = ctx.state.slots.get(sn);
      if (currentSlot) currentSlot.freshness = statistics;
      statistics.inputFrames++;
      const codec = frame.codec ?? ctx.state.slots.get(sn)?.codec;
      const mode = cam.transcode ?? ctx.cfg.go2rtcTranscode;
      const transcode = mode === "always" || (mode === "auto" && codec === "h265");
      if (!transcode) return writeFrame(frame.data, stamp, codec);
      if (!child) startAttempt(codec);
      if (sourceCodec !== codec) return stop(new Error("source codec changed"));
      let bytes;
      try { bytes = inputMux.push(frame); } catch (error) { return stop(error); }
      const key = BigInt(inputMux.pts90k).toString();
      if (pending.has(key)) return stop(new Error("duplicate source PTS cannot establish frame identity"));
      pending.set(key, stamp);
      if (pending.size > 120) return stop(new Error("encoder frame backlog exceeded bound"));
      if (child.stdin.writableLength + bytes.length > 4 * 1024 * 1024) {
        return stop(new Error("encoder input byte backlog exceeded bound"));
      }
      child.stdin.write(bytes);
    }
    active.add(stop);
    res.on("close", () => stop());
    detach = ctx.attachConsumer(sn, source, { onFrame });
    return stop;
  }
  return { attachFreshConsumer, stopFreshEgress: () => { for (const stop of active) stop(); } };
}
