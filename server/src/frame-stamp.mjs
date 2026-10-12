// Fixed user_data_unregistered SEI, inserted AFTER the encoder. The timing scope
// is bridge ingress; this stamp does not assert a calibrated sensor capture time.
const UUID = Buffer.from("455746524553484e4553533030303031", "hex");
const U64_MAX = (1n << 64n) - 1n;
function u64(value, name) {
  if (typeof value !== "bigint" || value < 0n || value > U64_MAX) throw new TypeError(`${name} must be a uint64 bigint`);
  return value;
}
function escapeRbsp(bytes) {
  const out = [];
  let zeros = 0;
  for (const byte of bytes) {
    if (zeros >= 2 && byte <= 3) { out.push(3); zeros = 0; }
    out.push(byte);
    zeros = byte === 0 ? zeros + 1 : 0;
  }
  return Buffer.from(out);
}
function unescapeRbsp(bytes) {
  const out = [];
  let zeros = 0;
  for (let i = 0; i < bytes.length; i++) {
    const byte = bytes[i];
    if (zeros >= 2 && byte === 3) {
      if (i + 1 >= bytes.length || bytes[i + 1] > 3) return null;
      zeros = 0;
      continue;
    }
    out.push(byte);
    zeros = byte === 0 ? zeros + 1 : 0;
  }
  return Buffer.from(out);
}
function nalStarts(bytes) {
  const starts = [];
  for (let i = 0; i < bytes.length - 2; i++) {
    if (bytes[i] === 0 && bytes[i + 1] === 0 && bytes[i + 2] === 1) {
      starts.push({ from: i > 0 && bytes[i - 1] === 0 ? i - 1 : i, body: i + 3 });
      i += 2;
    }
  }
  return starts;
}
const ANNEXB_START3 = Buffer.from([0, 0, 1]);

/** Read prefix NAL headers only; return before inspecting the first coded slice.
 * Native start-code searches skip prefix payloads without a JavaScript byte walk.
 * keyframe identifies an H.264 IDR or H.265 random-access VCL, not parameter sets.
 */
export function firstVclNal(data, codec) {
  if (!Buffer.isBuffer(data) || (codec !== "h264" && codec !== "h265")) return null;
  let searchFrom = 0;
  for (;;) {
    const start = data.indexOf(ANNEXB_START3, searchFrom);
    if (start < 0) return null;
    const body = start + 3;
    if (body >= data.length || (codec === "h265" && body + 1 >= data.length)) return null;
    const type = codec === "h264" ? data[body] & 31 : (data[body] >> 1) & 63;
    if (codec === "h264" ? type >= 1 && type <= 5 : type <= 31) {
      return { from: start > 0 && data[start - 1] === 0 ? start - 1 : start, body, type,
        keyframe: codec === "h264" ? type === 5 : type >= 16 && type <= 21 };
    }
    searchFrom = body + (codec === "h264" ? 1 : 2);
  }
}

function nalUnits(bytes) {
  const starts = nalStarts(bytes);
  if (!starts.length) return [bytes]; // A depacketized NAL without an Annex-B prefix.
  return starts.map((start, i) => bytes.subarray(start.body, starts[i + 1]?.from ?? bytes.length));
}

/** record: {serverEpoch: Buffer(8), sourceTimestamp: uint32, ingressMonoUs:
 * bigint, frameId: bigint, maxAgeMs?: uint16}. Returns stamped Annex-B bytes. */
export function stampVideoFrame(data, codec, record) {
  if (!Buffer.isBuffer(data) || !data.length) throw new TypeError("video data must be a nonempty Buffer");
  if (codec !== "h264" && codec !== "h265") throw new TypeError("stamp codec must be h264 or h265");
  const age = record?.maxAgeMs ?? 500;
  if (!Number.isInteger(age) || age < 1 || age > 65535) throw new TypeError("maxAgeMs must be 1..65535");
  if (!Buffer.isBuffer(record?.serverEpoch) || record.serverEpoch.length !== 8) throw new TypeError("serverEpoch must be an 8-byte Buffer");
  if (!Number.isInteger(record.sourceTimestamp) || record.sourceTimestamp < 0 || record.sourceTimestamp > 0xffffffff) throw new TypeError("sourceTimestamp must be uint32");
  const payload = Buffer.alloc(48);
  UUID.copy(payload);
  payload[16] = 1;
  payload[17] = 1; // bridge-ingress scope
  payload.writeUInt16BE(age, 18);
  record.serverEpoch.copy(payload, 20);
  payload.writeUInt32BE(record.sourceTimestamp, 28);
  payload.writeBigUInt64BE(u64(record.ingressMonoUs, "ingressMonoUs"), 32);
  payload.writeBigUInt64BE(u64(record.frameId, "frameId"), 40);
  const nalHeader = codec === "h264" ? Buffer.from([6]) : Buffer.from([39 << 1, 1]);
  const sei = escapeRbsp(Buffer.concat([Buffer.from([5, 48]), payload, Buffer.from([0x80])]));
  // The decoder/client may retain only the most recent prefix SEI. Place ours
  // after encoder SEIs and immediately before VCL, preserving all other bytes.
  const vcl = firstVclNal(data, codec);
  if (!vcl) throw new TypeError("encoded access unit has no Annex-B VCL NAL");
  return Buffer.concat([data.subarray(0, vcl.from), Buffer.from([0, 0, 0, 1]), nalHeader, sei, data.subarray(vcl.from)]);
}

/** Accepts an Annex-B access unit or a raw SEI NAL. Unknown/malformed stamps
 * return null. Integer clocks/IDs remain bigint and the epoch remains Buffer. */
export function parseFrameStamp(bytes, codec) {
  if (!Buffer.isBuffer(bytes) || (codec !== "h264" && codec !== "h265")) return null;
  for (const nal of nalUnits(bytes)) {
    const headerSize = codec === "h264" ? 1 : 2;
    if (nal.length <= headerSize) continue;
    if (codec === "h264" ? (nal[0] & 31) !== 6 : ((nal[0] >> 1) & 63) !== 39) continue;
    const rbsp = unescapeRbsp(nal.subarray(headerSize));
    if (!rbsp) continue;
    let at = 0;
    while (at < rbsp.length) {
      if (rbsp[at] === 0x80 && rbsp.subarray(at + 1).every((byte) => byte === 0)) break;
      let type = 0, size = 0;
      while (at < rbsp.length && rbsp[at] === 255) { type += 255; at++; }
      if (at >= rbsp.length) break;
      type += rbsp[at++];
      while (at < rbsp.length && rbsp[at] === 255) { size += 255; at++; }
      if (at >= rbsp.length) break;
      size += rbsp[at++];
      if (size > rbsp.length - at) break;
      const payload = rbsp.subarray(at, at + size);
      at += size;
      if (type !== 5 || size !== 48 || !payload.subarray(0, 16).equals(UUID)) continue;
      if (payload[16] !== 1 || payload[17] !== 1 || !payload.readUInt16BE(18)) continue;
      return { version: 1, scope: 1, maxAgeMs: payload.readUInt16BE(18), serverEpoch: Buffer.from(payload.subarray(20, 28)), sourceTimestamp: payload.readUInt32BE(28), ingressMonoUs: payload.readBigUInt64BE(32), frameId: payload.readBigUInt64BE(40) };
    }
  }
  return null;
}
