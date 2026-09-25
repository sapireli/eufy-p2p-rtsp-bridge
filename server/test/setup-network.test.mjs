import { test } from "node:test";
import assert from "node:assert/strict";
import net from "node:net";
import { bootstrapConfig, validateSetupNetwork, localProbeHost, probeRtsp, probeCameraRtsp, probeBridgeFrame, hasVideoSlice } from "../src/setup-network.mjs";

const interfaces = [{ name: "eth0", address: "192.168.10.12", cidr: "192.168.10.12/24" }];

test("setup authenticates on loopback before exposing the final client listener", () => {
  const final = { schema_version: 2, host: "0.0.0.0", port: 3000, cameras: { BAT: { mode: "on_motion" } } };
  const initial = bootstrapConfig(final);
  assert.equal(initial.host, "127.0.0.1");
  assert.equal(final.host, "0.0.0.0");
  assert.deepEqual(initial.cameras, final.cameras);
});

test("setup rejects LAN policies and bind addresses that cannot work on this host", () => {
  const valid = { lanCidr: "192.168.10.0/24", force: true, host: "192.168.10.12", publicHost: "wall.example", interfaces };
  assert.equal(validateSetupNetwork(valid).host, "192.168.10.12");
  assert.throws(() => validateSetupNetwork({ ...valid, lanCidr: "10.0.0.0/24" }), /contains no local IPv4/);
  assert.throws(() => validateSetupNetwork({ ...valid, host: "192.168.10.99" }), /not a detected local/);
  assert.throws(() => validateSetupNetwork({ ...valid, publicHost: "localhost" }), /client address is loopback/);
  assert.throws(() => validateSetupNetwork({ ...valid, publicHost: "http:\/\/bad" }), /without a scheme/);
  assert.throws(() => validateSetupNetwork({ ...valid, lanCidr: null }), /requires a LAN CIDR/);
});

test("local RTSP probe uses the bind address instead of client DNS", () => {
  assert.equal(localProbeHost("0.0.0.0", interfaces), "192.168.10.12");
  assert.equal(localProbeHost("192.168.10.12", interfaces), "192.168.10.12");
  assert.equal(localProbeHost("127.0.0.1", interfaces), "127.0.0.1");
});

