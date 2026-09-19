// The ONLY non-vendored file that imports @mega-yfue/eufy-sdk. Everything the bridge needs from the SDK
// is re-exposed here with stable names, so an SDK API change is a one-file edit (+ the contract test).
import { EufyMega, FileSessionStore, LoginStatus, ConsoleLogger, extractParamSets, codedGeometry } from "@mega-yfue/eufy-sdk";
import { createStreamClients } from "./stream-clients.mjs";

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

  const streamClients = createStreamClients({
    cfg,
    onClient: (client, sn) => hooks.onStreamClient?.(client, sn),
    logger: DEBUG ? new ConsoleLogger("debug") : undefined,
  });

  const sdk = {
    LoginStatus,
    extractParamSets,
    codedGeometry,

    /** Dedicated per-camera EufyMega (adapted upstream workaround: one P2P session per streaming camera). */
    streamClientFor: streamClients.streamClientFor,
    closeStreamClients: streamClients.closeStreamClients,

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
