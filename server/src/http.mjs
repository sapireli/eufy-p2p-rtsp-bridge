// HTTP surface. Video is plain chunked HTTP (go2rtc pulls it); everything else is small JSON for curl,
// the display clients (/api/cameras) and monitoring (/healthz). First-run auth is driven with curl.

function json(res, code, body) {
  const s = JSON.stringify(body);
  res.writeHead(code, { "content-type": "application/json", "content-length": Buffer.byteLength(s) });
  res.end(s);
}

export function createHttpHandler(ctx) {
  const { state } = ctx;
  const { flags } = state;

  async function auth(req, res, url, kind) {
    if (kind === "status") return json(res, 200, ctx.authStatus());
    if (kind === "captcha" && req.method === "GET") {
      const img = flags.lastLogin?.image;
      if (!img) return json(res, 404, { error: "no captcha pending" });
      const m = /^data:(image\/\w+);base64,(.*)$/.exec(img);
      if (!m) return json(res, 500, { error: "unexpected captcha image format" });
      const buf = Buffer.from(m[2], "base64");
      res.writeHead(200, { "content-type": m[1], "content-length": buf.length });
      return res.end(buf);
    }
    if (req.method !== "POST") return json(res, 405, { error: "POST required" });
    const code = url.searchParams.get("code");
    try {
      if (kind === "tfa") {
        if (!/^\d{6}$/.test(code ?? "")) return json(res, 400, { error: "code must be 6 digits: POST /auth/tfa?code=123456" });
        await ctx.applyLogin(await ctx.eufy.submitVerifyCode(code));
      } else if (kind === "captcha") {
        if (!code) return json(res, 400, { error: "POST /auth/captcha?code=<answer>" });
        await ctx.applyLogin(await ctx.eufy.solveCaptcha(code));
      } else if (kind === "retry") {
        await ctx.applyLogin(await ctx.eufy.login());
      } else return json(res, 404, { error: "not found" });
      return json(res, 200, ctx.authStatus());
    } catch (e) {
      return json(res, 502, { error: String(e?.message ?? e), auth: ctx.authStatus() });
    }
  }

  return async function handle(req, res) {
    try {
      return await route(req, res);
    } catch (e) {
      // A throw before/without a response (e.g. malformed URL or Host header) must not leave the socket
      // hanging open; once headers are out there is nothing safe left to write, so just close.
      console.error(`[bridge] ${req.method} ${req.url}: ${e?.stack ?? e}`);
      if (res.headersSent) return res.destroy();
      return json(res, 500, { error: String(e?.message ?? e) });
    }
  };

  async function route(req, res) {
    const url = new URL(req.url, `http://${req.headers.host ?? "localhost"}`);
    const [, kind, arg] = url.pathname.split("/");
    const host = (req.headers.host ?? "127.0.0.1").replace(/:\d+$/, "");

    if (url.pathname === "/healthz") {
      const idle = Date.now() - flags.lastActivity;
      return json(res, 200, {
        ok: true,
        schemaVersion: ctx.SCHEMA_VERSION,
        auth: ctx.authStatus(),
        sessionLost: flags.sessionLost,
        streaming: [...state.streaming],
        blocked: Object.fromEntries(state.blocked),
        cameras: ctx.listCameras().length,
        stalls: Object.fromEntries([...state.slots.values()].map((s) => [s.sn, s.stalls])),
        stalled: flags.ready && idle >= ctx.stallThresholdMs(),
        pushConnected: flags.pushConnected,
        go2rtc: flags.go2rtcProc ? "running" : "stopped",
      });
    }
    if (kind === "auth") return auth(req, res, url, arg);
    if (!flags.ready) return json(res, 503, { error: "not authenticated", auth: ctx.authStatus() });

    if (url.pathname === "/api/cameras") return json(res, 200, ctx.listCameras().map((c) => ctx.apiShape(c, host)));

    if (kind === "stream" && arg) {
      const cam = ctx.getCamera(arg);
      if (!cam || !cam.enabled) return json(res, 404, { error: "unknown or disabled camera" });
      if (ctx.isBlocked(arg)) return json(res, 423, { error: `blocked: ${state.blocked.get(arg)}` });
      res.writeHead(200, { "content-type": "video/H264", "cache-control": "no-cache", connection: "close" });
      const detach = ctx.attachConsumer(arg, res);
      req.on("close", detach);
      res.on("close", detach);
      return;
    }

    if (kind === "snapshot" && arg) {
      try {
        const cam = (await ctx.eufy.getDevice(arg)).camera?.();
        if (!cam?.snapshotLive) return json(res, 404, { error: "no camera on this device" });
        const { jpeg } = await cam.snapshotLive();
        res.writeHead(200, { "content-type": "image/jpeg", "content-length": jpeg.length });
        return res.end(jpeg);
      } catch (e) {
        return json(res, 502, { error: String(e?.message ?? e) });
      }
    }

    return json(res, 404, { error: "not found" });
  }
}
