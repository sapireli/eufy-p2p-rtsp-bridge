// The ONLY non-vendored file that imports @mega-yfue/eufy-sdk. Everything the bridge needs from the SDK
// is re-exposed here with stable names, so an SDK API change is a one-file edit (+ the contract test).
import { EufyMega, FileSessionStore, LoginStatus, ConsoleLogger, extractParamSets, codedGeometry, cameraPowerTier } from "@mega-yfue/eufy-sdk";

/**
 * @param {object} o
 * @param {object} [o.hooks] late-bound callbacks read at call time: `onStreamClient(client, sn)` is
 *   invoked for every per-camera client as soon as it is constructed (server.mjs points it at the LAN guard).
 */
export function createSdk({ cfg, DEBUG, hooks = {} }) {
  const powerOverrides = Object.fromEntries(
    Object.entries(cfg.cameras ?? {})
      .filter(([, camera]) => camera.powerOverride && camera.powerOverride !== "auto")
      .map(([sn, camera]) => [sn, camera.powerOverride]),
  );
  const eufy = new EufyMega({
    email: cfg.email,
    password: cfg.password,
    countryCode: cfg.country,
    store: new FileSessionStore(cfg.session),
    pollMs: cfg.pollMs,
    // No speculative P2P warm-ups on push events. The same push that would trigger one is the push our
    // own motion handler takes a hold on, and a hold opens the session immediately — so for an on_motion
    // camera pre-warm races our own open and wins nothing. For any other mode it would spend a battery
    // camera's radio opening a session nothing is going to stream.
    prewarmEvents: [],
    powerOverrides,
    localAddresses: Object.keys(cfg.lan.stationAddresses).length ? cfg.lan.stationAddresses : undefined,
    // Queried for every station and media session. The SDK rejects non-private IPv4 peers while this
    // station is pinned; lan-guard.mjs checks the configured CIDR on control and media sessions.
    lanOnly: (stationSn) => Boolean(hooks.lanOnlyForStation?.(stationSn)),
    logger: DEBUG ? new ConsoleLogger("debug") : undefined,
  });

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

    /** Open the raw Annex-B Readable for a camera on the given client. */
    async openFeed(client, sn, { standalone } = {}) {
      const cam = (await client.getDevice(sn)).camera?.();
      if (!cam?.openReadable) throw new Error(`${sn}: no live video (not a camera or openReadable unavailable)`);
      // warmTimeoutMs: see cfg.stall.warmTimeoutMs — the app waits out long dead spots rather than rebuilding.
      // warmRetryMs is deliberately slower for a standalone camera: it has far fewer session slots than a
      // HomeBase, and re-issuing the start every 2s for the whole deadline exhausts it rather than waking
      // it — one here stayed frozen until every client stopped asking.
      const opts = {
        warmTimeoutMs: cfg.stall.warmTimeoutMs,
        warmRetryMs: standalone ? cfg.stall.standaloneWarmRetryMs : cfg.stall.warmRetryMs,
      };
      return cam.openReadable(opts);
    },

    /**
     * The most recent push thumbnail the SDK has retained for a camera, as JPEG bytes.
     *
     * Reads from memory only: it performs no network or P2P work and CANNOT wake a sleeping camera,
     * which is what makes it safe to show on a wall for a battery camera that is asleep. Returns
     * undefined when nothing has been retained yet (no event since the bridge started) or the camera
     * does not report thumbnails.
     */
    async snapshotStored(sn) {
      const cam = (await eufy.getDevice(sn)).camera?.();
      if (!cam?.snapshotStored) return undefined;
      try {
        const shot = await cam.snapshotStored();
        // The SDK returns either raw bytes or {jpeg}; accept both rather than guessing.
        const bytes = shot?.jpeg ?? shot;
        return Buffer.isBuffer(bytes) ? bytes : undefined;
      } catch {
        return undefined; // nothing retained is the normal case, not an error worth logging per request
      }
    },

    /** Manifest + power source for the /api shape. */
    async describe(sn) {
      const dev = await eufy.getDevice(sn);
      const m = dev.describe();
      const powerOverride = powerOverrides[sn] ?? "auto";
      const automaticTier = cameraPowerTier(m.model, new Set(m.capabilities));
      return {
        sn: m.sn,
        name: m.name,
        model: m.model || m.modelName,
        modelName: m.modelName,
        isCamera: m.capabilities.includes("camera") || m.capabilities.includes("video"),
        battery: dev.has("battery"),
        powerOverride,
        powerTier: powerOverride === "auto" ? automaticTier : powerOverride === "always-on" ? "wired" : "battery",
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

  return { eufy, sdk };
}
