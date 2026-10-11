import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import { newVideoMpegTsMuxer, TimestampDiscontinuityError } from "../src/video-mpegts.mjs";

function packets(buf) {
  assert.equal(buf.length % 188, 0);
  const out = [];
  for (let i = 0; i < buf.length; i += 188) {
    const b = buf.subarray(i, i + 188);
    assert.equal(b[0], 0x47);
    const adaptation = !!(b[3] & 0x20);
    out.push({ b, pid: ((b[1] & 0x1f) << 8) | b[2], cc: b[3] & 15,
      payload: b.subarray(4 + (adaptation ? b[4] + 1 : 0)), start: !!(b[1] & 0x40) });
  }
  return out;
}
function pes(buf) { return Buffer.concat(packets(buf).filter(p => p.pid === 0x100).map(p => p.payload)); }
function pesUnits(buf) {
  const out = [], current = [];
  for (const packet of packets(buf).filter(p => p.pid === 0x100)) {
    if (packet.start && current.length) { out.push(Buffer.concat(current)); current.length = 0; }
    current.push(packet.payload);
  }
  if (current.length) out.push(Buffer.concat(current));
  return out;
}
function readPts(b) {
  return Number((BigInt((b[9] >> 1) & 7) << 30n) | (BigInt(b[10]) << 22n) |
    (BigInt(b[11] >> 1) << 15n) | (BigInt(b[12]) << 7n) | BigInt(b[13] >> 1));
}
function readPcr(packet) {
  const b = packet.b;
  assert.ok(b[5] & 0x10);
  return Number((BigInt(b[6]) << 25n) | (BigInt(b[7]) << 17n) | (BigInt(b[8]) << 9n) |
    (BigInt(b[9]) << 1n) | BigInt(b[10] >> 7));
}
function crcValid(bytes) {
  // Independent bit-by-bit division of the complete section including its CRC.
  let remainder = 0xffffffff;
  for (const byte of bytes) for (let bit = 7; bit >= 0; bit--) {
    const feedback = ((remainder >>> 31) ^ ((byte >>> bit) & 1)) !== 0;
    remainder = ((remainder << 1) ^ (feedback ? 0x04c11db7 : 0)) >>> 0;
  }
  return remainder === 0;
}
const unit = (ts, data = Buffer.from([0, 0, 0, 1, 0x65, 0xaa])) => ({ sourceTimestamp: ts, data, keyframe: true });

test("MPEG-TS preserves source timing, complete payload, PCR and valid program tables", () => {
  for (const codec of ["h264", "h265"]) {
    const mux = newVideoMpegTsMuxer({ codec });
    const first = mux.push(unit(900000));
    for (const pid of [0, 0x1000]) {
      const p = packets(first).find(p => p.pid === pid);
      const section = p.payload.subarray(1);
      const size = 3 + (((section[1] & 15) << 8) | section[2]);
      assert.ok(crcValid(section.subarray(0, size)), "PSI CRC verifies independently");
    }
    const pmt = packets(first).find(p => p.pid === 0x1000).payload.subarray(1);
    assert.equal(pmt[12], codec === "h264" ? 0x1b : 0x24);
    assert.equal(readPts(pes(first)), 0);
    const body = Buffer.alloc(100000, 0xaa);
    body.set([0, 0, 0, 1, 0x41]);
    const next = mux.push({ ...unit(900067, body), keyframe: false });
    assert.equal(readPts(pes(next)), 6030);
    assert.deepEqual(pes(next).subarray(14), body);
    assert.equal(pes(next).readUInt16BE(4), 0, "oversized video PES uses unbounded length");
    assert.equal(readPcr(packets(next).find(p => p.pid === 0x100)), 6030);
    const counters = new Map();
    for (const p of packets(Buffer.concat([first, next]))) {
      if (counters.has(p.pid)) assert.equal(p.cc, (counters.get(p.pid) + 1) & 15);
      counters.set(p.pid, p.cc);
    }
  }
});

