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
    logger: DEBUG ? new ConsoleLogger("info") : undefined,
  });

  const streamClients = createStreamClients({ cfg, onClient: (client, sn) => hooks.onStreamClient?.(client, sn) });

  const sdk = {
    LoginStatus,
    extractParamSets,
    codedGeometry,

    /** Dedicated per-camera EufyMega (adapted upstream workaround: one P2P session per streaming camera). */
    streamClientFor: streamClients.streamClientFor,
    closeStreamClients: streamClients.closeStreamClients,

    /** Open the raw Annex-B Readable for a camera on the given client. */
    async openFeed(client, sn) {
      const cam = (await client.getDevice(sn)).camera?.();
      if (!cam?.openReadable) throw new Error(`${sn}: no live video (not a camera or openReadable unavailable)`);
      return cam.openReadable();
    },

    /** Manifest + power source for the /api shape. */
    async describe(sn) {
      const dev = await eufy.getDevice(sn);
      const m = dev.describe();
      return {
        sn: m.sn,
        name: m.name,
        model: m.model || m.modelName,
        modelName: m.modelName,
        isCamera: m.capabilities.includes("camera") || m.capabilities.includes("video"),
        battery: dev.has("battery"),
      };
    },

    /**
     * Raw SET_PAYLOAD (1350) sub-command on a device channel — the escape hatch for settings the SDK has
     * no capability for yet (dual-lens view mode 6243/2700). Uses the SDK's private commandSinkFor; the
     * contract test asserts it exists. Replace with a public capability once upstream ships one.
     */
    async sendSetPayload(client, sn, cmd, payload) {
      const sink = client.commandSinkFor(sn);
      await sink.dispatch({ kind: "set-payload", cmd, payload, mValue3: 0 });
    },

    /** The IP the P2P session for `stationSn` is talking to, or undefined if unknown/not connected. */
    sessionPeerHost(client, stationSn) {
      const s = client.getP2pSessions().get(stationSn);
      return s?.connectAddress?.host; // private field in TS, reachable at runtime; contract test guards
    },

    async closeSession(client, stationSn) {
      const s = client.getP2pSessions().get(stationSn);
      if (s) await s.close();
    },
  };
  return { eufy, sdk };
}
