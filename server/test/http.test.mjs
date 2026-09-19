import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { createHttpHandler } from "../src/http.mjs";
import { createState } from "../src/state.mjs";

function ctxWith(over = {}) {
  const state = createState();
  state.flags.ready = true;
  const cams = [{ sn: "A", enabled: true }, { sn: "B", enabled: false }];
  return {
    cfg: { port: 3000 },
    state,
    SCHEMA_VERSION: 1,
    authStatus: () => ({ state: "ok" }),
    stallThresholdMs: () => 60_000,
    listCameras: () => cams,
    getCamera: (sn) => cams.find((c) => c.sn === sn),
    apiShape: (c, host) => ({ sn: c.sn, rtsp: `rtsp://${host}:8554/${c.sn}` }),
    streamStatus: () => ({ streaming: true, stalls: 0 }),
    attachConsumer: (sn, res) => { res.write(Buffer.from([0, 0, 0, 1, 0x67])); return () => {}; },
    isBlocked: (sn) => state.blocked.has(sn),
    eufy: {},
    ...over,
  };
}

async function withServer(ctx, fn) {
  const srv = http.createServer(createHttpHandler(ctx));
  await new Promise((r) => srv.listen(0, "127.0.0.1", r));
  const base = `http://127.0.0.1:${srv.address().port}`;
  try { await fn(base); } finally { srv.close(); }
}

test("healthz and api/cameras", async () => {
  await withServer(ctxWith(), async (base) => {
    const h = await (await fetch(`${base}/healthz`)).json();
    assert.equal(h.ok, true);
    assert.equal(h.auth.state, "ok");
    assert.equal(h.cameras, 2);
    const cams = await (await fetch(`${base}/api/cameras`)).json();
    assert.deepEqual(cams.map((c) => c.sn), ["A", "B"]);
    assert.match(cams[0].rtsp, /^rtsp:\/\/127\.0\.0\.1:8554\/A$/);
  });
});

test("stream: 200 for enabled, 404 disabled/unknown, 423 blocked, 503 not ready", async () => {
  const ctx = ctxWith();
  await withServer(ctx, async (base) => {
    const r = await fetch(`${base}/stream/A`);
    assert.equal(r.status, 200);
    assert.equal(r.headers.get("content-type"), "video/H264");
    const reader = r.body.getReader();
    const { value } = await reader.read();
    assert.deepEqual([...value], [0, 0, 0, 1, 0x67]);
    await reader.cancel();
    assert.equal((await fetch(`${base}/stream/B`)).status, 404);
    assert.equal((await fetch(`${base}/stream/ZZ`)).status, 404);
    ctx.state.blocked.set("A", "wan-path 1.2.3.4");
    assert.equal((await fetch(`${base}/stream/A`)).status, 423);
    ctx.state.flags.ready = false;
    assert.equal((await fetch(`${base}/stream/A`)).status, 503);
  });
});

test("auth endpoints drive login", async () => {
  const calls = [];
  const ctx = ctxWith({
    authStatus: () => ({ state: "require_2fa", method: "email" }),
    applyLogin: async (r) => calls.push(r),
    eufy: {
      submitVerifyCode: async (code) => ({ status: "ok", code }),
      solveCaptcha: async (ans) => ({ status: "ok", ans }),
      login: async () => ({ status: "ok" }),
    },
  });
  ctx.state.flags.lastLogin = { status: "captcha", image: "data:image/png;base64,aGk=" };
  await withServer(ctx, async (base) => {
    assert.equal((await (await fetch(`${base}/auth/status`)).json()).state, "require_2fa");
    const img = await fetch(`${base}/auth/captcha`);
    assert.equal(img.headers.get("content-type"), "image/png");
    assert.equal(Buffer.from(await img.arrayBuffer()).toString(), "hi");
    assert.equal((await fetch(`${base}/auth/tfa?code=123456`, { method: "POST" })).status, 200);
    assert.equal((await fetch(`${base}/auth/captcha?code=AB3D`, { method: "POST" })).status, 200);
    assert.equal((await fetch(`${base}/auth/retry`, { method: "POST" })).status, 200);
    assert.deepEqual(calls.map((c) => c.code ?? c.ans ?? c.status), ["123456", "AB3D", "ok"]);
    assert.equal((await fetch(`${base}/auth/tfa`, { method: "POST" })).status, 400);
  });
});
