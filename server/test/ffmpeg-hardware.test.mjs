import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { transcodeAttempts, hardwareFailure } from "../src/ffmpeg-hardware.mjs";

const input = "http://127.0.0.1:3000/stream/CAMERA";
const args = ["-hide_banner", "--eufy-vaapi", "/dev/dri/renderD128", "720", "auto",
  "-reinit_filter", "0", "-use_wallclock_as_timestamps", "1", "-i", input,
  "-vf", "ewb_scale", "-codec:v", "ewb_encoder", "-profile:v", "main", "-g:v", "30", "-bf:v", "0",
  "-fps_mode:v", "passthrough", "-enc_time_base:v", "1:90000", "-f", "null", "-"];

test("per-input hardware preference keeps timing and codec compatibility across fallback", () => {
  const attempts = transcodeAttempts(args);
  assert.deepEqual(attempts.map((a) => a.name), ["hardware-decode-encode", "software-decode-hardware-encode", "software-decode-encode"]);
  for (const { args: a } of attempts) {
    assert.equal(a[a.indexOf("-i") + 1], input);
    for (const [k, v] of [["-use_wallclock_as_timestamps", "1"], ["-profile:v", "main"], ["-g:v", "30"], ["-bf:v", "0"], ["-fps_mode:v", "passthrough"], ["-enc_time_base:v", "1:90000"]]) {
      assert.equal(a[a.indexOf(k) + 1], v);
    }
    assert.ok(!a.includes("--eufy-vaapi") && !a.includes("ewb_encoder") && !a.includes("ewb_scale"));
  }
  assert.ok(attempts[0].args.includes("scale_vaapi=w=-2:h=720:format=nv12"));
  assert.ok(!attempts[0].args.includes("hwupload"));
  assert.ok(attempts[1].args.includes("scale=-2:720:eval=frame,format=nv12,hwupload"));
  assert.ok(!attempts[1].args.includes("-hwaccel"));
  assert.ok(attempts[2].args.includes("libx264"));
  assert.ok(!attempts[2].args.includes("-vaapi_device"));
});

test("hardware decoding avoids frame-thread delay without limiting software fallback", () => {
  const [hardware, mixed, software] = transcodeAttempts(args);
  const threads = hardware.args.indexOf("-thread_type:v");
  assert.ok(threads >= 0 && threads < hardware.args.indexOf("-i"));
  assert.equal(hardware.args[threads + 1], "slice");
  for (const attempt of [mixed, software]) assert.ok(!attempt.args.includes("-thread_type:v"));
  for (const attempt of [hardware, mixed, software]) {
    assert.ok(!attempt.args.includes("-threads:v"));
    assert.ok(!attempt.args.includes("-async_depth"));
    assert.ok(!attempt.args.includes("nobuffer"));
  }
});

test("no-resize and explicit decoder override still retain encoder fallback", () => {
  const a = [...args]; a[a.indexOf("--eufy-vaapi") + 2] = "0";
  assert.ok(transcodeAttempts(a)[0].args.includes("scale_vaapi=format=nv12"));
  a[a.indexOf("--eufy-vaapi") + 3] = "encode";
  assert.deepEqual(transcodeAttempts(a).map((p) => p.name), ["software-decode-hardware-encode", "software-decode-encode"]);
  assert.deepEqual(transcodeAttempts(["-version"]), [{ name: "passthrough", args: ["-version"] }]);
});

test("automatic render-node discovery tries every GPU before software and handles no GPU", () => {
  const a = [...args]; a[a.indexOf("--eufy-vaapi") + 1] = "auto";
  const attempts = transcodeAttempts(a, { devices: ["/dev/dri/renderD129", "/dev/dri/renderD130"] });
  assert.deepEqual(attempts.map((p) => [p.name, p.device]), [
    ["hardware-decode-encode", "/dev/dri/renderD129"], ["hardware-decode-encode", "/dev/dri/renderD130"],
    ["software-decode-hardware-encode", "/dev/dri/renderD129"], ["software-decode-hardware-encode", "/dev/dri/renderD130"],
    ["software-decode-encode", undefined],
  ]);
  assert.deepEqual(transcodeAttempts(a, { devices: [] }).map((p) => p.name), ["software-decode-encode"]);
});

