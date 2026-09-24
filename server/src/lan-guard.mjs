// The SDK's lanOnly option rejects non-private peers when a station is pinned to LAN. This guard also
// checks the configured CIDR on connected control and media sessions, closing an out-of-range peer and
// marking the camera blocked until the stream manager can retry it.

export function inCidr(ip, cidr) {
  const [net, bitsStr] = cidr.split("/");
  const bits = Number(bitsStr);
  const toInt = (s) => {
    const p = s.split(".").map(Number);
    if (p.length !== 4 || p.some((n) => !Number.isInteger(n) || n < 0 || n > 255)) return null;
    return ((p[0] << 24) | (p[1] << 16) | (p[2] << 8) | p[3]) >>> 0;
  };
  const a = toInt(ip), n = toInt(net);
  if (a == null || n == null || !(bits >= 0 && bits <= 32)) return false;
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
  return (a & mask) === (n & mask);
}

export function createLanGuard(ctx) {
  const { cfg, state } = ctx;
  const attached = new WeakSet();
  const watchedMedia = new WeakSet();
  const seen = new Set(); // label:station:host already announced

  /** Decide for one connected session. Returns "ok" | "blocked" | "unknown". */
  async function checkSession(client, stationSn, label) {
    try {
      if (!cfg.lan.cidr) return "ok";
      const host = ctx.sdk.sessionPeerHost(client, stationSn);
      if (!host) {
        console.warn(`[bridge] ${label}: P2P connected to ${stationSn} but peer address unknown — cannot verify LAN path`);
        return "unknown";
      }
      if (inCidr(host, cfg.lan.cidr)) {
        if (state.blocked.delete(label)) console.log(`[bridge] ${label}: LAN path restored via ${host} — unblocked`);
        else if (!seen.has(`${label}:${stationSn}:${host}`)) console.log(`[bridge] ${label}: P2P peer ${host} for ${stationSn} (LAN)`);
        seen.add(`${label}:${stationSn}:${host}`);
        ctx.lanUpgrade?.onPeer?.(stationSn, "lan", host);
        return "ok";
      }
      if (!(ctx.lanUpgrade?.isForced?.(stationSn) ?? cfg.lan.force)) {
        console.warn(`[bridge] ${label}: P2P peer ${host} is outside ${cfg.lan.cidr} (relay/WAN path allowed)`);
        state.blocked.delete(label);
        ctx.lanUpgrade?.onPeer?.(stationSn, "wan", host);
        return "ok";
      }
      const reason = `wan-path ${host}`;
      state.blocked.set(label, reason);
      console.error(`[bridge] ${label}: P2P peer ${host} is outside ${cfg.lan.cidr} — closing session (force-LAN)`);
      try {
        await ctx.sdk.closeSession(client, stationSn);
      } catch (e) {
        console.error(`[bridge] ${label}: close after WAN detect failed: ${e?.message ?? e}`);
      }
      return "blocked";
    } catch (e) {
      console.error(`[bridge] ${label}: LAN check failed: ${e?.message ?? e}`);
      return "unknown";
    }
  }

  /** Check the station's control and per-camera media sessions, which do not emit p2pConnect. */
  async function checkMediaSessions(client, stationSn, label) {
    try {
      if (!cfg.lan.cidr) return "ok";
      const sessions = client.getP2pSessions?.();
      let observed = false;
      let outside = false;
      for (const [key, session] of sessions ?? []) {
        if (key !== stationSn && !String(key).startsWith(`${stationSn}#live:`)) continue;
        const host = ctx.sdk.sessionPeerHost(client, key);
        if (!host) {
          if (session && typeof session.once === "function" && !watchedMedia.has(session)) {
            watchedMedia.add(session);
            session.once("connect", () => void checkMediaSessions(client, stationSn, label));
          }
          continue;
        }
        observed = true;
        if (inCidr(host, cfg.lan.cidr)) continue;
        outside = true;
        if (!(ctx.lanUpgrade?.isForced?.(stationSn) ?? cfg.lan.force)) continue;
        state.blocked.set(label, `wan-path ${host}`);
        console.error(`[bridge] ${label}: P2P media peer ${host} is outside ${cfg.lan.cidr} — closing ${key}`);
        try {
          await ctx.sdk.closeSession(client, key);
        } catch (e) {
          console.error(`[bridge] ${label}: close after media WAN detect failed: ${e?.message ?? e}`);
        }
        return "blocked";
      }
      if (!observed) return "unknown";
      if (state.blocked.delete(label)) console.log(`[bridge] ${label}: LAN media path restored — unblocked`);
      ctx.lanUpgrade?.onPeer?.(stationSn, outside ? "wan" : "lan");
      return "ok";
    } catch (e) {
      console.error(`[bridge] ${label}: media LAN check failed: ${e?.message ?? e}`);
      return "unknown";
    }
  }

  /**
   * Subscribe once per EufyMega client (the control client and each per-camera stream client), and
   * judge any session that connected before we were listening (pins can open it before the stream).
   */
  function attachLanGuard(client, label) {
    if (!cfg.lan.cidr || attached.has(client)) return;
    attached.add(client);
    client.on("p2pConnect", (stationSn) => void checkSession(client, stationSn, label));
    const existing = typeof client.getP2pSessions === "function" ? client.getP2pSessions() : undefined;
    for (const key of existing?.keys?.() ?? []) {
      if (String(key).includes("#live:")) {
        void checkMediaSessions(client, String(key).split("#live:")[0], label);
        continue;
      }
      // A session still mid-handshake has no peer yet; its own p2pConnect will bring it here.
      if (ctx.sdk.sessionConnecting?.(client, key)) continue;
      void checkSession(client, key, label);
    }
  }

  const isBlocked = (sn) => state.blocked.has(sn);

  return { attachLanGuard, checkSession, checkMediaSessions, isBlocked };
}