test("normal uint32 source wrap works; rejected discontinuities do not change the clock", () => {
  const mux = newVideoMpegTsMuxer({ codec: "h264", maxTimestampStepMs: 100 });
  mux.push(unit(0xfffffff0));
  assert.equal(readPts(pes(mux.push(unit(0x33)))), 6030);
  assert.throws(() => mux.push(unit(0x32)), TimestampDiscontinuityError);
  assert.throws(() => mux.push(unit(0x1000)), TimestampDiscontinuityError);
  assert.equal(readPts(pes(mux.push(unit(0x76)))), 12060);
  assert.throws(() => mux.push(unit(undefined)), TypeError);
  assert.throws(() => mux.push({ ...unit(0xb9), codec: "h265" }), /codec changed/);
  mux.reset();
  const fresh = mux.push(unit(100));
  assert.equal(readPts(pes(fresh)), 0);
  assert.ok(packets(fresh).find(p => p.pid === 0x100).b[5] & 0x80);
  assert.equal(packets(fresh).filter(p => p.pid !== 0x100).length, 2);
});

test("finite PES packets carry a real length and each new consumer starts with tables", () => {
  const a = newVideoMpegTsMuxer({ codec: "h264" });
  const b = newVideoMpegTsMuxer({ codec: "h264" });
  const frame = unit(100);
  const first = a.push(frame);
  assert.equal(pes(first).readUInt16BE(4), frame.data.length + 8);
  assert.deepEqual(first, b.push(frame));
  assert.deepEqual(packets(a.push({ ...unit(167), keyframe: false })).map(p => p.pid), [0x100]);
  assert.equal(packets(a.push(unit(234))).filter(p => p.pid !== 0x100).length, 2);
});

test("FFmpeg-facing PES fragments are finite, byte-complete and share the original AU time", () => {
  const mux = newVideoMpegTsMuxer({ codec: "h264", maxPesPayloadBytes: 63000 });
  mux.push(unit(100));
  const body = Buffer.alloc(200000, 0xaa);
  const units = pesUnits(mux.push(unit(167, body)));
  assert.equal(units.length, 4);
  assert.deepEqual(Buffer.concat(units.map(p => p.subarray(14))), body);
  for (const [i, p] of units.entries()) {
    assert.equal(p.readUInt16BE(4), p.length - 6);
    assert.ok(p.readUInt16BE(4) > 0 && p.length - 14 <= 63000);
    assert.equal(readPts(p), 6030);
    assert.equal(!!(p[6] & 4), i === 0);
  }
  assert.throws(() => newVideoMpegTsMuxer({ codec: "h264", maxPesPayloadBytes: 65528 }), TypeError);
});

const hasFfmpeg = spawnSync("ffmpeg", ["-version"], { encoding: "utf8" }).status === 0 &&
  spawnSync("ffprobe", ["-version"], { encoding: "utf8" }).status === 0;
