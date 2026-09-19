// The ONLY non-vendored file that imports @mega-yfue/eufy-sdk. Everything the bridge needs from the SDK
// is re-exposed here with stable names, so an SDK API change is a one-file edit (+ the contract test).
import { EufyMega, FileSessionStore, LoginStatus, ConsoleLogger, extractParamSets, codedGeometry } from "@mega-yfue/eufy-sdk";

/**
 * @param {object} o
 * @param {object} [o.hooks] late-bound callbacks read at call time: `onStreamClient(client, sn)` is
 *   invoked for every per-camera client as soon as it is constructed (server.mjs points it at the LAN guard).
 */
export function createSdk({ cfg, DEBUG, hooks = {} }) {
  const eufy = new EufyMega({
    email: cfg.email,
    password: cfg.password,
    countryCode: cfg.country,
    store: new FileSessionStore(cfg.session),
    pollMs: cfg.pollMs,
    prewarmEvents: [], // no speculative P2P warm-ups on push events (Phase 1 streams are always warm anyway)
    localAddresses: Object.keys(cfg.lan.stationAddresses).length ? cfg.lan.stationAddresses : undefined,
    logger: DEBUG ? new ConsoleLogger("debug") : undefined,
  });

  let lastProbe = null; // most recent /debug/probe-sessions result, for polling over loopback

  const sdk = {
    LoginStatus,
    extractParamSets,
    codedGeometry,

    /**
     * SINGLE client for control + all streams. The beta SDK opens a dedicated per-camera media session
     * ("<stationSn>#live:<channel>") on demand, so one logged-in client streams every camera — including
     * multiple cameras off one HomeBase concurrently — with no extra logins. This replaces the old
     * per-camera-client workaround (streams.mjs), which predated the SDK's per-camera sessions and caused
     * self-contention: multiple logins on one account/identity displace each other's cloud session, which
     * silently breaks the DSK/cipher lookups a P2P connect needs. One client, one identity, no contention.
     */
    streamClientFor: async () => eufy,
    /** Tear down the station's P2P session (control + its media sessions) so the next open re-lookups a
     *  fresh port. Returns whether a session existed to close. */
    async dropStreamClient(sn, stationSn) {
      const key = stationSn ?? sn;
      const sessions = eufy.getP2pSessions?.() ?? new Map();
      let closed = false;
      for (const [k, s] of sessions) {
        if (k === key || String(k).startsWith(`${key}#`)) { await s.close?.().catch?.(() => {}); closed = true; }
      }
      return closed;
    },
    async closeStreamClients() { /* single client is owned by createSdk; disconnected on shutdown elsewhere */ },

    /**
     * DEBUG probe: open N throwaway P2P sessions (one fresh client each) to the given cameras at once and
     * report which deliver bytes. Answers "can this host hold multiple concurrent sessions to one HomeBase?"
     * without touching the live bridge. Each client logs in against the shared session file (hydrate, no
     * re-login) and is torn down after `seconds`. Returns per-sn {connected,bytes,error}.
     */
    async probeSessions(sns, { seconds = 30, powered = true } = {}) {
      const local = cfg.lan?.stationAddresses ?? {};
      const mkOpts = () => ({
        email: cfg.email, password: cfg.password, countryCode: cfg.country,
        store: new FileSessionStore(cfg.session),
        localAddresses: Object.keys(local).length ? local : undefined,
        logger: DEBUG ? new ConsoleLogger("debug") : undefined,
      });
      const results = Object.fromEntries(sns.map((sn) => [sn, { connected: false, bytes: 0, error: null }]));
      const clients = [];
      console.log(`[probe] opening ${sns.length} concurrent session(s): ${sns.join(", ")} (${seconds}s)`);
      await Promise.all(sns.map(async (sn) => {
        const client = new EufyMega(mkOpts());
        clients.push(client);
        client.on("error", (e) => { results[sn].error ??= String(e?.message ?? e); });
        try {
          const r = await client.login();
          if (r.status !== LoginStatus.Ok) throw new Error(`login ${r.status}`);
          const cam = (await client.getDevice(sn)).camera?.();
          if (!cam?.openReadable) throw new Error("no camera/openReadable");
          const feed = await cam.openReadable(powered ? { powered: "wired" } : {});
          feed.on("data", (c) => {
            if (!results[sn].connected) console.log(`[probe] ${sn}: FIRST BYTES (concurrent session connected)`);
            results[sn].connected = true; results[sn].bytes += c.length;
          });
          feed.on("error", (e) => { results[sn].error ??= String(e?.message ?? e); });
        } catch (e) {
          results[sn].error ??= String(e?.message ?? e);
          console.log(`[probe] ${sn}: open failed: ${results[sn].error}`);
        }
      }));
      await new Promise((r) => setTimeout(r, seconds * 1000));
      await Promise.all(clients.map((c) => c.disconnect?.().catch(() => {})));
      lastProbe = { at: new Date().toISOString(), seconds, results };
      console.log(`[probe] RESULT: ${sns.map((sn) => `${sn}=${results[sn].connected ? `STREAMING(${results[sn].bytes}B)` : `no(${results[sn].error ?? "no bytes"})`}`).join("  ")}`);
      return results;
    },
    getLastProbe() { return lastProbe; },

    /**
     * Open the raw Annex-B Readable for a camera on the given client.
     *
     * `powered` (the bridge's evidence-based verdict from cameras.mjs) is forwarded as the SDK's live
     * `powered` hint. The SDK otherwise infers it from the `battery` capability, which it grants to some
     * mains models (Floodlight E340/2 Pro, doorbells), so it arms a ~45 s battery budget and tears the
     * stream down mid-flight — an always-on wired camera then flaps every budget cycle. Telling it "wired"
     * disables that budget; a real battery camera keeps the SDK default so its budget still applies.
     */
    async openFeed(client, sn, { powered } = {}) {
      const cam = (await client.getDevice(sn)).camera?.();
      if (!cam?.openReadable) throw new Error(`${sn}: no live video (not a camera or openReadable unavailable)`);
      return cam.openReadable(powered ? { powered: "wired" } : {});
    },

    /** Manifest + power source for the /api shape. */
    async describe(sn) {
      const dev = await eufy.getDevice(sn);
      const m = dev.describe();
      const props = dev.getProperties?.() ?? {};
      return {
        sn: m.sn,
        name: m.name,
        model: m.model || m.modelName,
        modelName: m.modelName,
        isCamera: m.capabilities.includes("camera") || m.capabilities.includes("video"),
        battery: dev.has("battery"),
        // Evidence for cameras.mjs' power decision: `battery` above is only a *capability* flag (the SDK
        // grants it to some mains models, e.g. Floodlight E340/2 Pro); a reported level + charging state
        // tell the real story.
        batteryLevel: props.battery?.value,
        charging: props.charging?.value,
      };
    },

    /**
     * Raw SET_PAYLOAD (1350) sub-command on a device channel — the escape hatch for settings the SDK has
     * no capability for yet (dual-lens view mode 6243/2700). Uses the SDK's private commandContext (for
     * the device's HomeBase channel — a SET_PAYLOAD without it goes to channel 0, i.e. the wrong device
     * on a multi-camera station) and commandSinkFor; the contract test asserts both exist. Replace with
     * a public capability once upstream ships one.
     */
    async sendSetPayload(client, sn, cmd, payload) {
      const { channel } = await client.commandContext(sn);
      const sink = client.commandSinkFor(sn);
      await sink.dispatch({ kind: "set-payload", cmd, payload, channel, mValue3: 0 });
    },

    /** The IP the P2P session for `stationSn` is talking to, or undefined if unknown/not connected. */
    sessionPeerHost(client, stationSn) {
      const s = client.getP2pSessions().get(stationSn);
      return s?.connectAddress?.host; // private field in TS, reachable at runtime; contract test guards
    },

    /** True while a station's session exists but has not finished its handshake (no peer yet). */
    sessionConnecting(client, stationSn) {
      const s = client.getP2pSessions().get(stationSn);
      return Boolean(s) && s.connected !== true;
    },

    async closeSession(client, stationSn) {
      const s = client.getP2pSessions().get(stationSn);
      if (s) await s.close();
    },
  };

  // NOTE on local-port discovery: HomeBase 3's real local P2P port is a per-session NAT mapping the
  // cloud never exposes correctly, and it is bound to whichever socket first reaches it. So discovery
  // MUST happen on the connecting session's own socket — it lives in the SDK P2P-session patch (a
  // CHECK_CAM sweep, see patches/ + docs/hb3-local-port.md), not here. A host-side pre-scan would find
  // a port for the WRONG socket. This module only needs to supply the station's LAN *host*, which comes
  // from cfg.lan.station_addresses; the session sweep finds the port itself, fresh, every session —
  // which is also why there is nothing to cache and nothing to go stale.

  return { eufy, sdk };
}