async function withRtsp(t, responder) {
  const server = net.createServer((socket) => {
    let request = "";
    socket.on("data", (chunk) => {
      request += chunk;
      if (request.includes("\r\n\r\n")) responder(socket, request);
    });
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(() => server.close());
  return server.address().port;
}

test("RTSP probe accepts a video SDP from the selected stream path", async (t) => {
  let request;
  const port = await withRtsp(t, (socket, text) => {
    request = text;
    const sdp = "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\n";
    socket.end(`RTSP/1.0 200 OK\r\nContent-Type: application/sdp\r\nContent-Length: ${sdp.length}\r\n\r\n${sdp}`);
  });
  const result = await probeRtsp({ host: "127.0.0.1", streamKey: "front_door", port, timeoutMs: 500 });
  assert.equal(result.status, 200);
  assert.equal(result.codec, "h264");
  assert.match(request, /DESCRIBE rtsp:\/\/127\.0\.0\.1:\d+\/front_door RTSP\/1\.0/);
});

test("RTSP probe rejects missing paths and responses without video", async (t) => {
  const missing = await withRtsp(t, (socket) => socket.end("RTSP/1.0 404 Not Found\r\nCSeq: 1\r\n\r\n"));
  await assert.rejects(probeRtsp({ host: "127.0.0.1", streamKey: "gone", port: missing, timeoutMs: 500 }), /404 Not Found/);
  const noVideo = await withRtsp(t, (socket) => socket.end("RTSP/1.0 200 OK\r\nContent-Length: 5\r\n\r\nv=0\r\n"));
  await assert.rejects(probeRtsp({ host: "127.0.0.1", streamKey: "bad", port: noVideo, timeoutMs: 500 }), /no video track/);
});

test("RTSP probe has a bounded timeout for a camera that never responds", async (t) => {
  const port = await withRtsp(t, () => {});
  await assert.rejects(probeRtsp({ host: "127.0.0.1", streamKey: "sleeping", port, timeoutMs: 30 }), /timed out/);
});

test("RTSP deadline is absolute even when a peer trickles bytes", async (t) => {
  const port = await withRtsp(t, (socket) => {
    const trickle = setInterval(() => socket.write("x"), 5);
    socket.on("close", () => clearInterval(trickle));
    socket.on("error", () => clearInterval(trickle));
  });
  const started = Date.now();
  await assert.rejects(probeRtsp({ host: "127.0.0.1", streamKey: "sleeping", port, timeoutMs: 35 }), /timed out/);
  assert.ok(Date.now() - started < 300);
});

test("RTSP probe handles fragmented headers and rejects malformed or oversized replies", async (t) => {
  const sdp = "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H265/90000\r\n";
  const fragmented = await withRtsp(t, (socket) => {
    const bytes = Buffer.from(`RTSP/1.0 200 OK\r\nContent-Length: ${sdp.length}\r\n\r\n${sdp}`);
    let index = 0;
    const send = () => { if (index === bytes.length) return socket.end(); socket.write(bytes.subarray(index, ++index)); setImmediate(send); };
    send();
  });
  assert.equal((await probeRtsp({ host: "127.0.0.1", streamKey: "door", port: fragmented, timeoutMs: 1000 })).codec, "h265");
  const badStatus = await withRtsp(t, (socket) => socket.end("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nv=0\r\n"));
  await assert.rejects(probeRtsp({ host: "127.0.0.1", streamKey: "door", port: badStatus, timeoutMs: 500 }), /malformed status line/);
  const badLength = await withRtsp(t, (socket) => socket.end("RTSP/1.0 200 OK\r\nContent-Length: banana\r\n\r\n"));
  await assert.rejects(probeRtsp({ host: "127.0.0.1", streamKey: "door", port: badLength, timeoutMs: 500 }), /valid Content-Length/);
  const hugeLength = await withRtsp(t, (socket) => socket.end("RTSP/1.0 200 OK\r\nContent-Length: 999999999\r\n\r\n"));
  await assert.rejects(probeRtsp({ host: "127.0.0.1", streamKey: "door", port: hugeLength, timeoutMs: 500 }), /body length is invalid/);
  const hugeHeader = await withRtsp(t, (socket) => socket.end(`RTSP/1.0 200 OK\r\nX-Fill: ${"x".repeat(17_000)}\r\n\r\n`));
  await assert.rejects(probeRtsp({ host: "127.0.0.1", streamKey: "door", port: hugeHeader, timeoutMs: 500 }), /headers exceeded/);
  const truncated = await withRtsp(t, (socket) => socket.end("RTSP/1.0 200 OK\r\nContent-Length: 100\r\n\r\nv=0\r\n"));
  await assert.rejects(probeRtsp({ host: "127.0.0.1", streamKey: "door", port: truncated, timeoutMs: 500 }), /closed before a complete/);
});

test("battery probe releases its bounded hold even when RTSP fails", async () => {
  const calls = [];
  const fetchImpl = async (url, options) => { calls.push({ url, method: options.method }); return { ok: true }; };
  await assert.rejects(probeCameraRtsp({ sn: "BAT", mode: "on_motion", streamKey: "door" }, {
    bridgeUrl: "http://127.0.0.1:3000", host: "192.168.10.12", timeoutMs: 30, fetchImpl,
    probeImpl: async () => { throw new Error("RTSP failed"); },
  }), /RTSP failed/);
  assert.deepEqual(calls.map((c) => c.method), ["POST", "DELETE"]);
  assert.match(calls[0].url, /seconds=6/);
});

test("live probe requires actual stream bytes after a valid RTSP path", async () => {
  const calls = [];
  const fetchImpl = async (url, options) => {
    calls.push({ url, method: options.method });
    if (url.includes("/stream/")) return { ok: true, body: new ReadableStream({ start(c) { c.enqueue(new Uint8Array([0, 0, 0, 1, 0x67, 0x42, 0, 0, 0, 1, 0x65, 0x88])); c.close(); } }) };
    return { ok: true };
  };
  const result = await probeCameraRtsp({ sn: "BAT", mode: "on_motion", streamKey: "door" }, {
    bridgeUrl: "http://127.0.0.1:3000", host: "192.168.10.12", timeoutMs: 2_000, fetchImpl,
    probeImpl: async () => ({ ok: true, status: 200, codec: "h264", uri: "rtsp://192.168.10.12:8554/door" }),
  });
  assert.equal(result.frameBytes, 12);
  assert.deepEqual(calls.map((c) => c.method), ["POST", undefined, "DELETE"]);
});

test("a successful SDP without frames fails the live probe with a remedy", async () => {
  const fetchImpl = async () => ({ ok: true, body: new ReadableStream({ start(c) { c.close(); } }) });
  await assert.rejects(probeBridgeFrame({ bridgeUrl: "http://127.0.0.1:3000", sn: "A", timeoutMs: 100, fetchImpl }), /no live video bytes.*camera wake/);
});

test("header traffic does not count as a video frame", async () => {
  const spsPps = new Uint8Array([0, 0, 0, 1, 0x67, 0x42, 0, 0, 0, 1, 0x68, 0xee]);
  const vps = new Uint8Array([0, 0, 0, 1, 0x40, 0x01, 0xaa]);
  assert.equal(hasVideoSlice(spsPps, "h264"), false);
  assert.equal(hasVideoSlice(vps, "h265"), false);
  assert.equal(hasVideoSlice(new Uint8Array([...spsPps, 0, 0, 0, 1, 0x65, 0x88]), "h264"), true);
  assert.equal(hasVideoSlice(new Uint8Array([...vps, 0, 0, 0, 1, 0x26, 0x01, 0x88]), "h265"), true);
  assert.equal(hasVideoSlice(new Uint8Array([...spsPps, 0, 0, 0, 1, 0x65, 0x88])), true);
  assert.equal(hasVideoSlice(new Uint8Array([...vps, 0, 0, 0, 1, 0x26, 0x01, 0x88])), true);
  const fetchImpl = async () => ({ ok: true, body: new ReadableStream({ start(c) { c.enqueue(spsPps); c.close(); } }) });
  await assert.rejects(probeBridgeFrame({ bridgeUrl: "http://127.0.0.1:3000", sn: "A", codec: "h264", timeoutMs: 100, fetchImpl }), /ended before a video slice/);
});

test("tiny chunks and split Annex-B start codes find H264 and H265 slices without buffering the stream", async () => {
  for (const [codec, nal] of [["h264", [0x65, 0x88]], ["h265", [0x26, 0x01, 0x88]]]) {
    const bytes = [...new Array(4096).fill(0xff), 0, 0, 0, 1, ...nal];
    const fetchImpl = async () => ({ ok: true, body: new ReadableStream({
      start(controller) { for (const byte of bytes) controller.enqueue(new Uint8Array([byte])); controller.close(); },
    }) });
    assert.equal(await probeBridgeFrame({ bridgeUrl: "http://127.0.0.1:3000", sn: "A", codec, timeoutMs: 1000, fetchImpl }), bytes.length);
  }
});

test("split start codes and incomplete or invalid NALs cannot create a false video success", async () => {
  for (const [codec, bytes] of [
    ["h264", [0, 0, 1, 0x65, 0, 0, 1, 0x67, 0x42]], // the apparent payload zeros begin the next start code
    ["h264", [0, 0, 1, 0xe5, 0x88]], // forbidden_zero_bit
    ["h265", [0, 0, 1, 0x26, 0x00, 0x88]], // temporal_id_plus1 is zero
  ]) {
    const fetchImpl = async () => ({ ok: true, body: new ReadableStream({
      start(controller) { for (const byte of bytes) controller.enqueue(new Uint8Array([byte])); controller.close(); },
    }) });
    await assert.rejects(probeBridgeFrame({ bridgeUrl: "http://127.0.0.1:3000", sn: "A", codec, timeoutMs: 100, fetchImpl }), /ended before a video slice/);
  }
});

test("video probe bounds stream bytes even when no slice arrives", async () => {
  const fetchImpl = async () => ({ ok: true, body: new ReadableStream({ start(controller) { controller.enqueue(new Uint8Array(2 * 1024 * 1024 + 1)); controller.close(); } }) });
  await assert.rejects(probeBridgeFrame({ bridgeUrl: "http://127.0.0.1:3000", sn: "A", codec: "h264", timeoutMs: 100, fetchImpl }), /2 MiB without a video slice/);
});
