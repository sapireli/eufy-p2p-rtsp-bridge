// Can this host actually send UDP to the stations?
//
// P2P is outbound UDP. When the host refuses to send it — a firewall, a "block local network" privacy
// setting, a reject route — the failure looks nothing like its cause: the station still reaches US, so
// sessions log "connected" and CAM_ID arrives, and only the handshake we have to answer never completes.
// The bridge then reports "P2P connect timeout" and "did not provide its session key" forever, which reads
// like a camera or SDK fault and is neither.
//
// One datagram per station answers it. EHOSTUNREACH on a host whose address we were given means the packet
// never left this machine, and no amount of retrying will change that.
import dgram from "node:dgram";

const PROBE_PORT = 32108; // the eufy P2P port; nothing has to be listening for the send itself to be judged

/** Attempt one UDP send. Resolves to undefined on success, or the error code. */
export function probeSend(host, port = PROBE_PORT, timeoutMs = 2000) {
  return new Promise((resolve) => {
    const sock = dgram.createSocket("udp4");
    const done = (code) => {
      try {
        sock.close();
      } catch {
        /* already closed */
      }
      resolve(code);
    };
    const timer = setTimeout(() => done("ETIMEDOUT"), timeoutMs);
    timer.unref?.();
    sock.on("error", (err) => {
      clearTimeout(timer);
      done(err?.code ?? "EUNKNOWN");
    });
    sock.send(Buffer.from([0]), port, host, (err) => {
      clearTimeout(timer);
      done(err ? (err.code ?? "EUNKNOWN") : undefined);
    });
  });
}

/**
 * Explain a send failure in terms of what the operator can do about it. Deliberately specific: the whole
 * point is to not send someone hunting through camera and SDK logs for a fault that is on this host.
 */
export function explain(code, host) {
  if (!code) return undefined;
  if (code === "EHOSTUNREACH" || code === "ENETUNREACH") {
    return (
      `cannot send UDP to ${host} (${code}) — this host is refusing the packet, so P2P cannot complete ` +
      `even though the station can still reach us. Check: macOS Privacy & Security > Local Network (the ` +
      `terminal or service running the bridge must be enabled), a firewall or security product blocking ` +
      `local network traffic, and whether the route to this subnet is a reject route (netstat -rn).`
    );
  }
  if (code === "EACCES" || code === "EPERM") {
    return `not permitted to send UDP to ${host} (${code}) — a firewall or sandbox is blocking this process.`;
  }
  return `could not send UDP to ${host} (${code}).`;
}

/**
 * Probe every station whose LAN address we know. Returns one entry per station; `ok` false means P2P to
 * that station cannot work from this host until the cause is fixed.
 */
export async function preflightLan(stationAddresses, probe = probeSend) {
  const out = [];
  for (const [sn, addr] of Object.entries(stationAddresses ?? {})) {
    const host = String(addr).split(":")[0];
    const code = await probe(host);
    out.push({ sn, host, ok: !code, code, hint: explain(code, host) });
  }
  return out;
}

/** Run the probe and log anything actionable. Never throws: a diagnostic must not take the bridge down. */
export async function reportLanPreflight(stationAddresses, log = console.warn, probe = probeSend) {
  let results = [];
  try {
    results = await preflightLan(stationAddresses, probe);
  } catch {
    return [];
  }
  for (const r of results.filter((x) => !x.ok)) log(`[bridge] ${r.sn}: ${r.hint}`);
  return results;
}
