// FLV has a finite 24-bit length for every encoded video access unit. The
// go2rtc FLV reader can forward it immediately, including units over 64 KiB.
import { annexBNals, videoCodecConfiguration } from "./video-matroska.mjs";
import { TimestampDiscontinuityError } from "./video-mpegts.mjs";

function tag(type, data, timeMs) {
  if (data.length > 0xffffff) throw new RangeError("FLV tag exceeds finite length bound");
  const out = Buffer.alloc(11 + data.length + 4);
  out[0] = type;
  out.writeUIntBE(data.length, 1, 3);
  const timestamp = timeMs >>> 0;
  out.writeUIntBE(timestamp & 0xffffff, 4, 3);
  out[7] = timestamp >>> 24;
  data.copy(out, 11);
  out.writeUInt32BE(11 + data.length, out.length - 4);
  return out;
}
function metadata(codec) {
  // go2rtc's probe deliberately ignores FLV header flags (some cameras lie).
  // Explicit video-only metadata prevents its optional-audio probe waiting.
  const name = Buffer.from("onMetaData"), key = Buffer.from("videocodecid");
  const data = Buffer.alloc(3 + name.length + 5 + 2 + key.length + 9 + 3);
  let at = 0;
  data[at++] = 2; data.writeUInt16BE(name.length, at); at += 2;
  name.copy(data, at); at += name.length;
  data[at++] = 8; data.writeUInt32BE(1, at); at += 4;
  data.writeUInt16BE(key.length, at); at += 2; key.copy(data, at); at += key.length;
  data[at++] = 0; data.writeDoubleBE(codec === "h264" ? 7 : 12, at); at += 8;
  data.set([0, 0, 9], at);
  return tag(18, data, 0);
}
const FILE_HEADER = Buffer.from([70, 76, 86, 1, 1, 0, 0, 0, 9, 0, 0, 0, 0]);

export function newVideoFlvMuxer({ codec, maxFrameBytes = 4 * 1024 * 1024 } = {}) {
  if (codec !== "h264" && codec !== "h265") throw new TypeError("FLV codec must be h264 or h265");
  if (!Number.isSafeInteger(maxFrameBytes) || maxFrameBytes < 1 || maxFrameBytes > 0xffffff - 5) throw new TypeError("maxFrameBytes outside FLV length bound");
  const sets = codec === "h264" ? [7, 8] : [32, 33, 34];
  const latest = new Map();
  let previousSource, elapsedMs = 0, config, started = false;
  function push(frame) {
    if (!Buffer.isBuffer(frame?.data) || !frame.data.length || frame.data.length > maxFrameBytes) throw new TypeError("Video AU outside size bound");
    if (frame.codec !== undefined && frame.codec !== codec) throw new TypeError("Source codec changed");
    if (!Number.isInteger(frame.sourceTimestamp) || frame.sourceTimestamp < 0 || frame.sourceTimestamp > 0xffffffff) throw new TypeError("sourceTimestamp must be uint32");
    const step = previousSource === undefined ? 0 : ((frame.sourceTimestamp - previousSource + 2 ** 31) % 2 ** 32 + 2 ** 32) % 2 ** 32 - 2 ** 31;
    if (step < 0) throw new TimestampDiscontinuityError(step);
    const nals = annexBNals(frame.data);
    const type = (nal) => codec === "h264" ? nal[0] & 31 : (nal[0] >> 1) & 63;
    if (!nals.some((nal) => codec === "h264" ? type(nal) >= 1 && type(nal) <= 5 : type(nal) <= 31)) throw new TypeError("Video AU has no coded picture");
    const updated = new Map(latest);
    let sawSet = false;
    for (const t of sets) {
      const found = nals.filter((nal) => type(nal) === t);
      if (found.length) { updated.set(t, found.map((nal) => Buffer.from(nal))); sawSet = true; }
    }
    let nextConfig = config;
    if (!config || sawSet) nextConfig = videoCodecConfiguration([...updated.values()].flat(), codec);
    const keyframe = nals.some((nal) => codec === "h264" ? type(nal) === 5 : [19, 20, 21].includes(type(nal)));
    const timestamp = elapsedMs + step;
    const chunks = [];
    if (!started) chunks.push(FILE_HEADER, metadata(codec));
    if (!config || !nextConfig.equals(config)) {
      chunks.push(tag(9, Buffer.concat([codec === "h264" ? Buffer.from([0x17, 0, 0, 0, 0]) : Buffer.from([0x90, 104, 118, 99, 49]), nextConfig]), timestamp));
    }
    const length = nals.reduce((sum, nal) => sum + 4 + nal.length, 5);
    if (length > 0xffffff) throw new RangeError("Video AU exceeds FLV tag bound");
    const body = Buffer.allocUnsafe(length);
    body.set(codec === "h264" ? [((keyframe ? 1 : 2) << 4) | 7, 1, 0, 0, 0] : [0x80 | ((keyframe ? 1 : 2) << 4) | 3, 104, 118, 99, 49]);
    let at = 5;
    for (const nal of nals) { body.writeUInt32BE(nal.length, at); at += 4; nal.copy(body, at); at += nal.length; }
    chunks.push(tag(9, body, timestamp));
    // Commit only after validation, so a rejected unit cannot alter the clock.
    previousSource = frame.sourceTimestamp; elapsedMs = timestamp; config = nextConfig; started = true;
    latest.clear(); for (const [t, group] of updated) latest.set(t, group);
    return Buffer.concat(chunks);
  }
  return { push, get pts90k() { return elapsedMs * 90; } };
}
