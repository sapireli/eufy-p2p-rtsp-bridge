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
    assert.equal((await fetch(`${base}/auth/tfa`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ code: "654321" }) })).status, 200);
    assert.equal((await fetch(`${base}/auth/captcha`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ code: "XY9" }) })).status, 200);
    assert.equal((await fetch(`${base}/auth/retry`, { method: "POST" })).status, 200);
    assert.deepEqual(calls.map((c) => c.code ?? c.ans ?? c.status), ["123456", "AB3D", "654321", "XY9", "ok"]);
    assert.equal((await fetch(`${base}/auth/tfa`, { method: "POST" })).status, 400);
    assert.equal((await fetch(`${base}/auth/tfa`, { method: "POST", headers: { "content-type": "application/json" }, body: "{" })).status, 400);
  });
});

test("a synchronous throw inside the handler answers 500 instead of hanging the socket", async () => {
  const ctx = ctxWith({ listCameras: () => { throw new Error("boom"); } });
  const logs = [];
  const origError = console.error;
  console.error = (...a) => logs.push(a.join(" "));
  try {
    await withServer(ctx, async (base) => {
      const r = await fetch(`${base}/api/cameras`, { signal: AbortSignal.timeout(2000) });
      assert.equal(r.status, 500);
      assert.deepEqual(await r.json(), { error: "boom" });
      // Malformed Host header → `new URL` throws before any route ran.
      const bad = await fetch(`${base}/healthz`, { headers: { host: "not a host:xx" }, signal: AbortSignal.timeout(2000) });
      assert.equal(bad.status, 500);
    });
    assert.equal(logs.length, 2);
  } finally {
    console.error = origError;
  }
});

// A tile shows a still while its camera is asleep or waking. The endpoint serves only what the SDK has
// already retained from a push event, so it can never wake a camera to satisfy a wall.
test("snapshot serves the retained still, and says so plainly when there is none", async () => {
  const jpeg = Buffer.from([0xff, 0xd8, 0xff, 0xe0, 1, 2, 3, 0xff, 0xd9]);
  const asked = [];
  const ctx = ctxWith({
    sdk: {
      snapshotStored: async (sn) => {
        asked.push(sn);
        return sn === "A" ? jpeg : undefined;
      },
    },
  });

  await withServer(ctx, async (base) => {
    const ok = await fetch(`${base}/snapshot/A`);
    assert.equal(ok.status, 200);
    assert.equal(ok.headers.get("content-type"), "image/jpeg");
    assert.equal(ok.headers.get("cache-control"), "no-cache", "a stale still on a wall is worse than a re-fetch");
    assert.deepEqual(Buffer.from(await ok.arrayBuffer()), jpeg);

    // Nothing retained yet is the normal case for a camera that has not moved since the bridge started.
    const none = await fetch(`${base}/snapshot/NOPE_BUT_ENABLED`);
    assert.equal(none.status, 404);

    const disabled = await fetch(`${base}/snapshot/B`);
    assert.equal(disabled.status, 404, "a disabled camera has nothing to show");
  });
  assert.ok(asked.includes("A"));
});

test("a camera with no retained still returns 404 rather than an empty image", async () => {
  const ctx = ctxWith({ sdk: { snapshotStored: async () => undefined } });
  await withServer(ctx, async (base) => {
    const r = await fetch(`${base}/snapshot/A`);
    assert.equal(r.status, 404);
    const body = await r.json();
    assert.match(body.error, /no retained snapshot/i);
  });
});
