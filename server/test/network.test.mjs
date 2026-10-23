import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { once } from "node:events";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { WebSocket } from "ws";
import { parse } from "yaml";
import { listenHttp, requestHost, urlHost } from "../src/network.mjs";
import { createHttpHandler } from "../src/http.mjs";
import { createWsHub } from "../src/ws-hub.mjs";
import { createState } from "../src/state.mjs";
import { createFreshness } from "../src/freshness.mjs";
import { createCameras } from "../src/cameras.mjs";
import { createGo2rtc } from "../src/go2rtc.mjs";

test("URL authorities preserve IPv6 and explicit hostname/IPv4 bindings", () => {
  for (const host of ["127.0.0.1", "bridge.test", "::1", "2001:db8::3000"]) {
    const authority = `${urlHost(host)}:3000`;
    assert.equal(requestHost(authority), host);
    assert.equal(requestHost(urlHost(host)), host);
  }
  assert.equal(urlHost("[::1]"), "[::1]");
  assert.throws(() => requestHost("bridge.test/path"));
  assert.throws(() => requestHost("operator@bridge.test"));
});

test("one listener serves real HTTP and WebSockets on IPv4 and IPv6 with usable URLs", { timeout: 10000 }, async t => {
  const cam = { sn: "CAM", name: "Door", enabled: true, mode: "always" };
  const state = createState(); state.flags.ready = true; state.flags.lastActivity = Date.now();
  state.slots.set(cam.sn, { codec: "h265", width: 1920, height: 1080 });
  const ctx = { cfg: { port: 0, rtspPort: 9554, go2rtcTranscode: "never" }, state,
    SCHEMA_VERSION: 1, bridgeId: "3e51d183-1314-4f3a-8536-f69d4a58cc7e", authStatus: () => ({ state: "ok" }), stallThresholdMs: () => 60000,
    listCameras: () => [cam], getCamera: () => cam, streamStatus: () => ({ streaming: true }),
    streamKeyFor: () => "door", sdk: { snapshotStored: async () => undefined } };
  ctx.freshness = createFreshness(ctx);
  ctx.apiShape = createCameras(ctx).apiShape;
  const hub = createWsHub(ctx), server = http.createServer(createHttpHandler(ctx));
  hub.attach(server);
  const sockets = [];
  t.after(async () => { sockets.forEach(ws => ws.terminate()); hub.close(); await new Promise(r => server.close(r)); });
  const address = await listenHttp(server, { port: 0 });
  ctx.cfg.port = address.port;
  for (const host of ["127.0.0.1", "::1"]) {
    const origin = `http://${urlHost(host)}:${address.port}`;
    assert.equal((await (await fetch(`${origin}/healthz`)).json()).auth.state, "ok");
    const response = await fetch(`${origin}/api/cameras`);
    assert.equal(response.headers.get("x-eufy-wall-bridge-id"), ctx.bridgeId);
    const [camera] = await response.json();
    const ws = new WebSocket(`ws://${urlHost(host)}:${address.port}/ws`); sockets.push(ws);
    const [raw] = await once(ws, "message");
    const hello = JSON.parse(raw);
    assert.deepEqual(hello.auth, { state: "ok" });
    assert.equal(hello.ready, true);
    assert.equal(hello.bridgeId, ctx.bridgeId);
    for (const record of [camera, hello.cameras[0]]) {
      assert.ok(record.rtsp.startsWith(`rtsp://${urlHost(host)}:9554/`));
      assert.ok(record.nativeRendition.rtsp.startsWith(`rtsp://${urlHost(host)}:9554/`));
      for (const capability of [record.freshness, record.nativeRendition.freshness]) {
        assert.equal(capability.clockUrl, `${origin}/api/clock`);
        assert.equal((await fetch(capability.clockUrl)).status, 200);
      }
    }
  }
  ctx.authStatus = () => ({ state: "require_2fa", method: "email" });
  state.flags.ready = false;
  const snapshots = sockets.map(ws => once(ws, "message"));
  await hub.broadcastSnapshot();
  const messages = await Promise.all(snapshots);
  assert.match(JSON.parse(messages[1][0]).cameras[0].rtsp, /^rtsp:\/\/\[::1\]:9554\//);
  for (const [raw] of messages) {
    assert.deepEqual(JSON.parse(raw).auth, { state: "require_2fa" });
    assert.equal(JSON.parse(raw).ready, false);
  }
});

test("IPv6 self_host produces usable internal copy, transcode and native pull URLs", async () => {
  const dir = mkdtempSync(join(tmpdir(), "ewb-v6-pull-"));
  try {
    for (const selfHost of ["::1", "[::1]"]) {
      const cfg = { selfHost, port: 3000, go2rtcConfig: join(dir, "go2rtc.yaml"), go2rtcTranscode: "auto" };
      const cameras = [{ sn: "A", enabled: true }, { sn: "B", enabled: true }];
      const state = createState(); state.slots.set("B", { codec: "h265", width: 1920, height: 1080 });
      const ctx = { cfg, state, listCameras: () => cameras, getCamera: sn => cameras.find(c => c.sn === sn) };
      ctx.freshness = createFreshness(ctx);
      await createGo2rtc(ctx).writeGo2rtc();
      const sources = Object.values(parse(readFileSync(cfg.go2rtcConfig, "utf8")).streams).flat();
      assert.ok(sources.length >= 3);
      for (const source of sources) assert.match(source, /http:\/\/\[::1\]:3000\//);
      assert.ok(sources.some(source => source.includes("rendition=native")));
    }
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
