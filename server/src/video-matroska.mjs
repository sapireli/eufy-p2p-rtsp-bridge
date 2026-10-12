// Complete encoded AUs in finite Matroska SimpleBlocks. Unlike MPEG-TS's full
// Annex-B parser, the decoder receives an explicit packet boundary immediately.
import { TimestampDiscontinuityError } from "./video-mpegts.mjs";
const AVC_EXTENDED_PROFILES = new Set([100,110,122,144,244,44,83,86,118,128,138,139,134,135]);

function size(value) {
  const n = BigInt(value);
  for (let width = 1; width <= 8; width++) if (n < (1n << BigInt(width * 7)) - 1n) {
    const bytes = Buffer.alloc(width);
    let word = n | (1n << BigInt(width * 7));
    for (let i = width - 1; i >= 0; i--) { bytes[i] = Number(word & 255n); word >>= 8n; }
    return bytes;
  }
  throw new RangeError("Matroska element too large");
}
function element(id, bytes) { return Buffer.concat([Buffer.from(id, "hex"), size(bytes.length), bytes]); }
function uint(value) {
  const n = BigInt(value);
  if (n < 0n || n > (1n << 64n) - 1n) throw new RangeError("Matroska uint64 outside range");
  let width = 1;
  while (n >= (1n << BigInt(width * 8))) width++;
  const bytes = Buffer.alloc(width);
  let word = n;
  for (let i = width - 1; i >= 0; i--) { bytes[i] = Number(word & 255n); word >>= 8n; }
  return bytes;
}
const number = (id, value) => element(id, uint(value));
const text = (id, value) => element(id, Buffer.from(value));
const u16 = (value) => { const b = Buffer.alloc(2); b.writeUInt16BE(value); return b; };

export function annexBNals(data) {
  const starts = [];
  for (let at = 0; at < data.length - 2; at++) if (data[at] === 0 && data[at + 1] === 0 && data[at + 2] === 1) {
    starts.push({ from: at > 0 && data[at - 1] === 0 ? at - 1 : at, body: at + 3 }); at += 2;
    if (starts.length > 1024) throw new RangeError("Too many video NAL units");
  }
  if (!starts.length || data.subarray(0, starts[0].from).some((byte) => byte !== 0)) throw new TypeError("Video packet must be Annex-B");
  return starts.map((start, i) => data.subarray(start.body, starts[i + 1]?.from ?? data.length)).filter((nal) => nal.length);
}
function rbsp(bytes) {
  const out = []; let zeros = 0;
  for (let i = 0; i < bytes.length; i++) {
    const byte = bytes[i];
    if (zeros >= 2 && byte === 3) {
      if (i + 1 >= bytes.length || bytes[i + 1] > 3) throw new TypeError("Invalid SPS emulation prevention");
      zeros = 0; continue;
    }
    out.push(byte); zeros = byte === 0 ? zeros + 1 : 0;
  }
  return Buffer.from(out);
}
class Bits {
  constructor(data) { this.data = data; this.at = 0; }
  read(count) {
    if (count > 32 || this.at + count > this.data.length * 8) throw new TypeError("Truncated HEVC SPS");
    let value = 0;
    while (count--) { value = value * 2 + ((this.data[this.at >> 3] >> (7 - (this.at & 7))) & 1); this.at++; }
    return value;
  }
  skip(count) { if (this.at + count > this.data.length * 8) throw new TypeError("Truncated HEVC profile"); this.at += count; }
  ue() {
    let zeros = 0;
    while (!this.read(1)) if (++zeros > 30) throw new TypeError("Invalid HEVC Golomb value");
    return 2 ** zeros - 1 + this.read(zeros);
  }
}
function configuration(nals, codec) {
  const type = (nal) => codec === "h264" ? nal[0] & 31 : (nal[0] >> 1) & 63;
  const sets = codec === "h264" ? [7, 8] : [32, 33, 34];
  const grouped = sets.map((t) => nals.filter((nal) => type(nal) === t));
  if (grouped.some((group) => !group.length || group.length > 31 || group.some((nal) => nal.length > 65535))) throw new TypeError("Complete bounded parameter sets required");
  if (codec === "h264") {
    const [sps, pps] = grouped;
    if (sps[0].length < 4) throw new TypeError("Truncated AVC SPS");
    let extension = Buffer.alloc(0);
    if (AVC_EXTENDED_PROFILES.has(sps[0][1])) {
      const bits = new Bits(rbsp(sps[0].subarray(1))); bits.skip(24); bits.ue();
      const chroma = bits.ue(); if (chroma > 3) throw new TypeError("Invalid AVC chroma format");
      if (chroma === 3) bits.skip(1);
      const luma = bits.ue(), chromaDepth = bits.ue();
      if (luma > 7 || chromaDepth > 7) throw new TypeError("Unsupported AVC bit depth");
      extension = Buffer.from([0xfc|chroma,0xf8|luma,0xf8|chromaDepth,0]);
    }
    return Buffer.concat([Buffer.from([1, sps[0][1], sps[0][2], sps[0][3], 255, 224 | sps.length]), ...sps.flatMap((nal) => [u16(nal.length), nal]), Buffer.from([pps.length]), ...pps.flatMap((nal) => [u16(nal.length), nal]), extension]);
  }
  const sps = rbsp(grouped[1][0].subarray(2));
  if (sps.length < 13) throw new TypeError("Truncated HEVC SPS profile");
  const bits = new Bits(sps);
  bits.skip(4);
  const layersMinus1 = bits.read(3), nesting = bits.read(1);
  if (layersMinus1 > 6) throw new TypeError("Unsupported HEVC temporal layer count");
  bits.skip(96); // general_profile_tier_level: profile, compatibility, constraints, level
  const sub = [];
  for (let i = 0; i < layersMinus1; i++) sub.push([bits.read(1), bits.read(1)]);
  if (layersMinus1) bits.skip((8 - layersMinus1) * 2);
  for (const [profile, level] of sub) { if (profile) bits.skip(88); if (level) bits.skip(8); }
  bits.ue(); // sps_seq_parameter_set_id
  const chroma = bits.ue();
  if (chroma > 3) throw new TypeError("Invalid HEVC chroma format");
  if (chroma === 3) bits.skip(1);
  bits.ue(); bits.ue(); // coded dimensions
  if (bits.read(1)) for (let i = 0; i < 4; i++) bits.ue();
  const lumaDepth = bits.ue(), chromaDepth = bits.ue();
  if (lumaDepth > 7 || chromaDepth > 7) throw new TypeError("Unsupported HEVC bit depth");
  const header = Buffer.alloc(23);
  header[0] = 1; sps.copy(header, 1, 1, 13);
  header.writeUInt16BE(0xf000, 13); // unspecified minimum spatial segmentation
  header[15] = 0xfc; // unspecified parallelism
  header[16] = 0xfc | chroma;
  header[17] = 0xf8 | lumaDepth; header[18] = 0xf8 | chromaDepth;
  header[21] = ((layersMinus1 + 1) << 3) | (nesting << 2) | 3;
  header[22] = sets.length;
  return Buffer.concat([header, ...grouped.flatMap((group, i) => [Buffer.from([128 | sets[i]]), u16(group.length), ...group.flatMap((nal) => [u16(nal.length), nal])])]);
}
function header(codec, privateData, width, height) {
  const ebml = element("1a45dfa3", Buffer.concat([number("4286", 1), number("42f7", 1), number("42f2", 4), number("42f3", 8), text("4282", "matroska"), number("4287", 4), number("4285", 2)]));
  const info = element("1549a966", Buffer.concat([number("2ad7b1", 1000000), text("4d80", "eufy-wall"), text("5741", "eufy-wall")]));
  const tracks = element("1654ae6b", element("ae", Buffer.concat([number("d7", 1), number("73c5", 1), number("83", 1), text("86", codec === "h264" ? "V_MPEG4/ISO/AVC" : "V_MPEGH/ISO/HEVC"), element("63a2", privateData), element("e0", Buffer.concat([number("b0", width), number("ba", height)]))])));
  return Buffer.concat([ebml, Buffer.from("1853806701ffffffffffffff", "hex"), info, tracks]);
}

