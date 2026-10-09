#!/usr/bin/env node
// go2rtc FFmpeg launcher. Hardware support depends on the actual codec, profile,
// frame size and driver, so try the live input instead of guessing from a GPU name.
import { spawn } from "node:child_process";
import { readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

export function renderNodes() {
  try { return readdirSync("/dev/dri").filter((name) => /^renderD\d+$/.test(name)).sort((a, b) => Number(a.slice(7)) - Number(b.slice(7))).map((name) => `/dev/dri/${name}`); }
  catch { return []; }
}

export function transcodeAttempts(argv, { devices = renderNodes() } = {}) {
  const at = argv.indexOf("--eufy-vaapi");
  if (at < 0) return [{ name: "passthrough", args: argv }];
  const [device, heightText, preference] = argv.slice(at + 1, at + 4);
  const height = Number(heightText);
  if (!device || !Number.isInteger(height) || height < 0 || !["auto", "encode"].includes(preference)) {
    throw new Error("Invalid --eufy-vaapi device/height/preference");
  }
  const base = [...argv.slice(0, at), ...argv.slice(at + 4)];
  const input = base.indexOf("-i");
  const filter = base.indexOf("ewb_scale");
  const encoder = base.indexOf("ewb_encoder");
  if (input < 0 || filter < 0 || encoder < 0) throw new Error("Incomplete bridge FFmpeg template");
  const build = (name, beforeInput, vf, codec, extra = []) => {
    const args = base.map((arg) => arg === "ewb_scale" ? vf : arg === "ewb_encoder" ? codec : arg);
    args.splice(input, 0, ...beforeInput);
    // Encoder-specific options belong after the input, before the output URL.
    args.splice(args.indexOf("-codec:v") + 2, 0, ...extra);
    return { name, args };
  };
  const resize = height ? `scale=-2:${height}:eval=frame,` : "";
  const nodes = device === "auto" ? devices : [device];
  // Frame threading holds future frames even with VAAPI decoding. One decoder
  // thread lets each hardware-decoded frame reach the encoder immediately.
  const hardware = nodes.map((node) => ({ ...build("hardware-decode-encode", ["-init_hw_device", `vaapi=ewb_vaapi:${node}`,
    "-filter_hw_device", "ewb_vaapi", "-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi", "-hwaccel_device", "ewb_vaapi", "-threads:v", "1"],
    `scale_vaapi=${height ? `w=-2:h=${height}:` : ""}format=nv12`, "h264_vaapi"), device: node }));
  const mixed = nodes.map((node) => ({ ...build("software-decode-hardware-encode", ["-vaapi_device", node],
    `${resize}format=nv12,hwupload`, "h264_vaapi"), device: node }));
  const software = build("software-decode-encode", [], `${resize}format=yuv420p`, "libx264",
    ["-preset:v", "superfast", "-tune:v", "zerolatency"]);
  return preference === "encode" ? [...mixed, software] : [...hardware, ...mixed, software];
}

// Limit downgrades to hardware/filter failures. Authentication, source disconnects,
// malformed video and output/network errors require the normal stream reconnect.
export function hardwareFailure(stderr) {
  const diagnostic = stderr.split("\n").map((line) => line.replace(/^\[[^\]]+\]\s*/, "")).join("\n");
  return /(?:Failed to (?:initiali[sz]e|create|open|configure|setup|sync|transfer|map|upload|download).*(?:VAAPI|VA-API|hardware|hwaccel)|(?:VAAPI|VA-API|hardware accelerator|hwaccel).*(?:not supported|unsupported|failed|failure|error)|No (?:device|support).*decoder|Device creation failed|A hardware device reference is required|Unknown encoder ['"]h264_vaapi|(?:scale_vaapi|hwupload).*?(?:failed|not supported|not found)|(?:VAProfile|VAEntrypoint|vaCreate\w+|vaBeginPicture|vaEndPicture).*(?:not supported|failed|error)|(?:Hardware|Driver) does not support|Error while opening encoder|No usable encoding (?:entrypoint|profile) found)/i.test(diagnostic);
}

export async function run(argv, { bin = process.env.BRIDGE_FFMPEG_BIN || "ffmpeg" } = {}) {
  const attempts = transcodeAttempts(argv);
  let child;
  let stopped = false;
  let killTimer;
  const stop = (signal) => {
    if (stopped) return;
    stopped = true;
    child?.kill(signal);
    killTimer = setTimeout(() => child?.kill("SIGKILL"), 1000);
    killTimer.unref();
  };
  const onTerm = () => stop("SIGTERM");
  const onInt = () => stop("SIGINT");
  // go2rtc cancels the launcher with SIGTERM (configured in its output URL).
  // Never start a fallback during shutdown or after its parent has disappeared.
  process.on("SIGTERM", onTerm);
  process.on("SIGINT", onInt);
  const parent = process.ppid;
  const parentWatch = setInterval(() => { if (process.ppid !== parent && !stopped) stop("SIGTERM"); }, 1000);
  parentWatch.unref();
  try {
    for (const attempt of attempts) {
      if (stopped) return 143;
      let stderr = "";
      if (attempt.name !== "passthrough") process.stderr.write(`[bridge-ffmpeg] trying ${attempt.name}${attempt.device ? ` on ${attempt.device}` : ""}\n`);
      const result = await new Promise((resolve) => {
        child = spawn(bin, attempt.args, { stdio: ["inherit", "inherit", "pipe"] });
        child.stderr.on("data", (chunk) => {
          process.stderr.write(chunk);
          stderr = (stderr + chunk.toString()).slice(-32768);
        });
        child.on("error", (err) => {
          process.stderr.write(`[bridge-ffmpeg] ${err.message}\n`);
          resolve({ code: 127 });
        });
        child.on("close", (code, signal) => resolve({ code: code ?? (signal ? 143 : 1) }));
      });
      child = undefined;
      clearTimeout(killTimer);
      if (stopped) return 143;
      if (result.code === 0 || !hardwareFailure(stderr) || attempt === attempts.at(-1)) return result.code;
      process.stderr.write(`[bridge-ffmpeg] ${attempt.name} unavailable; retrying same input with ${attempts[attempts.indexOf(attempt) + 1].name}\n`);
    }
    return 1;
  } finally {
    process.off("SIGTERM", onTerm);
    process.off("SIGINT", onInt);
    clearTimeout(killTimer);
    clearInterval(parentWatch);
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try { process.exitCode = await run(process.argv.slice(2)); }
  catch (err) { console.error(`[bridge-ffmpeg] ${err.message}`); process.exitCode = 1; }
}