test("FFmpeg decodes unchanged pictures while ffprobe observes explicit irregular source PTS", { skip: !hasFfmpeg }, () => {
  const dir = mkdtempSync(join(tmpdir(), "eufy-ts-test-"));
  function run(bin, args) {
    const r = spawnSync(bin, args, { encoding: "utf8", maxBuffer: 10 * 1024 * 1024 });
    assert.equal(r.status, 0, `${bin}: ${r.stderr}`);
    return r.stdout;
  }
  try {
    const raw = join(dir, "source.h264");
    run("ffmpeg", ["-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x96:rate=15", "-frames:v", "30",
      "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-bf", "0", "-g", "15",
      "-x264-params", "aud=1:repeat-headers=1", "-f", "h264", raw]);
    const data = readFileSync(raw);
    const starts = [];
    for (let i = 0; i + 5 < data.length; i++) {
      const length = data[i] === 0 && data[i + 1] === 0 && data[i + 2] === 0 && data[i + 3] === 1 ? 4 :
        data[i] === 0 && data[i + 1] === 0 && data[i + 2] === 1 ? 3 : 0;
      if (length && (data[i + length] & 31) === 9) { starts.push(i); i += length; }
    }
    assert.equal(starts.length, 30);
    function hashes(input) {
      return run("ffmpeg", ["-v", "error", "-i", input, "-map", "0:v:0", "-fps_mode", "passthrough", "-f", "framemd5", "-"])
        .split("\n").filter(l => l && !l.startsWith("#")).map(l => l.split(",").at(-1).trim());
    }
    for (const maxPesPayloadBytes of [0, 63000]) {
      const mux = newVideoMpegTsMuxer({ codec: "h264", maxPesPayloadBytes });
      const chunks = [], expected = [];
      for (let i = 0; i < starts.length; i++) {
        const timestamp = Math.round(i * 1000 / 15) + (i >= 10 ? 217 : 0);
        expected.push(timestamp * 90);
        let accessUnit = data.subarray(starts[i], starts[i + 1] ?? data.length);
        // Valid filler data forces oversized PES without changing coded pixels.
        if (i === 15) accessUnit = Buffer.concat([accessUnit, Buffer.from([0, 0, 0, 1, 12]), Buffer.alloc(100000, 0xff), Buffer.from([0x80])]);
        const bytes = mux.push({ sourceTimestamp: 100000 + timestamp, data: accessUnit, keyframe: i % 15 === 0 });
        if (maxPesPayloadBytes) assert.ok(pesUnits(bytes).every(p => p.readUInt16BE(4) > 0));
        chunks.push(bytes);
      }
      const ts = join(dir, `timed-${maxPesPayloadBytes}.ts`);
      writeFileSync(ts, Buffer.concat(chunks));
      const probe = JSON.parse(run("ffprobe", ["-v", "error", "-select_streams", "v:0", "-show_packets", "-show_entries", "packet=pts", "-of", "json", ts]));
      assert.deepEqual(probe.packets.map(p => Number(p.pts)), expected);
      assert.deepEqual(hashes(ts), hashes(raw), "timestamp container leaves all decoded pictures unchanged");
    }
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

const hasX265 = hasFfmpeg && spawnSync("ffmpeg", ["-encoders"], { encoding: "utf8" }).stdout.includes("libx265");
test("FFmpeg recognizes and decodes timestamped HEVC access units", { skip: !hasX265 }, () => {
  const dir = mkdtempSync(join(tmpdir(), "eufy-hevc-ts-test-"));
  function run(bin, args) {
    const r = spawnSync(bin, args, { encoding: "utf8", maxBuffer: 10 * 1024 * 1024 });
    assert.equal(r.status, 0, `${bin}: ${r.stderr}`);
    return r.stdout;
  }
  try {
    const raw = join(dir, "source.h265");
    run("ffmpeg", ["-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x96:rate=15", "-frames:v", "6",
      "-c:v", "libx265", "-preset", "ultrafast", "-tune", "zerolatency", "-bf", "0", "-g", "15",
      "-x265-params", "aud=1:repeat-headers=1:pools=1:frame-threads=1:wpp=0:log-level=error", "-f", "hevc", raw]);
    const data = readFileSync(raw);
    const starts = [];
    for (let i = 0; i + 5 < data.length; i++) {
      const length = data[i] === 0 && data[i + 1] === 0 && data[i + 2] === 0 && data[i + 3] === 1 ? 4 :
        data[i] === 0 && data[i + 1] === 0 && data[i + 2] === 1 ? 3 : 0;
      if (length && ((data[i + length] >> 1) & 63) === 35) { starts.push(i); i += length; }
    }
    assert.equal(starts.length, 6);
    const expected = [0, 67, 134, 234, 301, 368];
    function hashes(input) {
      return run("ffmpeg", ["-v", "error", "-i", input, "-map", "0:v:0", "-fps_mode", "passthrough", "-f", "framemd5", "-"])
        .split("\n").filter(l => l && !l.startsWith("#")).map(l => l.split(",").at(-1).trim());
    }
    for (const maxPesPayloadBytes of [0, 63000]) {
      const mux = newVideoMpegTsMuxer({ codec: "h265", maxPesPayloadBytes });
      const chunks = starts.map((start, i) => {
        let accessUnit = data.subarray(start, starts[i + 1] ?? data.length);
        // HEVC filler_data_rbsp (NAL type 38), valid temporal_id_plus1=1.
        if (i === 0) accessUnit = Buffer.concat([accessUnit, Buffer.from([0, 0, 0, 1, 38 << 1, 1]), Buffer.alloc(100000, 0xff), Buffer.from([0x80])]);
        const bytes = mux.push({ sourceTimestamp: 100000 + expected[i], data: accessUnit, keyframe: i === 0 });
        if (maxPesPayloadBytes) assert.ok(pesUnits(bytes).every(p => p.readUInt16BE(4) > 0));
        return bytes;
      });
      const ts = join(dir, `timed-${maxPesPayloadBytes}.ts`);
      writeFileSync(ts, Buffer.concat(chunks));
      const probe = JSON.parse(run("ffprobe", ["-v", "error", "-select_streams", "v:0", "-show_packets", "-show_entries", "packet=pts", "-of", "json", ts]));
      assert.deepEqual(probe.packets.map(p => Number(p.pts)), expected.map(t => t * 90));
      assert.deepEqual(hashes(ts), hashes(raw));
    }
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
