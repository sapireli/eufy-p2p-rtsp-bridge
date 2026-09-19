// Force-LAN: the SDK always races a LAN lookup against the PPCS cloud lookup and keeps whichever peer
// answers first. We can't stop the race (no local-only option in 0.1.1 — upstream PR pending), so we
// inspect the winner on every p2pConnect and refuse a WAN/relay peer: close the session, mark the camera
// blocked (visible in /healthz + /api/cameras), and let the stream manager's backoff try again — the
// station usually answers locally on the next attempt.

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
        return "ok";
      }
      if (!cfg.lan.force) {
        console.warn(`[bridge] ${label}: P2P peer ${host} is outside ${cfg.lan.cidr} (lan.force=false, allowing)`);
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

  /**
   * Subscribe once per EufyMega client (the control client and each per-camera stream client), and
   * judge any session that connected before we were listening (pins can open it before the stream).
   */
  function attachLanGuard(client, label) {
    if (!cfg.lan.cidr || attached.has(client)) return;
    attached.add(client);
    client.on("p2pConnect", (stationSn) => void checkSession(client, stationSn, label));
    const existing = typeof client.getP2pSessions === "function" ? client.getP2pSessions() : undefined;
    for (const stationSn of existing?.keys?.() ?? []) {
      // A session still mid-handshake has no peer yet; its own p2pConnect will bring it here.
      if (ctx.sdk.sessionConnecting?.(client, stationSn)) continue;
      void checkSession(client, stationSn, label);
    }
  }

  const isBlocked = (sn) => state.blocked.has(sn);

  return { attachLanGuard, checkSession, isBlocked };
}
