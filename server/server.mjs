// eufy-wall-bridge — wiring only. See docs/superpowers/specs/2026-09-18-eufy-wall-design.md.
//   HTTP :3000/stream/<sn>   Annex-B H.264 (go2rtc pulls this)     rtsp://host:8554/<sn>  (go2rtc)
//   HTTP :3000/api/cameras   camera list for display clients       /healthz  /auth/*
import http from "node:http";
import fs from "node:fs";
// Load local.env (git-ignored: EUFY_EMAIL/PASSWORD/COUNTRY) if present, so `node server.mjs` needs no
// wrapper to see the secrets. Env already set by the shell/systemd wins — loadEnvFile does not overwrite.
for (const f of [process.env.BRIDGE_ENV, "./local.env"]) {
  if (f && fs.existsSync(f)) { try { process.loadEnvFile(f); } catch {} }
}
// TEST toggle: a `.no-sweep` marker file disables the SDK local-port sweep so the beta SDK's native
// cloud-brokered per-camera connect can be evaluated on its own. Remove the file to restore the sweep.
if (fs.existsSync("./.no-sweep")) { globalThis.__ewNoSweep = true; console.log("[bridge] .no-sweep present — SDK port sweep DISABLED (testing native connect)"); }
import { loadConfig } from "./src/config.mjs";
import { createState } from "./src/state.mjs";
import { createSdk } from "./src/sdk-adapter.mjs";
import { createCameras } from "./src/cameras.mjs";
import { createPins } from "./src/pins.mjs";
import { createStreamManager } from "./src/stream-manager.mjs";
import { createLanGuard } from "./src/lan-guard.mjs";
import { createGo2rtc } from "./src/go2rtc.mjs";
import { createHttpHandler } from "./src/http.mjs";
import { installRecoveryRepin } from "./src/recovery.mjs";
import { createAuth } from "./src/vendor/ha-bridge/auth.mjs";
import { createWatchdog } from "./src/vendor/ha-bridge/watchdog.mjs";

const { cfg, DEBUG } = loadConfig();
fs.mkdirSync(cfg.dataDir, { recursive: true });

const state = createState();
const hooks = {};
const { eufy, sdk } = createSdk({ cfg, DEBUG, hooks });
const ctx = { cfg, DEBUG, eufy, sdk, state, SCHEMA_VERSION: 1, PUSH_STALL_MS: 15 * 60_000 };

// Vendored auth/watchdog broadcast auth changes to "clients"; we have none in phase 1 → log.
ctx.broadcast = (evt) => console.log(`[bridge] event ${JSON.stringify(evt)}`);

Object.assign(ctx, createCameras(ctx), createPins(ctx), createLanGuard(ctx), createStreamManager(ctx), createGo2rtc(ctx), createAuth(ctx), createWatchdog(ctx));
// Guard every per-camera client from the moment it exists: pins.mjs opens its P2P session (and fires
// p2pConnect) before the stream manager ever sees it.
hooks.onStreamClient = (client, sn) => ctx.attachLanGuard(client, sn);
installRecoveryRepin(ctx); // pins re-applied after watchdog / kicked-session re-logins

/** Runs once after the first successful login (re-auth calls it again and it returns immediately). */
ctx.completeBoot = async function completeBoot() {
  const { flags, timers } = state;
  if (flags.ready || flags.booting) return;
  flags.booting = true;
  try {
    eufy.on("deviceState", ctx.bumpActivity);
    const cams = await ctx.refreshCameras();
    const enabled = cams.filter((c) => c.enabled);
    console.log(`[bridge] cameras: ${cams.map((c) => `${c.sn}(${c.name}${c.enabled ? "" : ", off"}${c.isDual ? ", dual" : ""})`).join(", ")}`);
    ctx.attachLanGuard(eufy, "control");
    await ctx.applyAllPins();
    await ctx.writeGo2rtc();
    ctx.startGo2rtc();
    flags.ready = true;
    flags.lastActivity = Date.now();
    for (const c of enabled) void ctx.ensureWarm(c.sn);
    timers.stream ??= setInterval(() => ctx.streamTick(), 2000);
    timers.watchdog ??= setInterval(() => void ctx.watchdogTick(), 2 * 60_000);
    console.log(`[bridge] ready — ${enabled.length} always-on camera(s); RTSP at rtsp://<this-host>:8554/<sn>`);
  } finally {
    flags.booting = false;
  }
};

eufy.on("error", (e) => {
  console.error(`[bridge] sdk error: ${e?.message ?? e}`);
  if (e?.name === "SessionExpiredError") ctx.maybeRecoverSession();
});
eufy.on("pushConnect", () => { state.flags.pushConnected = true; state.flags.pushSince = Date.now(); });
eufy.on("pushDisconnect", () => { state.flags.pushConnected = false; state.flags.pushSince = Date.now(); });
// After a re-login (kicked session / watchdog recovery) the cameras may need their pins again.
eufy.on("p2pConnect", () => { if (state.flags.ready) void ctx.applyAllPins().catch(() => {}); });

const server = http.createServer(createHttpHandler(ctx));

async function main() {
  server.listen(cfg.port, cfg.host, () => console.log(`[bridge] listening on ${cfg.host}:${cfg.port}`));
  try {
    await ctx.applyLogin(await eufy.login());
  } catch (e) {
    console.error(`[bridge] login failed: ${e?.message ?? e} — POST /auth/retry to try again`);
  }
  if (!state.flags.ready) {
    const a = ctx.authStatus();
    console.log(`[bridge] auth required: ${a.state} — see docs/runbook-server.md (curl /auth/status, /auth/captcha, /auth/tfa?code=…)`);
  }
}

async function shutdown() {
  for (const t of Object.values(state.timers)) if (t) clearInterval(t);
  ctx.stopGo2rtc();
  await ctx.stopAll();
  await sdk.closeStreamClients();
  await eufy.disconnect?.().catch(() => {});
  process.exit(0);
}
process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
process.on("unhandledRejection", (e) => console.error(`[bridge] unhandled rejection: ${e?.stack ?? e}`));

main().catch((e) => { console.error("[bridge] fatal:", e); process.exit(1); });

// eufy-wall: bridge entry (see docs/). Restart marker.