export function newVideoMatroskaMuxer({ codec, maxFrameBytes = 4 * 1024 * 1024 } = {}) {
  if (codec !== "h264" && codec !== "h265") throw new TypeError("Matroska codec must be h264 or h265");
  if (!Number.isSafeInteger(maxFrameBytes) || maxFrameBytes < 1) throw new TypeError("maxFrameBytes must be positive");
  let previousSource, elapsedMs = 0, privateData, format;
  const latestSets = new Map();
  function push(frame) {
    if (!Buffer.isBuffer(frame?.data) || !frame.data.length || frame.data.length > maxFrameBytes) throw new TypeError("Video AU outside size bound");
    if (frame.codec !== undefined && frame.codec !== codec) throw new TypeError("Source codec changed");
    if (!Number.isInteger(frame.sourceTimestamp) || frame.sourceTimestamp < 0 || frame.sourceTimestamp > 0xffffffff) throw new TypeError("sourceTimestamp must be uint32");
    const step = previousSource === undefined ? 0 : ((frame.sourceTimestamp - previousSource + 2 ** 31) % 2 ** 32 + 2 ** 32) % 2 ** 32 - 2 ** 31;
    if (step < 0) throw new TimestampDiscontinuityError(step);
    const nals = annexBNals(frame.data);
    const types = codec === "h264" ? [7,8] : [32,33,34];
    let setsPresent = false;
    for (const type of types) {
      const found = nals.filter((nal) => (codec === "h264" ? nal[0]&31 : (nal[0]>>1)&63) === type);
      if (found.length) { latestSets.set(type,found); setsPresent=true; }
    }
    let prefix = Buffer.alloc(0);
    if (!privateData || setsPresent) {
      const config = configuration([...latestSets.values()].flat(), codec);
      const nextFormat = codec === "h265" ? config.subarray(16,19).toString("hex") : AVC_EXTENDED_PROFILES.has(config[1]) ? config.subarray(-4,-1).toString("hex") : "fdf8f8";
      if (format !== undefined && format !== nextFormat) throw new TypeError("Source chroma/bit depth changed; start a new Matroska stream");
      if (!privateData) {
        if (!Number.isInteger(frame.width) || !Number.isInteger(frame.height) || frame.width < 1 || frame.height < 1 || frame.width > 16384 || frame.height > 16384) throw new TypeError("Valid coded dimensions required for Matroska track");
        prefix = header(codec, config, frame.width, frame.height); privateData = config; format=nextFormat;
      }
    }
    const packet = Buffer.concat(nals.flatMap((nal) => { const length = Buffer.alloc(4); length.writeUInt32BE(nal.length); return [length, nal]; }));
    const keyframe = nals.some((nal) => codec === "h264" ? (nal[0] & 31) === 5 : [19, 20, 21].includes((nal[0] >> 1) & 63));
    elapsedMs += step; previousSource = frame.sourceTimestamp;
    const cluster = element("1f43b675", Buffer.concat([number("e7", elapsedMs), element("a3", Buffer.concat([Buffer.from([0x81, 0, 0, keyframe ? 128 : 0]), packet]))]));
    return Buffer.concat([prefix, cluster]);
  }
  return { push, get pts90k() { return elapsedMs * 90; } };
}
