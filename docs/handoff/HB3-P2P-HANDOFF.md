# HomeBase 3 P2P streaming — handoff

Status: **unsolved for the primary goal.** Cloud-relay streaming works; reliable **direct LAN (P2P)**
streaming to the HomeBase 3 does not yet. This doc is a complete handoff: the goal, everything tried,
what was learned, where the fork is, and open leads. The previous agent's leading hypothesis
(network/segment positioning) is included but **the user disagrees with it** — keep an open mind.

---

## 1. Goal (revised by the user — this supersedes "force-LAN by default")

Build the bridge so it:

1. **Resolves to P2P (direct LAN) when it can** — prefer a direct device↔bridge media path.
2. **Falls back to the cloud path** when direct P2P can't be established (media may ride Anker's TURN relay).
3. Offers an **opt-in "P2P-only" mode** (reject any non-LAN/relay peer) for users who want LAN-only.

So this is **NOT force-LAN by default anymore.** Today the code force-LANs unconditionally (the LAN guard
closes every non-LAN peer). That must become a **mode**, default off. Concretely: `lan.force` in
`server/config.yaml` already exists (currently `true`) and drives `server/src/lan-guard.mjs`. Setting it
`false` makes the guard *warn but keep* a WAN/relay session — which, combined with the working TURN
handshake (below), should immediately give cloud-fallback streaming. The "P2P-only" mode = `lan.force: true`.

Both paths should work; direct P2P is preferred; relay is the fallback; P2P-only is selectable.

---

## 2. TL;DR of current state

- Bridge SDK upgraded from `@mega-yfue/eufy-sdk` **0.1.1 → 0.2.0-beta.22** (per-camera media sessions,
  contended-session handling). Branch: **`sdk-0.2-beta-migration`**.
- Architecture rewritten to the SDK's intended shape: **one client** for control + all streams; the SDK
  opens a per-camera media session (`<stationSn>#live:<channel>`) on demand. Removed the old per-camera-client
  workaround, the multichannel bypass, and station-resync group recovery. Recovery = solo per-camera reopen
  with a session teardown before retry.
- **A TURN rendezvous handshake was implemented in a fork** (the beta lacks it) and it **works**: it completes
  a connection via `TURN_SERVER_CAM_ID`. BUT that connection is the **cloud relay** (peer = Anker AWS, e.g.
  `3.16.202.62`, `18.215.90.2`), so the current force-LAN guard closes it. With `lan.force:false` this would
  stream via relay.
- **Direct LAN to the HB3 is intermittent.** Sometimes a direct `CAM_ID` arrives from `192.168.23.233:<port>`
  and it streams (observed live ports: 10498, 18690, 27076); often the HB3 is silent on the LAN to us.
- **Standalone cameras (Balcony, T8423 @ .89) connect natively and reliably.** Only the HB3 is the problem.
- The eufy **app streams the HB3 instantly** from the same account (so the account/credentials are fine; the
  account is a *member/shared* account — settings writes return `20004 "Only the owner can change settings"`,
  but DSK/cipher/streaming all work).

---

## 3. Environment & how to run / test (important — the operator drives the LAN)

- **The bridge must run in the user's Ghostty terminal**, not via the assistant's shell. macOS Local Network
  privacy denies LAN UDP to the process the assistant's Bash runs under (EHOSTUNREACH to every LAN peer except
  the gateway). The assistant reaches the bridge only over **loopback (127.0.0.1)**.
- Run command (in Ghostty), with debug + the sweep-error spam filtered:
  ```
  cd ~/code/eufy-p2p-rtsp-bridge/server && BRIDGE_DEBUG=1 node --watch server.mjs 2>&1 \
    | grep --line-buffered -vE "send err|EHOSTUNREACH" > run.log
  ```
  `--watch` auto-restarts on any `.mjs`/`node_modules` edit. `server.mjs` auto-loads `server/local.env`
  (git-ignored: `EUFY_EMAIL`/`EUFY_PASSWORD`/`EUFY_COUNTRY`).
