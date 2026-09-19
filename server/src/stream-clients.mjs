// One streaming client per STATION — adapted from mega-yfue/ha-eufy-sdk-bridge `streams.mjs`
// (https://github.com/mega-yfue/ha-eufy-sdk-bridge, Apache-2.0, commit in vendor/ha-bridge/VENDOR.md).
// Not synced by scripts/sync-upstream.sh: re-diff by hand against upstream when bumping the SHA.
//
// Why a copy instead of the vendored file: the wall needs two things upstream does not —
//  1. `localAddresses` (cfg.lan.stationAddresses) on every client, exactly like the control client in
//     sdk-adapter.mjs, so force-LAN has a fighting chance on the first lookup;
//  2. an `onClient` hook so the LAN guard is attached the moment a client exists (pins.mjs opens the
//     P2P session before the stream manager ever sees the client);
// plus de-duplication of concurrent `streamClientFor()` calls (pins + ensureWarm race at boot).
//
// KEYED BY STATION, not by camera. The SDK keeps ONE P2P session per station and multiplexes cameras by
// channel: each LiveStream starts/stops/filters on its own device channel, and the keepalive contention
// settles once a camera's own frames flow (SDK-measured: two HomeBase-attached cameras held a 40 s
// simultaneous stream). An earlier revision used a session PER CAMERA on the belief that a HomeBase tags
// all media channel 0 — true in older stacks, obsolete in @mega-yfue/eufy-sdk — and that broke co-located
// cameras: two sessions to one HomeBase can't both win its single ephemeral hole-punch port, so the second
// camera timed out forever. A login hydrates every device on the account, so one station client can
// getDevice()/openReadable() any camera on that station. Clients share the session FILE, so they hydrate
// the same token instead of logging in again — eufy permits one active login per account.
import { EufyMega as SdkEufyMega, FileSessionStore, LoginStatus } from "@mega-yfue/eufy-sdk";

/**
 * @param {object} o
 * @param {object} o.cfg          bridge config (email/password/country/session/lan.stationAddresses)
 * @param {Function} [o.EufyMega] constructor override (tests)
 * @param {Function} [o.onClient] (client, stationSn) called once per created client, before login
 */
export function createStreamClients({ cfg, EufyMega = SdkEufyMega, onClient, logger } = {}) {
  const clients = new Map(); // stationSn -> EufyMega (hydrated)
  const pending = new Map(); // stationSn -> Promise<EufyMega> (login in flight)

  function options() {
    const local = cfg.lan?.stationAddresses ?? {};
    return {
      email: cfg.email,
      password: cfg.password,
      countryCode: cfg.country,
      store: new FileSessionStore(cfg.session), // shared session file → hydrate, no fresh login
      localAddresses: Object.keys(local).length ? local : undefined,
      logger, // debug logger (when BRIDGE_DEBUG) — the level-2/gateway negotiation lives on these clients
    };
  }

  /**
   * Get (or lazily create + hydrate) the shared stream client for a camera's STATION. Every camera on one
   * HomeBase resolves to the same client (one P2P session, multiplexed by channel). Concurrent calls for
   * the same station share one login. `stationSn` defaults to `sn` for standalone cameras (own station).
   */
  function streamClientFor(sn, stationSn = sn) {
    const key = stationSn || sn;
    const ready = clients.get(key);
    if (ready) return Promise.resolve(ready);
    let p = pending.get(key);
    if (p) return p;
    p = (async () => {
      const client = new EufyMega(options());
      client.on("error", (e) => console.error(`[bridge] station(${key}) sdk error: ${e?.message ?? e}`));
      onClient?.(client, key);
      const result = await client.login();
      if (result.status !== LoginStatus.Ok) throw new Error(`stream client for station ${key} could not hydrate session (${result.status})`);
      clients.set(key, client);
      return client;
    })().finally(() => pending.delete(key));
    pending.set(key, p);
    return p;
  }

  /** Tear down every stream client (on shutdown). */
  async function closeStreamClients() {
    await Promise.all([...clients.values()].map((c) => c.disconnect?.().catch(() => {})));
    clients.clear();
  }

  return { streamClientFor, closeStreamClients };
}
