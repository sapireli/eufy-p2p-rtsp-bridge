// The ONLY non-vendored file that imports @mega-yfue/eufy-sdk. Everything the bridge needs from the SDK
// is re-exposed here with stable names, so an SDK API change is a one-file edit (+ the contract test).
import { EufyMega, FileSessionStore, LoginStatus, ConsoleLogger, extractParamSets, codedGeometry } from "@mega-yfue/eufy-sdk";
import dgram from "node:dgram";
import os from "node:os";

// p2p_did "PREFIX-NUMBER-SUFFIX" -> 20 bytes [prefix:8][number:u32be][suffix:8] (matches the SDK's own encoder).
function p2pDidToBuffer(did) {
  const [pre, num, suf] = String(did).split("-");
  const b = Buffer.alloc(20);
  Buffer.from(pre ?? "").copy(b, 0, 0, 8);
  b.writeUInt32BE(Number.parseInt(num ?? "0", 10) >>> 0, 8);
  Buffer.from(suf ?? "").copy(b, 12, 0, 8);
  return b;
}
// Global PPCS master pool observed in the app packet capture (used when the station's own p2p_conn list
// isn't handy); the app sprays LOOKUP_WITH_KEY across all of these.
const CAPTURE_CLOUD_POOL = ["18.211.176.129", "3.13.12.246", "184.72.36.250", "54.153.101.7", "34.235.4.153", "18.223.127.200", "13.56.9.112", "3.23.201.30", "54.164.145.238"];

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
      // warmTimeoutMs: see cfg.stall.warmTimeoutMs — the app waits out long dead spots rather than rebuilding.
      const opts = { warmTimeoutMs: cfg.stall.warmTimeoutMs };
      return cam.openReadable(powered ? { powered: "wired", ...opts } : opts);
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

    /** Dump the exact P2P connect parameters the SDK holds for a station, to compare against a packet
     *  capture of the app: p2p_did, dsk key, the cloud lookup servers, and the pinned LAN host. */
    async stationParams(sn) {
      const sess = eufy.getP2pSessions?.().get(sn) ?? [...(eufy.getP2pSessions?.() ?? new Map())].find(([k]) => String(k).startsWith(`${sn}#`))?.[1];
      let dsk;
      try { dsk = (await eufy.mega.getDskKeys([sn]))[sn]?.dskKey; } catch (e) { dsk = `ERR ${e?.message ?? e}`; }
      return {
        sn,
        fromLiveSession: Boolean(sess),
        p2pDid: sess?.cfg?.p2pDid ?? null,
        sessionDsk: sess?.cfg?.dskKey ?? null,
        megaDsk: dsk,
        cloudAddresses: (sess?.cfg?.cloudAddresses ?? []).map((a) => `${a.host}:${a.port}`),
        selfAddress: sess?.selfAddress ? `${sess.selfAddress.host}:${sess.selfAddress.port}` : null,
        connectAddress: sess?.connectAddress ? `${sess.connectAddress.host}:${sess.connectAddress.port}` : null,
        lanHost: cfg.lan?.stationAddresses?.[sn],
      };
    },

    /**
     * APP-STYLE hole-punch experiment (runs in the LAN-permitted bridge process; DEBUG/loopback only).
     * Replicates what the eufy app does per the packet capture: open N sockets, ping the cloud pool
     * (f100) and register each socket's own LAN addr with LOOKUP_WITH_KEY (f126, app byte layout) on
     * ports 32100/32101/32102, then wait for the station to punch (f141 LOCAL_LOOKUP_RESP) to one of our
     * sockets, CHECK_CAM it, and report which socket got a CAM_ID (f142) and how fast. Proves whether the
     * multi-socket + wide-server recipe elicits the reliable ~300ms LAN punch the app gets.
     *
     * Overrides (didHex/dsk/cloud/lanIp) let a captured credential be replayed verbatim; otherwise the
     * SDK's own p2p_did/dsk/cloud for `sn` are used.
     */
    async appPunch({ sn, host, sockets = 8, ms = 4000, didHex, dsk, cloud, ports, eager = false, nocheck = false, legacy = false } = {}) {
      const sess = sn ? (eufy.getP2pSessions?.().get(sn) ?? [...(eufy.getP2pSessions?.() ?? new Map())].find(([k]) => String(k).startsWith(`${sn}#`))?.[1]) : null;
      const didBuf = didHex ? Buffer.from(didHex, "hex") : (sess?.cfg?.p2pDid ? p2pDidToBuffer(sess.cfg.p2pDid) : null);
      let dskKey = dsk || sess?.cfg?.dskKey;
      if (!dskKey && sn) { try { dskKey = (await eufy.mega.getDskKeys([sn]))[sn]?.dskKey; } catch {} }
      let cloudHosts = cloud;
      if (!cloudHosts?.length) cloudHosts = [...new Set([...(sess?.cfg?.cloudAddresses ?? []).map((a) => a.host), ...CAPTURE_CLOUD_POOL])];
      const cloudPorts = ports?.length ? ports : [32100, 32101, 32102];
      const lanIp = (() => {
        for (const list of Object.values(os.networkInterfaces())) for (const i of list ?? []) if (i.family === "IPv4" && !i.internal && i.address.startsWith(cfg.lan.cidr.split(".").slice(0, 3).join(".") + ".")) return i.address;
        return null;
      })();
      if (!didBuf || didBuf.length !== 20) return { error: "need a 20-byte p2p_did (didHex) or a resolvable sn" };
      if (!dskKey) return { error: "no dsk (pass dsk= or a logged-in sn)" };
      if (!lanIp) return { error: "could not find our LAN IPv4 in cfg.lan.cidr" };

      const ipRev = Buffer.from(lanIp.split(".").reverse().map((o) => Number(o)));
      const mkMsg = (h, payload = Buffer.alloc(0)) => { const b = Buffer.allocUnsafe(4); b[0] = h >> 8; b[1] = h & 255; b.writeUInt16BE(payload.length, 2); return Buffer.concat([b, payload]); };
      const splitter = legacy ? [0, 2] : [0, 0];      // SDK uses 00 02, app uses 00 00
      const version = legacy ? [2, 5, 1, 5] : [2, 5, 2, 2]; // SDK uses 02 05 01 05, app uses 02 05 02 02
      const f126 = (port) => { const pb = Buffer.allocUnsafe(2); pb.writeUInt16LE(port, 0); return mkMsg(0xf126, Buffer.concat([didBuf, Buffer.from(splitter), pb, ipRev, Buffer.alloc(8), Buffer.from(version), Buffer.from(dskKey), Buffer.from([0, 0, 0, 0])])); };
      const checkCam = mkMsg(0xf141, Buffer.concat([didBuf, Buffer.from([0, 0, 0])]));
      const F100 = mkMsg(0xf100);

      const result = { host, lanIp, cloudHosts, cloudPorts, sockets, eager, nocheck, legacy, dsk: dskKey, connected: null, punchMs: null, camMs: null, punches: [], events: [], rx: [] };
      const t0 = Date.now();
      const socks = [];
      const done = () => { for (const s of socks) try { s.close(); } catch {} };
      await new Promise((resolve) => {
        let finished = false;
        const finish = () => { if (finished) return; finished = true; done(); resolve(); };
        for (let i = 0; i < sockets; i++) {
          const sock = dgram.createSocket("udp4");
          socks.push(sock);
          sock.on("message", (m, r) => {
            const hdr = m.subarray(0, 2).toString("hex");
            // Every inbound datagram is evidence: cloud hello-replies (f101), broker answers (f140/f182/f121),
            // and the station punch. Keep a compact trace (first 60 entries).
            if (result.rx.length < 60) result.rx.push(`${Date.now() - t0}ms sock${sock.address().port} <- ${r.address}:${r.port} ${hdr} len=${m.length}${m.length > 4 && m.length <= 24 ? " " + m.subarray(4).toString("hex") : ""}`);
            if (hdr === "f140" && r.address !== host && m.length >= 20) {
              // LOOKUP_ADDR: [0002][port LE][ip reversed] — CHECK_CAM the advertised endpoint (+/-3), like the SDK.
              const port = m.readUInt16LE(6);
              const ip = [m[11], m[10], m[9], m[8]].join(".");
              if (!nocheck && ip !== "0.0.0.0" && !sock.__cc?.has?.(`${ip}:${port}`)) {
                (sock.__cc ??= new Set()).add(`${ip}:${port}`);
                result.events.push(`f140 -> ${ip}:${port} (CHECK_CAM +/-3) @${Date.now() - t0}ms sock ${sock.address().port}`);
                for (let p = port - 3; p <= port + 3; p++) if (p > 0) sock.send(checkCam, p, ip);
              }
              return;
            }
            if (hdr === "f101" && r.address !== host) {
              // App ordering: the master server acknowledged our hello — now register THIS socket with it.
              if (!sock.__reg) sock.__reg = new Set();
              if (!sock.__reg.has(r.address)) { sock.__reg.add(r.address); for (const p of cloudPorts) sock.send(f126(sock.address().port), p, r.address); }
              return;
            }
            if (r.address === host) {
              if (hdr === "f141") { // station punch (LOCAL_LOOKUP_RESP) — CHECK_CAM its source
                if (result.punchMs === null) result.punchMs = Date.now() - t0;
                result.punches.push({ from: `${r.address}:${r.port}`, atMs: Date.now() - t0, sockPort: sock.address().port });
                sock.send(checkCam, r.port, r.address);
              } else if (hdr === "f142") { // CAM_ID — direct LAN connection established
                if (result.camMs === null) { result.camMs = Date.now() - t0; result.connected = `${r.address}:${r.port}`; }
                result.events.push(`f142 from ${r.address}:${r.port} @${Date.now() - t0}ms sock ${sock.address().port}`);
                finish();
              }
            }
          });
          sock.bind(0, () => {
            const port = sock.address().port;
            const reg = f126(port);
            // App ordering: hello (f100) first; f126 goes out per-server when its f101 comes back (see onmessage).
            // `eager=1` also sends f126 immediately alongside the hello (the previous behaviour) for comparison.
            const spray = () => { if (finished) return; for (const h of cloudHosts) for (const p of cloudPorts) { sock.send(F100, p, h); if (eager) sock.send(reg, p, h); } };
            spray();
            sock.__t = setInterval(spray, 1000);
          });
        }
        setTimeout(finish, ms);
      });
      for (const s of socks) if (s.__t) clearInterval(s.__t);
      return result;
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