test("hardware diagnostics trigger fallback but network and bad-media errors do not", () => {
  for (const text of ["Failed to initialise VAAPI connection: -1", "Device creation failed: -22", "No device available for decoder: device type vaapi", "Failed to create processing pipeline config: 12 (the requested VAProfile is not supported)", "Unknown encoder 'h264_vaapi'"]) assert.ok(hardwareFailure(text), text);
  for (const text of ["Connection refused", "[vost#0:0/h264_vaapi] Error submitting a packet to the muxer: Broken pipe", "Invalid data found when processing input", "[h264] non-existing PPS 0 referenced"]) assert.ok(!hardwareFailure(text), text);
});

async function fixture(t, scenario) {
  const dir = await mkdtemp(join(tmpdir(), "ewb-ffmpeg-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const bin = join(dir, "fake-ffmpeg.mjs");
  const trace = join(dir, "trace.jsonl");
  await writeFile(bin, `#!${process.execPath}\nimport {appendFileSync} from 'node:fs';
const a=process.argv.slice(2); appendFileSync(process.env.TRACE, JSON.stringify({args:a,pid:process.pid})+'\\n');
if (process.env.SCENARIO==='stall') { process.on('SIGTERM',()=>{appendFileSync(process.env.TRACE,JSON.stringify({terminated:true})+'\\n');process.exit(0)});setInterval(()=>{},1000); }
else if(process.env.SCENARIO==='network') {console.error('[vost#0:0/h264_vaapi] Error submitting a packet to the muxer: Broken pipe');process.exit(1)}
else if(a.includes('-hwaccel')) {console.error('No device available for decoder: device type vaapi');process.exit(1)}
else if(a.includes('h264_vaapi')) {console.error('Failed to initialise VAAPI connection: -1');process.exit(1)}
else {console.log('ffmpeg version fake');process.exit(0)}\n`, { mode: 0o755 });
  const proc = spawn(process.execPath, [fileURLToPath(new URL("../src/ffmpeg-hardware.mjs", import.meta.url)), ...args], {
    env: { ...process.env, BRIDGE_FFMPEG_BIN: bin, TRACE: trace, SCENARIO: scenario }, stdio: ["ignore", "pipe", "pipe"],
  });
  let stderr = "";
  proc.stderr.on("data", (b) => { stderr += b; });
  const done = new Promise((resolve) => proc.on("close", (code) => resolve(code)));
  t.after(() => { if (proc.exitCode === null) proc.kill("SIGTERM"); });
  return { proc, done, trace, stderr: () => stderr, rows: async () => (await readFile(trace, "utf8")).trim().split("\n").map(JSON.parse) };
}

test("launcher retries the identical live URL at most three times and reaches software", async (t) => {
  const f = await fixture(t, "fallback");
  assert.equal(await f.done, 0);
  const rows = await f.rows();
  assert.equal(rows.length, 3);
  assert.ok(rows.every(({ args: a }) => a[a.indexOf("-i") + 1] === input));
  assert.match(f.stderr(), /retrying same input with software-decode-hardware-encode/);
  assert.match(f.stderr(), /retrying same input with software-decode-encode/);
});

test("producer output disconnect is returned without a software retry", async (t) => {
  const f = await fixture(t, "network");
  assert.equal(await f.done, 1);
  assert.equal((await f.rows()).length, 1);
});

test("startup timeout or consumer cancellation terminates FFmpeg and prevents fallback", { timeout: 5000 }, async (t) => {
  const f = await fixture(t, "stall");
  for (let i = 0; i < 100; i++) {
    try { if ((await f.rows()).length) break; } catch {}
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  f.proc.kill("SIGTERM");
  assert.equal(await f.done, 143);
  const rows = await f.rows();
  assert.equal(rows.filter((r) => r.args).length, 1);
  assert.ok(rows.some((r) => r.terminated));
  assert.throws(() => process.kill(rows[0].pid, 0), /ESRCH/);
});
