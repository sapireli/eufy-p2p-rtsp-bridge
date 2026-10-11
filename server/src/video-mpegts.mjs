// Timestamped Annex-B units in whole or finite-fragment PES. No audio, frame-rate synthesis,
// or waiting: the caller owns discontinuity/reconnect policy and consumer lifetime.
const PACKET_SIZE = 188;
const VIDEO_PID = 0x100;
const PMT_PID = 0x1000;
const MODULO_PTS = 2 ** 33;

export class TimestampDiscontinuityError extends Error {
  constructor(stepMs) {
    super(`source video timestamp discontinuity (${stepMs} ms)`);
    this.name = "TimestampDiscontinuityError";
    this.stepMs = stepMs;
  }
}

function crc32Mpeg(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc = (crc ^ (byte << 24)) >>> 0;
    for (let i = 0; i < 8; i++) crc = ((crc << 1) ^ ((crc & 0x80000000) ? 0x04c11db7 : 0)) >>> 0;
  }
  return crc;
}

function section(body) {
  const out = Buffer.alloc(body.length + 4);
  Buffer.from(body).copy(out);
  out.writeUInt32BE(crc32Mpeg(out.subarray(0, body.length)), body.length);
  return out;
}

function writePts(out, at, time) {
  const t = BigInt(time % MODULO_PTS);
  out[at] = 0x21 | Number((t >> 29n) & 0x0en);
  out[at + 1] = Number((t >> 22n) & 0xffn);
  out[at + 2] = Number((t >> 14n) & 0xfen) | 1;
  out[at + 3] = Number((t >> 7n) & 0xffn);
  out[at + 4] = Number((t << 1n) & 0xfen) | 1;
}

function writePcr(out, at, time) {
  const t = BigInt(time % MODULO_PTS);
  out[at] = Number((t >> 25n) & 0xffn);
  out[at + 1] = Number((t >> 17n) & 0xffn);
  out[at + 2] = Number((t >> 9n) & 0xffn);
  out[at + 3] = Number((t >> 1n) & 0xffn);
  out[at + 4] = Number((t & 1n) << 7n) | 0x7e;
  out[at + 5] = 0; // PCR extension: exact 90 kHz clock, no fractional tick.
}

export function newVideoMpegTsMuxer({ codec, maxTimestampStepMs, maxPesPayloadBytes = 0 } = {}) {
  const streamType = codec === "h264" ? 0x1b : codec === "h265" ? 0x24 : undefined;
  if (!streamType) throw new TypeError("MPEG-TS video codec must be h264 or h265");
  if (maxTimestampStepMs !== undefined && (!Number.isFinite(maxTimestampStepMs) || maxTimestampStepMs <= 0)) {
    throw new TypeError("maxTimestampStepMs must be positive");
  }
  if (!Number.isInteger(maxPesPayloadBytes) || maxPesPayloadBytes < 0 || maxPesPayloadBytes > 65527) {
    throw new TypeError("maxPesPayloadBytes must be 0 (whole unit) or 1..65527");
  }
  const continuity = new Map();
  let previousSource;
  let pts90k = 0;
  let first = true;
  let discontinuity = false;

  function header(packet, pid, start, adaptation) {
    const cc = continuity.get(pid) ?? 0;
    packet[0] = 0x47;
    packet[1] = (start ? 0x40 : 0) | (pid >> 8);
    packet[2] = pid & 0xff;
    packet[3] = (adaptation ? 0x30 : 0x10) | cc;
    continuity.set(pid, (cc + 1) & 15);
  }

  function table(pid, bytes) {
    const packet = Buffer.alloc(PACKET_SIZE, 0xff);
    header(packet, pid, true, false);
    packet[4] = 0; // PSI pointer: section begins immediately.
    bytes.copy(packet, 5);
    return packet;
  }

  function reset() {
    previousSource = undefined;
    pts90k = 0;
    first = true;
    discontinuity = true;
  }

  function push(frame, options = {}) {
    if (!Buffer.isBuffer(frame?.data) || frame.data.length === 0) throw new TypeError("video access unit must contain bytes");
    if (!Number.isInteger(frame.sourceTimestamp) || frame.sourceTimestamp < 0 || frame.sourceTimestamp > 0xffffffff) {
      throw new TypeError("sourceTimestamp must be a uint32 source timestamp");
    }
    if (frame.codec !== undefined && frame.codec !== codec) throw new TypeError("video codec changed; create a new MPEG-TS consumer");
    // Validate before updating any clock/counter state. A rejected unit is not emitted.
    const step = previousSource === undefined || options.discontinuity ? 0 :
      ((frame.sourceTimestamp - previousSource + 2 ** 31) % 2 ** 32 + 2 ** 32) % 2 ** 32 - 2 ** 31;
    if (step < 0 || (maxTimestampStepMs !== undefined && step > maxTimestampStepMs)) throw new TimestampDiscontinuityError(step);
    if (options.discontinuity) reset();
    // This host interprets the validated P2P source clock as milliseconds.
    // The SDK preserves the raw word without promising a universal time unit.
    pts90k += step * 90;
    previousSource = frame.sourceTimestamp;
    const out = [];
    if (first || frame.keyframe) {
      out.push(table(0, section([0x00, 0xb0, 0x0d, 0, 1, 0xc1, 0, 0, 0, 1, 0xf0, 0])));
      out.push(table(PMT_PID, section([0x02, 0xb0, 0x12, 0, 1, 0xc1, 0, 0,
        0xe1, 0x00, 0xf0, 0, streamType, 0xe1, 0x00, 0xf0, 0])));
    }
    const limit = maxPesPayloadBytes || frame.data.length;
    for (let offset = 0; offset < frame.data.length; offset += limit) {
      const body = frame.data.subarray(offset, offset + limit);
      const firstFragment = offset === 0;
      const pes = Buffer.alloc(14 + body.length);
      pes.set([0, 0, 1, 0xe0], 0);
      // FFmpeg can reconstruct an elementary access unit across finite PES
      // fragments with the same original PTS. Its codec parser then sees the
      // next unit without a zero-length PES waiting for yet another unit.
      // Direct go2rtc MPEG-TS treats each PES as a unit: keep whole-unit mode
      // for that consumer, even when oversized video requires length=0.
      pes.writeUInt16BE(body.length + 8 <= 0xffff ? body.length + 8 : 0, 4);
      pes.set([firstFragment ? 0x84 : 0x80, 0x80, 5], 6);
      writePts(pes, 9, pts90k);
      body.copy(pes, 14);
      let at = 0;
      while (at < pes.length) {
        const start = at === 0;
        const size = Math.min(pes.length - at, start ? 176 : 184);
        const adaptationLength = 183 - size;
        const adaptation = start || size < 184;
        const packet = Buffer.alloc(PACKET_SIZE, 0xff);
        header(packet, VIDEO_PID, start, adaptation);
        let payloadAt = 4;
        if (adaptation) {
          packet[4] = adaptationLength;
          if (adaptationLength > 0) packet[5] = start ? 0x10 | (firstFragment && frame.keyframe ? 0x40 : 0) | (firstFragment && discontinuity ? 0x80 : 0) : 0;
          if (start) writePcr(packet, 6, pts90k);
          payloadAt += 1 + adaptationLength;
        }
        pes.copy(packet, payloadAt, at, at + size);
        out.push(packet);
        at += size;
      }
    }
    first = false;
    discontinuity = false;
    return Buffer.concat(out);
  }
  return { push, reset };
}
