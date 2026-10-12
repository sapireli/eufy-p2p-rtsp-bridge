import test from "node:test";
import assert from "node:assert/strict";
import { EncodedFrameReader } from "../src/encoded-frame-reader.mjs";
import { stampVideoFrame, parseFrameStamp } from "../src/frame-stamp.mjs";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

test("joins split data/stats in either arrival order, emits complete first packet immediately", () => {
  const frames = [];
  const reader = new EncodedFrameReader({ onFrame: (f) => frames.push(f) });
  reader.pushStats("0 8589934590 1/90000 ");
  reader.pushData(Buffer.from([1, 2]));
  reader.pushStats("3\n1 8589934592 1/90000 2\n");
  assert.equal(frames.length, 0);
  reader.pushData(Buffer.from([3, 4]));
  assert.equal(frames.length, 1, "finite packet does not wait for the next packet");
  assert.deepEqual(frames[0], { data: Buffer.from([1, 2, 3]), pts90k: 8589934590n });
  reader.pushData(Buffer.from([5]));
  assert.deepEqual(frames[1], { data: Buffer.from([4, 5]), pts90k: 8589934592n });
  reader.end();
  assert.throws(() => reader.pushData(Buffer.from([6])), /ended/);
});
test("large frame remains one exact packet and millisecond timebase rescales exactly", () => {
  const frames = [], payload = Buffer.alloc(250000, 0xab);
  const reader = new EncodedFrameReader({ onFrame: (f) => frames.push(f) });
  for (let i = 0; i < payload.length; i += 7919) reader.pushData(payload.subarray(i, i + 7919));
  reader.pushStats(`0 4294967295 1/1000 ${payload.length}`);
  assert.equal(frames.length, 0);
  reader.end();
  assert.deepEqual(frames, [{ data: payload, pts90k: 4294967295n * 90n }]);
});
test("rejects ambiguous PTS, malformed/discontinuous stats and resource excess", () => {
  for (const line of ["1 0 1/90000 1\n", "0 NOPTS 1/90000 1\n", "0 1 1/7 1\n", "0 0 0/90000 1\n", "0 0 1/0 1\n", "0 0 1/90000 0\n", "0 0 1/90000 5\n"]) {
    const reader = new EncodedFrameReader({ onFrame() {}, maxFrameBytes: 4 });
    assert.throws(() => reader.pushStats(line));
  }
  const reader = new EncodedFrameReader({ onFrame() {}, maxBufferedBytes: 3, maxPendingFrames: 1 });
  assert.throws(() => reader.pushData(Buffer.alloc(4)), /buffer limit/);
  reader.pushStats("0 0 1/90000 1\n");
  assert.throws(() => reader.pushStats("1 1 1/90000 1\n"), /pending/);
  assert.throws(() => new EncodedFrameReader({ onFrame() {} }).pushStats(Buffer.from([255])), /ASCII/);
  assert.throws(() => new EncodedFrameReader({ onFrame() {} }).pushStats("1".repeat(257)), /line limit/);
});
test("EOF detects truncated packets, surplus data and final malformed stats", () => {
  for (const [stats, data] of [["0 0 1/90000 2\n", [1]], ["0 0 1/90000 1\n", [1, 2]], ["0 NOPTS", []]]) {
    const reader = new EncodedFrameReader({ onFrame() {} });
    reader.pushStats(stats);
    reader.pushData(Buffer.from(data));
    assert.throws(() => reader.end());
  }
});

test("real FFmpeg packet stats preserve source cadence and post-encode stamps preserve decoded pictures", (t) => {
  if (spawnSync("ffmpeg", ["-version"]).status !== 0) return t.skip("ffmpeg unavailable");
  const dir = mkdtempSync(join(tmpdir(), "ewb-frame-stamp-"));
  try {
    const statsPath = join(dir, "stats.txt");
    const encoded = spawnSync("ffmpeg", ["-hide_banner", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x96:rate=15", "-frames:v", "3", "-c:v", "libx264", "-bf", "0", "-g", "3", "-enc_time_base:v", "1:90000", "-fps_mode:v", "passthrough", "-map", "0:v:0", "-f", "data", "-stats_mux_pre", statsPath, "-stats_mux_pre_fmt", "{n} {pts} {tb} {size}", "pipe:1"], { maxBuffer: 1024 * 1024 });
    assert.equal(encoded.status, 0, encoded.stderr.toString());
    const frames = [];
    const reader = new EncodedFrameReader({ onFrame: (frame) => frames.push(frame) });
    for (let at = 0; at < encoded.stdout.length; at += 137) reader.pushData(encoded.stdout.subarray(at, at + 137));
    const stats = readFileSync(statsPath);
    for (let at = 0; at < stats.length; at += 7) reader.pushStats(stats.subarray(at, at + 7));
    reader.end();
    assert.deepEqual(frames.map((frame) => frame.pts90k), [0n, 6000n, 12000n]);
    assert.deepEqual(Buffer.concat(frames.map((frame) => frame.data)), encoded.stdout);
    const stamped = frames.map((frame, index) => {
      const record = { serverEpoch: Buffer.alloc(8, 0x12), sourceTimestamp: 1000 + index * 67, ingressMonoUs: 123000n + BigInt(index) * 67000n, frameId: BigInt(index) };
      const data = stampVideoFrame(frame.data, "h264", record);
      assert.equal(parseFrameStamp(data, "h264").frameId, BigInt(index));
      return data;
    });
    function decodedMd5(input) {
      const result = spawnSync("ffmpeg", ["-hide_banner", "-v", "error", "-threads", "1", "-f", "h264", "-i", "pipe:0", "-f", "framemd5", "pipe:1"], { input, maxBuffer: 1024 * 1024 });
      assert.equal(result.status, 0, result.stderr.toString());
      return result.stdout.toString().split("\n").filter((line) => line && !line.startsWith("#"));
    }
    assert.deepEqual(decodedMd5(Buffer.concat(stamped)), decodedMd5(encoded.stdout));
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
