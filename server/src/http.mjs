// HTTP surface. Video is plain chunked HTTP (go2rtc pulls it); everything else is small JSON for curl,
// the display clients (/api/cameras) and monitoring (/healthz). First-run auth is driven with curl.

function json(res, code, body) {
  const s = JSON.stringify(body);
  res.writeHead(code, { "content-type": "application/json", "content-length": Buffer.byteLength(s) });
  res.end(s);
}

async function authCode(req, url) {
  // Keep the old query form for existing callers. New setup clients use a JSON body so challenge answers
  // stay out of access logs and URLs. Limit untrusted input before parsing it.
  let body = "";
  for await (const chunk of req) {
    body += chunk;
    if (Buffer.byteLength(body) > 4096) throw Object.assign(new Error("auth request body exceeds 4096 bytes"), { status: 413 });
  }
  if (body) {
    const type = req.headers["content-type"]?.split(";", 1)[0];
    if (type !== "application/json") throw Object.assign(new Error("auth request body must be application/json"), { status: 415 });
    let value;
    try { value = JSON.parse(body); } catch { throw Object.assign(new Error("invalid JSON auth request body"), { status: 400 }); }
    if (!value || typeof value.code !== "string") throw Object.assign(new Error("auth request body requires a string code"), { status: 400 });
    return value.code;
  }
  return url.searchParams.get("code");
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
    try {
      const code = kind === "tfa" || kind === "captcha" ? await authCode(req, url) : null;
      if (kind === "tfa") {
        if (!/^\d{6}$/.test(code ?? "")) return json(res, 400, { error: "code must be 6 digits in a JSON body" });
        await ctx.applyLogin(await ctx.eufy.submitVerifyCode(code));
      } else if (kind === "captcha") {
        if (!code) return json(res, 400, { error: "captcha answer required in a JSON body" });
        await ctx.applyLogin(await ctx.eufy.solveCaptcha(code));
      } else if (kind === "retry") {
        await ctx.applyLogin(await ctx.eufy.login());
      } else return json(res, 404, { error: "not found" });
      return json(res, 200, ctx.authStatus());
    } catch (e) {
      return json(res, e?.status ?? 502, { error: String(e?.message ?? e), auth: ctx.authStatus() });
    }
  }

  return async function handle(req, res) {
    try {
      return await route(req, res);
    } catch (e) {
      // A throw before/without a response (e.g. malformed URL or Host header) must not leave the socket
      // hanging open; once headers are out there is nothing safe left to write, so just close.
      console.error(`[bridge] ${req.method} ${req.url?.split("?", 1)[0]}: ${e?.stack ?? e}`);
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

    // A tile asks for a camera it wants to show. The wall is on a trusted LAN and this only ever makes a
    // camera stream for a bounded time, so it is deliberately unauthenticated like /stream itself.
    //
    // POST /hold/<sn>?owner=<id>&seconds=<n>   take or extend a hold
    // DELETE /hold/<sn>?owner=<id>             release it early (a tile that stopped showing the camera)
    if (kind === "hold" && arg) {
      const cam = ctx.getCamera?.(arg);
      if (!cam || !cam.enabled) return json(res, 404, { error: "unknown or disabled camera" });
      const owner = url.searchParams.get("owner") || `client:${req.socket.remoteAddress ?? "?"}`;
      if (req.method === "DELETE") {
        ctx.holds.release(arg, owner);
        return json(res, 200, { sn: arg, owner, held: ctx.holds.isHeld(arg) });
      }
      if (req.method !== "POST") return json(res, 405, { error: "use POST to take a hold, DELETE to release it" });
      const seconds = Number(url.searchParams.get("seconds")) || undefined;
      const until = ctx.holds.hold(arg, owner, seconds);
      return json(res, 200, { sn: arg, owner, untilMs: until - Date.now(), owners: ctx.holds.owners(arg) });
    }

    if (kind === "holds") return json(res, 200, ctx.holds?.status?.() ?? {});

    // DEBUG API — loopback only, and gated by BRIDGE_DEBUG. Read-only state plus one recovery lever:
    // `p2p` (live sessions and the peer each settled on), `lan` (per-station LAN/relay mode) and `drop`
    // (tear a station's sessions down so the next open re-runs the lookup).
    if (kind === "debug") {
      const remote = req.socket.remoteAddress ?? "";
      const isLocal = remote === "127.0.0.1" || remote === "::1" || remote === "::ffff:127.0.0.1";
      if (!ctx.DEBUG || !isLocal) return json(res, 404, { error: "not found" });
      if (arg === "p2p") {
        // Active P2P sessions on the control client (stream clients are separate instances).
        const sessions = [...(ctx.eufy.getP2pSessions?.() ?? new Map())].map(([sn, s]) => ({
          station: sn, connected: s.connected === true, peer: s.connectAddress?.host, hasLevel2: s.hasLevel2Key,
        }));
        return json(res, 200, { sessions });
      }
      if (arg === "drop") {
        // Tear down a station's P2P session(s) (control + media) so an experiment can run as the sole client.
        const sn = url.searchParams.get("sn");
        if (!sn) return json(res, 400, { error: "?sn=" });
        const dropped = await ctx.sdk.dropStreamClient?.(sn, sn);
        return json(res, 200, { sn, dropped });
      }
      if (arg === "motion") {
        // Inject a motion event, so a motion wall can be exercised without waiting for something to walk
        // past a camera. Takes the same path as a real event: broadcast to every client, and a hold for
        // an on_motion camera.
        const sn = url.searchParams.get("sn");
        const cam = ctx.getCamera?.(sn);
        if (!cam?.enabled) return json(res, 404, { error: "unknown or disabled camera" });
        // ?still=1 claims a thumbnail exists, to exercise a wall's snapshot path before any camera has
        // actually pushed one. Without it this reports the truth, like a real event.
        const still = url.searchParams.get("still") === "1" || Boolean(await ctx.sdk.snapshotStored?.(sn).catch(() => undefined));
        ctx.broadcastEvent?.({ type: "motion", sn, event: "motion", still, simulated: true });
        const until = cam.mode === "on_motion" ? ctx.holds.hold(sn, "motion", cam.holdSeconds) : 0;
        return json(res, 200, { sn, mode: cam.mode, heldForMs: until ? until - Date.now() : 0 });
      }
      if (arg === "lan") {
        return json(res, 200, {
          force: ctx.cfg.lan.force,
          stations: ctx.lanUpgrade?.status?.() ?? {},
          // Whether this host can send UDP to each station at all — the first thing to check when every
          // camera times out at once. See src/lan-preflight.mjs.
          preflight: ctx.lanPreflight ?? [],
        });
      }
      return json(res, 404, { error: "debug: unknown probe" });
    }

    if (!flags.ready) return json(res, 503, { error: "not authenticated", auth: ctx.authStatus() });

    if (url.pathname === "/api/cameras") return json(res, 200, ctx.listCameras().map((c) => ctx.apiShape(c, host)));

    // The last thumbnail the SDK retained for a camera. Served so a tile can show something real while
    // its camera is asleep or still waking, instead of a black rectangle. Never wakes the camera.
    if (kind === "snapshot" && arg) {
      const cam = ctx.getCamera?.(arg);
      if (!cam || !cam.enabled) return json(res, 404, { error: "unknown or disabled camera" });
      const jpeg = await ctx.sdk.snapshotStored(arg);
      if (!jpeg) return json(res, 404, { error: "no retained snapshot yet for this camera" });
      res.writeHead(200, {
        "content-type": "image/jpeg",
        "content-length": jpeg.length,
        // It changes whenever the camera pushes a new thumbnail, and a stale still on a wall is worse
        // than a re-fetch.
        "cache-control": "no-cache",
      });
      return res.end(jpeg);
    }

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
