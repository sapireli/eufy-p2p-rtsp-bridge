# Draft upstream issue — mega-yfue/eufy-sdk

**Title:** HomeBase 3 (T8030) local P2P never connects — no device-initiated hole-punch / TURN fallback

**Body:**

Cameras behind a HomeBase 3 fail to stream locally: every `openReadable`/`live` ends in
`P2P connect timeout for T8030…`. Standalone cameras work; HB3-attached ones do not. The official app
streams the same cameras over the LAN fine. (Same class as ha-eufy-sdk-bridge #39, #51; fuatakgun/eufy_security #473.)

**Root cause (packet-verified):**

1. The cloud `LOOKUP_ADDR` for HB3 returns the station's **NAT-translated** port (e.g. `192.168.23.233:21762`);
   a local `CHECK_CAM` there gets ICMP port-unreachable. The station's real *local* P2P port is a per-session
   NAT/hole-punch mapping (observed values on one HB3 across runs: 29136, 13832, 14423, 20726, 25214, 28669),
   many thousands of ports away from the cloud value — outside the `±3` `sendCamCheck` window.
2. HB3 does **not** answer `LOCAL_LOOKUP` (`0xf130`) on `32108`, so the SDK never learns the real port that way.
3. A `tcpdump` of the app (via `rvictl`) shows the connection is a **mutual, cloud-brokered UDP hole-punch**:
   the cloud gives the station the client's predicted port and the **station initiates back**, fanning `±3`
   across the client's port range until `CAM_ID` completes. The SDK only attempts the client→station direction
   to the NAT'd port; the station never calls back, so it times out.
4. There is no TURN-relay fallback (`TURN_SERVER_LIST 0xf169` / `TURN_SERVER_TOKEN 0xf173` are unhandled), which
   is how bropat's client connects HB3 when direct fails.

**Ask:** implement the device-initiated local hole-punch (register with the cloud so the station fans back and
answer its `CAM_ID`), and/or the TURN-relay handshake, for HB3-class stations. Happy to share pcaps.

**Workaround we ship (force-LAN, no relay):** patch the P2P session to sweep `CHECK_CAM` across all local ports
on its own connecting socket when a LAN address is pinned, stopping on the first `CAM_ID`. Details:
`docs/hb3-local-port.md`. It's brute-force but keeps the session purely local.

---

_Also worth a smaller follow-up:_ `streamingQuality` has no working writer in 0.1.1 (`setProperty` throws
"wire unverified"), so a host cannot pin a camera to a fixed H.264 tier and must do it in the app.