- **Config**: `server/config.yaml` is git-ignored (live config). Editing it does NOT trigger `--watch`
  (it's read, not imported) — `touch server.mjs` to restart after a config change.
- **Auth / 2FA**: the account uses 2FA. On a fresh login the bridge sits at `auth: pending`; submit the code
  over loopback: `curl -sX POST "http://127.0.0.1:3000/auth/tfa?code=NNNNNN"`. `POST /auth/retry` re-triggers
  login. **Do not churn logins** — eufy rate-limits 2FA sends after many attempts (we hit this). The beta and
  0.1.1 use different session-file formats, so switching SDK versions forces a fresh 2FA.
- **Observability**: `curl -s 127.0.0.1:3000/healthz` (streaming set, stalls, auth), `GET /api/cameras`
  (per-cam codec/dims/streaming), and `run.log` (debug P2P trace: `<<< host:port header=fXXX` inbound,
  `LOOKUP_ADDR ->`, `beginCheckCam`, `connected`, `TURN_SERVER_INIT ->`, etc.).
- **Loopback debug API** (DEBUG-gated, `server/src/http.mjs`): `GET /debug/p2p` (control-client sessions),
  `GET /debug/probe-sessions?sns=SN1,SN2&seconds=N` + `GET /debug/probe-result` (open throwaway sessions),
  and (currently) `/debug/multichannel`, `/debug/stall-ms` toggles. These let the assistant run LAN
  experiments through the LAN-permitted bridge over loopback.

### Real devices on this account (192.168.23.0/24)
- **T8030P1324221481** — HomeBase 3, LAN `192.168.23.233`. Parents: Front Door + Garage (+ Garage Interior, battery/off).
- **T8214510242321E6** — Front Door CLE (doorbell dual, view-mode-12 split), on the HB3. H.264 1600×2200 when up.
- **T8425T1124130A0C** — Garage CLE (dual), on the HB3. H.265.
- **T8423T60241702B1** — Balcony CLE (Floodlight, **standalone**, its own station @ `192.168.23.89`). H.265. Connects natively & reliably.
- T81A0… ×2 (battery, disabled in phase 1), T821451… Garage Interior (battery, on HB3, disabled).

---

## 4. Repo map (key files)

- `server/src/sdk-adapter.mjs` — the ONLY file importing `@mega-yfue/eufy-sdk`. Builds the single `EufyMega`
  client; `streamClientFor()` returns that one client; `openFeed()` = `getDevice(sn).camera().openReadable({powered})`;
  `dropStreamClient()` closes a station's sessions (control + `#live:` media) for teardown-retry; `closeSession`,
  `sessionPeerHost`, `sendSetPayload` (raw SET_PAYLOAD escape hatch for dual-view pins), `probeSessions` (debug).
- `server/src/stream-manager.mjs` — slots, warm feeds, fan-out to go2rtc, stall/gap watchdog, `scheduleReopen`
  (solo per-camera), `openFeedInto` (teardown-before-retry via `recreate_client_after`, default 8).
- `server/src/lan-guard.mjs` — force-LAN enforcement. Closes any P2P session whose peer ∉ `lan.cidr`. **This is
  what must become opt-in (`lan.force`).**
- `server/src/config.mjs` — config + env; `stall.*` (stallMs 30000, gapMs 45000, backoffMs [1,2,4,8]s,
  recreateClientAfter, exitAfterMs), `lan.{cidr,force,stationAddresses}`.
- `server/config.yaml` (git-ignored) — live config. `lan.cidr: 192.168.23.0/24`, `lan.force: true`,
  `station_addresses: { T8030…: 192.168.23.233, T8423…: 192.168.23.89 }`.
- `server/scripts/patch-sdk.mjs` — postinstall patcher for the pinned SDK (the local-port **sweep** and the old
  multichannel bypass). **Do not use/discuss the sweep** — the user has explicitly deferred it until they
  authorize it (it only ever produced a working connect by brute force and muddied results). The fork/native
  path is the direction.
- `server/src/go2rtc.mjs`, `server/data/go2rtc.yaml` — go2rtc pulls `/stream/<sn>` (Annex-B) → RTSP/WebRTC/MSE.
- `server/src/pins.mjs` — dual-lens view-mode-12 pin + streamingQuality (the quality write always fails on this
  member account: `20004`; codec must be set in the owner's app).
- `docs/hb3-local-port.md` — earlier (0.1.1-era) writeup of the HB3 local-port problem (some of it, e.g. the
  "sweep is required / cloud port is always NAT'd" framing, is **superseded** — see findings below).
- `docs/upstream-issue-eufy-sdk.md` — draft upstream issue (file under the USER's GitHub account, not as Claude).
- Memory: `~/.claude/projects/-Users-eliransapir-code-eufy-p2p-rtsp-bridge/memory/` — `project-eufy-wall-decisions.md`,
  `project-hb3-p2p-session-model.md` (note the latter's "sweep=1/host" conclusion is partly superseded).

---

## 5. The fork (native cloud hole-punch / TURN handshake)

- **Upstream source**: `https://github.com/mega-yfue/eufy-sdk`, branch **`beta-0.2.0`** (this is what npm
  publishes as `0.2.0-beta.*`; the committed `package.json` version still reads 0.1.1). P2P code:
  `src/transport/p2p/` (`p2p-session.ts`, `codec.ts`, `command-router.ts`, `session-manager.ts`, `lan-ip.ts`).
- **Our changes** are captured in **`docs/handoff/sdk-turn-handshake.patch`** (git diff against `beta-0.2.0`).
  They add bropat-style TURN rendezvous to `codec.ts` (opcodes `TURN_SERVER_INIT 0xf170`, `TURN_CLIENT_OK 0xf172`,
  `CHECK_CAM_TOKEN 0xf183`, `TURN_LOOKUP_WITH_KEY 0xf180`, responses `TURN_SERVER_OK 0xf171`,
  `TURN_SERVER_TOKEN 0xf173`, `TURN_SERVER_LOOKUP_OK 0xf181`; payload builders `buildCheckCamTokenPayload`,
  `buildTurnLookupPayload`) and to `p2p-session.ts` `onMessage`: on `LOOKUP_ADDR2` → `beginTurnRendezvous`
  (CHECK_CAM the embedded token + `TURN_SERVER_INIT` once/host), `TURN_SERVER_OK` → `TURN_CLIENT_OK`,
  `TURN_SERVER_TOKEN` → CHECK_CAM_TOKEN + `TURN_LOOKUP_WITH_KEY` to cloud addrs.
- **Reference implementation**: bropat/eufy-security-client `src/p2p/session.ts` (~L1615-1690 handshake) and
  `src/p2p/utils.ts` (`buildCheckCamPayload2`, `buildLookupWithKeyPayload3`). Clone it to cross-check byte layouts.
- **The fork itself was built in an EPHEMERAL job dir and will be gone.** To recreate: clone `beta-0.2.0`,
  `git apply docs/handoff/sdk-turn-handshake.patch`, `npm install`, `npm run build` (→ `dist/index.js`), then
  link into the bridge (copy `dist/` over `server/node_modules/@mega-yfue/eufy-sdk/dist`, or a `file:` dep —
  note `postinstall` runs `patch-sdk.mjs`, which would re-apply the deferred sweep; guard against that).
- **The user wants this to become a PR** to `mega-yfue/eufy-sdk` (under the user's GitHub account). It passes
  typecheck; the repo also has `guard:*` CI scripts and `npm test` (vitest) to satisfy for a clean PR.

---

## 6. What was tried (chronological, with outcomes)

1. **Continuous local-port sweep** (SDK patch): brute-force CHECK_CAM all 65535 ports. Reliably connected the
   HB3 — but it's brute force and the user has **deferred it**. Do not pursue without authorization.
2. **Powered hint** (`openReadable({powered:"wired"})`): the SDK mis-read mains cams as battery and armed a
   ~45s budget that tore streams down. Fixed — real win, keep.
3. **Shared session per station + multichannel bypass** (0.1.1): got both HB3 cameras on one session but was
   fragile under stalls (contention cascade). Superseded by the beta's per-camera sessions.
4. **Station-coordinated recovery** (tear down all station cams together): worked but is the wrong model on the
   beta (per-camera sessions are independent). Removed.
5. **SDK upgrade 0.1.1 → 0.2.0-beta.22**: per-camera media sessions, contended handling. Kept.
6. **Single-client architecture**: one `EufyMega` for everything (research showed multiple clients sharing one
   account/identity displace each other's cloud session → breaks DSK/cipher lookups → connect timeouts). Kept.
7. **Teardown-before-retry** (`recreate_client_after`): close the station session so the next open re-lookups a
   fresh port. Helps refresh a stale looked-up port; do NOT tear the *control* session on every per-camera flap
   (that kills the shared session — set the threshold >1, currently 8).
8. **TURN rendezvous handshake** (the fork): implemented and **works** — but resolves to the **cloud relay**
   (WAN peer), which force-LAN blocks. It IS the cloud-path answer; it is NOT direct-LAN.
9. **HB3 reboot + clean single-client native test**: control session connected directly (`.233:18690`,
   `CAM_ID`/`f142` from `.233`), proving native LAN *can* work — but per-camera media sessions then timed out,
   and across runs direct-LAN to the HB3 stayed intermittent.

---

## 7. Key findings (SDK mechanics — from reading the 0.2 source)

- Connect (`p2p-session.ts`): every ~1s (`LOOKUP_RETRY_MS`) it sends broadcast `LOCAL_LOOKUP` to
  `255.255.255.255:32108` (macOS/routers often drop this), unicast `LOCAL_LOOKUP` to `localAddress:32108`, and
  cloud `LOOKUP_WITH_KEY`; on a lookup response it `beginCheckCam(addr)` = `CHECK_CAM` to the reported port **±3**.
  `CONNECT_TIMEOUT_MS = 15s`; on timeout the session closes (no auto-retry — recovery is rebuild-on-next-use).
- `close()` sends `END` to the station and closes the socket; each `connect()` binds a fresh socket and re-runs
  the lookup (no port caching).
- **Our HB3 never answers `LOCAL_LOOKUP` on `.233:32108`** (0 `LOCAL_LOOKUP_RESP` from `.233`). So the only
  direct path is cloud `LOOKUP_ADDR` → CHECK_CAM ±3. The cloud-returned port is the station's registration; it
  is **sometimes the live LAN port** (then direct connect works) and **sometimes stale** (then ±3 misses).
  Within one session the cloud returns the same port repeatedly; it changes after a teardown/reboot.
- HomeBase cameras **require the level-2 key** (`CMD_GATEWAYINFO` → `resolveCipherKey`/cloud `get_ciphers`),
  which works on this account. Standalone cams can stream at level-1.
- Per-camera media sessions (PR #168) are **automatic** in the beta (no `maxLiveStreamsPerStation` knob here —
  that's the sibling `keesmod/eufy-mega-client`). `getP2pSessions()` returns composite `<sn>#live:<ch>` keys.
- `TURN_SERVER_CAM_ID` (`0xf184`) = connect via **relay** (WAN). Direct P2P success is `CAM_ID` (`0xf142`) from
  the device's own LAN address.
- **Contention is real**: multiple logins on one account/`openudid` displace each other (SDK
  `mega-client.ts` `CONTENDED_SESSION_HINT`, backoff 60s→30min). The single-client change addresses our
  self-contention; give the bridge its own `openudid` if ever running alongside another client.

---

## 8. The core open problem + competing hypotheses

**Problem:** direct-LAN P2P to the HB3 is intermittent; the app does it instantly; we don't.

- **Hypothesis A (previous agent — user disagrees): network/segment.** The bridge Mac (`.229`, iface `en1`)
  may not be reliably on the HB3's L2 segment/Wi-Fi AP, so the HB3's direct-LAN responses don't consistently
  reach it, while cloud/relay (WAN) always does. Balcony (`.89`) works from the bridge, the HB3 mostly doesn't.
  Test: wire the Mac to the same switch as the HB3 / same AP; check AP/client isolation and VLANs.
  **The user rejects this as the explanation — do not assume it; treat as one lead among several.**
- **Hypothesis B: we're driving the cloud-brokered *direct* hole-punch wrong.** The implemented TURN handshake
  resolves to relay (`TURN_SERVER_CAM_ID`), not a direct LAN punch. The app likely gets the station to punch
  back **directly on the LAN**. The byte layouts of the TURN token/`TURN_LOOKUP_WITH_KEY` were ported from
  bropat and may be subtly off, or a step is missing, such that we only ever get the relay, never the direct
  punch. **A packet capture of the phone app connecting to the HB3 is the ground truth** and was never taken
  (would need `rvictl -s <iPhone-UDID>` + `sudo tcpdump -i rvi0 -w cap.pcap udp` while streaming in the app).
  iPhone UDID on file: `00008130-0006203A2403401C`.
- **Hypothesis C: stale looked-up port + no direct fallback.** The cloud gives a stale port; ±3 misses; we need
  a way to obtain the live port without the sweep — e.g., a wider/adaptive CHECK_CAM window, or forcing a fresh
  station registration, or actually getting `LOCAL_LOOKUP` to work (why does the HB3 ignore our unicast on
  32108 but answer the app? payload? source port? segment?).

---

## 9. Concrete next steps (suggested)

1. **Ship the dual-path now**: make `lan.force` truly optional and default it **off** so the working TURN/relay
   path streams the HB3 (cloud fallback), while direct-LAN is still preferred when a `CAM_ID` from `.233`
   arrives first. Add an explicit **P2P-only** mode (`lan.force: true` → keep the current guard behavior).
   This satisfies "both paths work; resolve to P2P if possible; P2P-only selectable" immediately.
2. **Get the app pcap** (Hypothesis B) — the single highest-value diagnostic for the *direct* LAN punch. Compare
   the app's TURN/lookup bytes to `docs/handoff/sdk-turn-handshake.patch` and fix the direct path.
3. **Investigate why the HB3 ignores our unicast `LOCAL_LOOKUP` on `.233:32108`** (Hypothesis C) — inspect the
   `buildLocalLookupPayload` (currently `[0,0]`) vs what the app sends; try the directed subnet broadcast
   `192.168.23.255` (macOS allows it; SDK uses `255.255.255.255` which macOS drops).
4. Only if 1-3 fail and the user authorizes it, revisit the deferred brute-force sweep as a last-resort mode.
5. Finish the PR to `mega-yfue/eufy-sdk` for the TURN handshake (typecheck passes; satisfy `guard:*` + vitest).

## 10. Gotchas
- Don't run two logins on the account at once (contention). Don't churn logins (2FA rate-limit).
- Editing `config.yaml` needs a `touch server.mjs` to take effect; editing `.mjs`/`node_modules` auto-restarts.
- The member account can't write settings (`20004`) — camera codec (H.265→H.264) must be changed in the
  **owner's** eufy app; Balcony + Garage are H.265 (the Pi can't decode H.265 → needs app change or transcode).
- `docs/hb3-local-port.md` and the `project-hb3-p2p-session-model.md` memory contain some **superseded**
  conclusions (the "sweep is required / cloud port unrelated to LAN port" framing). Trust this handoff's §7.
</content>
