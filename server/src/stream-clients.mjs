// Session-per-streaming-camera — adapted from mega-yfue/ha-eufy-sdk-bridge `streams.mjs`
// (https://github.com/mega-yfue/ha-eufy-sdk-bridge, Apache-2.0, commit in vendor/ha-bridge/VENDOR.md).
// Not synced by scripts/sync-upstream.sh: re-diff by hand against upstream when bumping the SHA.
//
// Why a copy instead of the vendored file: the wall needs two things upstream does not —
//  1. `localAddresses` (cfg.lan.stationAddresses) on every per-camera client, exactly like the control
//     client in sdk-adapter.mjs, so force-LAN has a fighting chance on the first lookup;
//  2. an `onClient` hook so the LAN guard is attached the moment a client exists (pins.mjs opens the
//     P2P session before the stream manager ever sees the client);
// plus de-duplication of concurrent `streamClientFor(sn)` calls (pins + ensureWarm race at boot).
//
// Upstream rationale, kept verbatim: the SDK keeps ONE P2P session per station, and the HomeBase tags
// every inbound media frame channel 0 regardless of which camera was started — so two cameras streamed
// through one session arrive byte-identical. A separate EufyMega instance per streaming camera means a
// separate session. These clients share the session FILE, so they hydrate the same token instead of
// logging in again — eufy permits one active login per account, and a second login kicks the first.
import { EufyMega as SdkEufyMega, FileSessionStore, LoginStatus } from "@mega-yfue/eufy-sdk";

/**
 * @param {object} o
 * @param {object} o.cfg          bridge config (email/password/country/session/lan.stationAddresses)
 * @param {Function} [o.EufyMega] constructor override (tests)
 * @param {Function} [o.onClient] (client, sn) called once per created client, before login
 */
export function createStreamClients({ cfg, EufyMega = SdkEufyMega, onClient } = {}) {
  const clients = new Map(); // sn -> EufyMega (hydrated)
  const pending = new Map(); // sn -> Promise<EufyMega> (login in flight)

  function options() {
    const local = cfg.lan?.stationAddresses ?? {};
    return {
      email: cfg.email,
      password: cfg.password,
      countryCode: cfg.country,
      store: new FileSessionStore(cfg.session), // shared session file → hydrate, no fresh login
      localAddresses: Object.keys(local).length ? local : undefined,
    };
  }

  /** Get (or lazily create + hydrate) the dedicated stream client for a camera. Concurrent calls share one login. */
  function streamClientFor(sn) {
    const ready = clients.get(sn);
    if (ready) return Promise.resolve(ready);
    let p = pending.get(sn);
    if (p) return p;
    p = (async () => {
      const client = new EufyMega(options());
      client.on("error", (e) => console.error(`[bridge] stream(${sn}) sdk error: ${e?.message ?? e}`));
      onClient?.(client, sn);
      const result = await client.login();
      if (result.status !== LoginStatus.Ok) throw new Error(`stream client for ${sn} could not hydrate session (${result.status})`);
      clients.set(sn, client);
      return client;
    })().finally(() => pending.delete(sn));
    pending.set(sn, p);
    return p;
  }

  /** Tear down every stream client (on shutdown). */
  async function closeStreamClients() {
    await Promise.all([...clients.values()].map((c) => c.disconnect?.().catch(() => {})));
    clients.clear();
  }

  return { streamClientFor, closeStreamClients };
}
