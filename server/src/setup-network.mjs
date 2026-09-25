import net from "node:net";
import { inCidr } from "./lan-guard.mjs";
import { isValidCidr } from "./config.mjs";

const RTSP_PORT = 8554;
const MAX_RTSP_BYTES = 64 * 1024;
const MAX_RTSP_HEADER_BYTES = 16 * 1024;
const MAX_STREAM_BYTES = 2 * 1024 * 1024;

/** Validate choices against this host before applying a client-facing listener. */
export function validateSetupNetwork({ lanCidr, force, host, publicHost, interfaces }) {
  if (force && !lanCidr) throw new Error("LAN-only mode requires a LAN CIDR");
  if (lanCidr && !isValidCidr(lanCidr)) throw new Error(`LAN CIDR ${lanCidr} is invalid`);
  if (lanCidr && !interfaces.some((nic) => inCidr(nic.address, lanCidr)))
    throw new Error(`LAN CIDR ${lanCidr} contains no local IPv4 interface; choose one of ${interfaces.map((n) => n.cidr).join(", ") || "the detected networks"}`);
  if (host !== "0.0.0.0" && host !== "127.0.0.1" && !interfaces.some((nic) => nic.address === host))
    throw new Error(`bind address ${host} is not a detected local IPv4 address`);
  if (!publicHost || !/^[a-zA-Z0-9.-]+$/.test(publicHost)) throw new Error("client address must be a hostname or IPv4 address without a scheme or port");
  if ((publicHost === "127.0.0.1" || publicHost === "localhost") && host !== "127.0.0.1")
    throw new Error("client address is loopback; choose the bridge's LAN address for remote clients");
  return { host, publicHost, lanCidr, force };
}

export function bootstrapConfig(finalConfig) {
  return { ...finalConfig, host: "127.0.0.1" };
}

/** Probe a local listener through its bind address; a client-facing DNS name may not resolve on this host. */
export function localProbeHost(host, interfaces) {
  return host === "0.0.0.0" ? interfaces[0]?.address ?? "127.0.0.1" : host;
}

