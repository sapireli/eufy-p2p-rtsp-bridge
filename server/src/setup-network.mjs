import net from "node:net";
import { inCidr } from "./lan-guard.mjs";
import { isValidCidr } from "./config.mjs";

const RTSP_PORT = 8554;

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
    let response = "";
    const finish = (error, result) => {
      if (settled) return;
      settled = true;
      socket.destroy();
      if (error) reject(error); else resolve(result);
    };
    socket.setTimeout(timeoutMs, () => finish(new Error(`RTSP probe timed out after ${timeoutMs} ms; check camera wake time, go2rtc, and port ${port}`)));
    socket.on("error", (error) => finish(new Error(`RTSP ${host}:${port} unavailable: ${error.message}`)));
    socket.on("connect", () => {
      const uri = `rtsp://${host}:${port}/${encodeURIComponent(streamKey)}`;
      socket.write(`DESCRIBE ${uri} RTSP/1.0\r\nCSeq: 1\r\nAccept: application/sdp\r\nUser-Agent: eufy-bridge-setup\r\n\r\n`);
    });
    socket.on("data", (chunk) => {
      response += chunk;
      if (response.length > 64 * 1024) return finish(new Error("RTSP response exceeded 64 KiB"));
      const line = response.split("\r\n", 1)[0];
      const match = /^RTSP\/1\.0 (\d{3}) (.*)$/.exec(line);
      if (!match) return;
      const status = Number(match[1]);
      if (status !== 200) return finish(new Error(`RTSP DESCRIBE returned ${status} ${match[2]}; check the camera stream key and bridge logs`));
      const headerEnd = response.indexOf("\r\n\r\n");
      if (headerEnd < 0) return;
      const header = response.slice(0, headerEnd);
      const length = Number(/^content-length:\s*(\d+)/im.exec(header)?.[1] ?? 0);
      const body = response.slice(headerEnd + 4);
      if (length && body.length < length) return;
      if (/^m=video\s/im.test(body)) {
        const codecName = /^a=rtpmap:\d+\s+(H264|H265|HEVC)\//im.exec(body)?.[1]?.toLowerCase();
        const codec = codecName === "hevc" ? "h265" : codecName ?? null;
        finish(null, { ok: true, status, codec, uri: `rtsp://${host}:${port}/${encodeURIComponent(streamKey)}` });
      }
      else if (length) finish(new Error("RTSP DESCRIBE returned no video track; check camera codec and bridge logs"));
    });
    socket.on("end", () => finish(new Error("RTSP peer closed before a DESCRIBE response")));
  });
}

/** A real H.264/H.265 picture slice, ignoring SPS/PPS/VPS and other header-only traffic. */
export function hasVideoSlice(bytes, codec) {
  let kind = codec;
  for (let i = 0; i < bytes.length - 4; i++) {
    if (bytes[i] !== 0 || bytes[i + 1] !== 0) continue;
    const start = bytes[i + 2] === 1 ? i + 3 : bytes[i + 2] === 0 && bytes[i + 3] === 1 ? i + 4 : -1;
    if (start < 0 || start >= bytes.length) continue;
    const h264 = bytes[start] & 0x1f;
    const h265 = (bytes[start] >> 1) & 0x3f;
    if (!kind && (h264 === 7 || h264 === 8)) kind = "h264";
    if (!kind && (h265 === 32 || h265 === 33 || h265 === 34)) kind = "h265";
    if (kind === "h264" && (h264 === 1 || h264 === 5) && start + 1 < bytes.length) return true;
    if (kind === "h265" && h265 <= 31 && start + 2 < bytes.length) return true;
  }
  return false;
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
    let bytes = Buffer.alloc(0);
    for (;;) {
      const { value, done } = await reader.read();
      if (done) throw new Error("HTTP stream ended before a video slice arrived");
      if (!value?.length) continue;
      bytes = Buffer.concat([bytes, Buffer.from(value)]);
      if (bytes.length > 2 * 1024 * 1024) throw new Error("HTTP stream sent 2 MiB without a video slice");
      if (hasVideoSlice(bytes, codec)) return bytes.length;
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
