# HomeBase 3 (T8030) local P2P — the hidden-port problem and our fix

## Symptom

Cameras behind a **HomeBase 3** never stream locally through `@mega-yfue/eufy-sdk`: every open ends in
`P2P connect timeout for T8030…`. Standalone cameras (their own station) work; HomeBase-attached ones
don't. The eufy phone app streams the same cameras fine over the LAN.

## Root cause (verified on the wire)

eufy P2P is ThroughTek PPCS. A client reaches a station by learning its `IP:port` and sending `CHECK_CAM`
(`0xf141`), to which the station replies `CAM_ID` (`0xf142`). Discovery is either a LAN broadcast
`LOCAL_LOOKUP` (`0xf130` → `LOCAL_LOOKUP_RESP` `0xf141`) or a cloud lookup that returns the station's
address.

On HomeBase 3, three things combine to break the SDK's local path:

1. **The cloud returns a NAT-translated port.** The cloud `LOOKUP_ADDR` gives e.g. `192.168.23.233:21762`
   — the station's *WAN-side* port. Sending `CHECK_CAM` there locally gets ICMP port-unreachable (`EPIPE`);
   nothing listens on `21762` on the LAN.
2. **HB3 ignores `LOCAL_LOOKUP` on 32108.** A direct/broadcast `LOCAL_LOOKUP` to the HomeBase gets no
   `LOCAL_LOOKUP_RESP`, so the SDK never learns the real local port. (Confirmed: 0 replies.)
3. **The real local port is ephemeral and per-session.** A protocol-aware scan (send `CHECK_CAM` to every
   UDP port and watch for `CAM_ID`) finds the station answering on **a different port every run** —
   observed `29136`, `13832`, `14423`, `20726`, `25214`, `28669`. It is a fresh NAT/hole-punch mapping per
   connection, **bound to the socket that reached it**.

A packet capture of the phone (via `rvictl` + `tcpdump -i rvi0`) shows the app succeeds via a **mutual,
cloud-brokered UDP hole-punch**: the cloud tells the station the client's predicted port and the *station
initiates back*, fanning ±3 across the client's port range. The SDK never implements that device-initiated
direction — it only tries client→station to the NAT'd port with a ±3 sweep that's thousands of ports off —
so HB3 is effectively unsupported for local streaming (matches upstream `ha-eufy-sdk-bridge` #39/#51 and
`fuatakgun/eufy_security` #473).

The SDK's `bropat` counterpart hides this by falling back to a **TURN relay** (via Anker's servers) — which
`@mega-yfue/eufy-sdk` does not implement, and which we don't want anyway (a force-LAN wall must stay local).

## Our fix

Because the real port is bound to the connecting socket, discovery must happen **on that socket**. We patch
the SDK's P2P session so that, when a LAN address is pinned for the station (`localAddresses`), it sweeps
`CHECK_CAM` across all local ports **on its own socket**, paced in 2048-port bursts, stopping the instant a
`CAM_ID` arrives — at which point the SDK's normal `onConnected` path takes over. Result: a **pure-LAN**
session (the LAN guard confirms the peer is inside `lan.cidr`; force-LAN stays intact).

- Patch: `server/scripts/patch-sdk.mjs` (idempotent, runs on `npm install` via `postinstall`). It rewrites
  the `sendLookups()` local-address branch in `@mega-yfue/eufy-sdk/dist/index.js`.
- Config: put the **HomeBase LAN IP** (host only) under `lan.station_addresses` — e.g.
  `T8030…: 192.168.23.233`. The session finds the port; you do **not** pin a port.
- Scope: only **HomeBase-attached** cameras sweep (they're the ones with the NAT'd port). Standalone
  cameras have no `station_addresses` entry, so they use the SDK's normal path (which works for them).

### Why there is no cache to go stale

The port changes every session and is socket-bound, so there is deliberately **no cross-session cache and
no "try the last port first" fast-path** — that could get stuck on a dead port. Each session sweeps fresh;
if a session drops, the stream manager opens a new one which sweeps again. The sweep is always the source
of truth, so it degrades gracefully by construction.

### Cost

One burst of ≤65535 small UDP packets per session open (~1 s, self-throttled), which also draws ICMP
port-unreachable from closed ports (ignored). Acceptable for an always-on bridge; it is brute-force, and
the *correct* long-term fix is the device-initiated hole-punch below.

## The correct upstream fix

The clean solution belongs in the SDK: implement the **device-initiated local hole-punch** the app uses
(register with the cloud so the station fans back to the client's port range and answers `CAM_ID`), and/or
a TURN-relay fallback. See `docs/upstream-issue-eufy-sdk.md`. Until then, the socket-sweep patch is the
force-LAN-compatible workaround.

## Reproduce / diagnose

- `server/spikes/portscan.mjs <stationSn> <lanIP>` — protocol-aware scan; prints the port answering
  `CAM_ID` right now (needs macOS Local Network permission for the terminal, and a LAN path to the station).
- Phone capture: `rvictl -s <iPhone-UDID>` then `sudo tcpdump -n -i rvi0 -w cap.pcap udp` while streaming in
  the app; look for the station→phone `CHECK_CAM`/`CAM_ID` fan-out and the settled media port.