/** Bounded RTSP DESCRIBE. This verifies that go2rtc accepts the camera path and can produce SDP. */
export function probeRtsp({ host, streamKey, port = RTSP_PORT, timeoutMs = 8_000 }) {
  if (!streamKey) return Promise.reject(new Error("camera has no RTSP stream key"));
  return new Promise((resolve, reject) => {
    const socket = net.createConnection({ host, port });
    let settled = false;
    const response = Buffer.alloc(MAX_RTSP_BYTES);
    let used = 0;
    let bodyStart = -1;
    let bodyLength = -1;
    const deadline = setTimeout(() => finish(new Error(`RTSP probe timed out after ${timeoutMs} ms; check camera wake time, go2rtc, and port ${port}`)), timeoutMs);
    const finish = (error, result) => {
      if (settled) return;
      settled = true;
      clearTimeout(deadline);
      socket.destroy();
      if (error) reject(error); else resolve(result);
    };
    socket.on("error", (error) => finish(new Error(`RTSP ${host}:${port} unavailable: ${error.message}`)));
    socket.on("connect", () => {
      const uri = `rtsp://${host}:${port}/${encodeURIComponent(streamKey)}`;
      socket.write(`DESCRIBE ${uri} RTSP/1.0\r\nCSeq: 1\r\nAccept: application/sdp\r\nUser-Agent: eufy-bridge-setup\r\n\r\n`);
    });
    socket.on("data", (chunk) => {
      if (settled) return;
      if (used + chunk.length > MAX_RTSP_BYTES) return finish(new Error("RTSP response exceeded 64 KiB"));
      const oldUsed = used;
      chunk.copy(response, used);
      used += chunk.length;
      if (bodyStart < 0) {
        const headerEnd = response.indexOf("\r\n\r\n", Math.max(0, oldUsed - 3));
        if (headerEnd < 0) {
          if (used > MAX_RTSP_HEADER_BYTES) finish(new Error("RTSP headers exceeded 16 KiB"));
          return;
        }
        bodyStart = headerEnd + 4;
        if (bodyStart > MAX_RTSP_HEADER_BYTES) return finish(new Error("RTSP headers exceeded 16 KiB"));
        const header = response.toString("utf8", 0, headerEnd);
        const [statusLine, ...lines] = header.split("\r\n");
        const match = /^RTSP\/1\.0 ([0-9]{3}) (.+)$/.exec(statusLine);
        if (!match) return finish(new Error("RTSP DESCRIBE returned a malformed status line"));
        const status = Number(match[1]);
        if (status !== 200) return finish(new Error(`RTSP DESCRIBE returned ${status} ${match[2]}; check the camera stream key and bridge logs`));
        if (lines.some((line) => !/^[!#$%&'*+.^_`|~\w-]+:\s*[^\r\n]*$/.test(line)))
          return finish(new Error("RTSP DESCRIBE returned malformed headers"));
        const lengths = lines.filter((line) => /^content-length:/i.test(line));
        if (lengths.length !== 1 || !/^content-length:\s*\d+\s*$/i.test(lengths[0]))
          return finish(new Error("RTSP DESCRIBE requires one valid Content-Length header"));
        bodyLength = Number(lengths[0].split(":", 2)[1].trim());
        if (!bodyLength || !Number.isSafeInteger(bodyLength) || bodyLength > MAX_RTSP_BYTES - bodyStart)
          return finish(new Error("RTSP DESCRIBE body length is invalid or exceeds 64 KiB"));
      }
      if (used < bodyStart + bodyLength) return;
      const body = response.toString("utf8", bodyStart, bodyStart + bodyLength);
      if (!/^m=video\s/im.test(body)) return finish(new Error("RTSP DESCRIBE returned no video track; check camera codec and bridge logs"));
      const codecName = /^a=rtpmap:\d+\s+(H264|H265|HEVC)\//im.exec(body)?.[1]?.toLowerCase();
      const codec = codecName === "hevc" ? "h265" : codecName ?? null;
      finish(null, { ok: true, status: 200, codec, uri: `rtsp://${host}:${port}/${encodeURIComponent(streamKey)}` });
    });
    socket.on("end", () => finish(new Error("RTSP peer closed before a complete DESCRIBE response")));
  });
}

/** Incremental Annex-B scanner; a candidate needs a NAL header and at least one payload byte. */
class VideoSliceScanner {
  constructor(codec) { this.codec = codec; this.zeros = 0; this.headerBytes = 0; this.first = 0; this.second = 0; this.inNal = false; }
  nalByte(byte) {
    if (!this.inNal) return false;
    if (this.headerBytes === 0) {
      this.first = byte;
      this.headerBytes = 1;
      if (!this.codec) {
        const h264 = byte & 0x1f, h265 = (byte >> 1) & 0x3f;
        if (h264 === 7 || h264 === 8) this.codec = "h264";
        else if (h265 === 32 || h265 === 33 || h265 === 34) this.codec = "h265";
      }
      return false;
    }
    const first = this.first;
    if (this.codec === "h264") {
      const type = first & 0x1f;
      return (first & 0x80) === 0 && (type === 1 || type === 5);
    }
    if (this.headerBytes === 1) { this.second = byte; this.headerBytes = 2; return false; }
    return this.codec === "h265" && (first & 0x80) === 0 && (this.second & 0x07) !== 0 && ((first >> 1) & 0x3f) <= 31;
  }
  feed(bytes) {
    for (const byte of bytes) {
      if (byte === 0) { this.zeros++; continue; }
      if (byte === 1 && this.zeros >= 2) {
        this.inNal = true; this.headerBytes = 0; this.zeros = 0;
        continue;
      }
      // Zeros are held until we know they are payload rather than a split start code.
      for (let i = 0; i < Math.min(this.zeros, 3); i++) if (this.nalByte(0)) return true;
      this.zeros = 0;
      if (this.nalByte(byte)) return true;
    }
    return false;
  }
}

/** A real H.264/H.265 picture slice, ignoring SPS/PPS/VPS and other header-only traffic. */
export function hasVideoSlice(bytes, codec) {
  return new VideoSliceScanner(codec).feed(bytes);
}

/** Read a real Annex-B chunk from the bridge. SDP alone does not prove that frames are arriving. */
export async function probeBridgeFrame({ bridgeUrl, sn, codec, timeoutMs = 7_000, fetchImpl = fetch }) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  let reader;
  try {
    const response = await fetchImpl(`${bridgeUrl}/stream/${encodeURIComponent(sn)}`, { signal: controller.signal });
    if (!response.ok) throw new Error(`HTTP stream returned ${response.status}`);
    reader = response.body?.getReader();
    if (!reader) throw new Error("HTTP stream returned no readable body");
    const scanner = new VideoSliceScanner(codec);
    let bytes = 0;
    for (;;) {
      const { value, done } = await reader.read();
      if (done) throw new Error("HTTP stream ended before a video slice arrived");
      if (!value?.length) continue;
      bytes += value.length;
      if (bytes > MAX_STREAM_BYTES) throw new Error("HTTP stream sent 2 MiB without a video slice");
      if (scanner.feed(value)) return bytes;
    }
  } catch (error) {
    throw new Error(`${sn}: no live video bytes within ${timeoutMs} ms (${error.message}); check camera wake, LAN policy, and bridge logs`);
  } finally {
    clearTimeout(timer);
    await reader?.cancel().catch(() => {});
  }
}

/** Take a bounded hold for sleeping cameras, probe, then always release the hold. */
export async function probeCameraRtsp(camera, { bridgeUrl, host, timeoutMs = 12_000, fetchImpl = fetch, probeImpl = probeRtsp, frameProbeImpl = probeBridgeFrame }) {
  const owner = `setup-${process.pid}`;
  const path = `/hold/${encodeURIComponent(camera.sn)}?owner=${owner}&seconds=${Math.ceil((timeoutMs + 5_000) / 1000)}`;
  const needsHold = camera.mode !== "always";
  if (needsHold) {
    const held = await fetchImpl(`${bridgeUrl}${path}`, { method: "POST", signal: AbortSignal.timeout(3_000) });
    if (!held.ok) throw new Error(`${camera.sn}: could not take a bounded stream hold (HTTP ${held.status})`);
  }
  try {
    const started = Date.now();
    const rtsp = await probeImpl({ host, streamKey: camera.streamKey, timeoutMs: Math.min(5_000, timeoutMs) });
    const frameBytes = await frameProbeImpl({ bridgeUrl, sn: camera.sn, codec: rtsp.codec ?? camera.codec, timeoutMs: Math.max(1_000, timeoutMs - (Date.now() - started)), fetchImpl });
    return { ...rtsp, frameBytes };
  }
  finally {
    if (needsHold) await fetchImpl(`${bridgeUrl}${path}`, { method: "DELETE", signal: AbortSignal.timeout(3_000) }).catch(() => {});
  }
}
