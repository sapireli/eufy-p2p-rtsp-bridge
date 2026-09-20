# Direct-LAN P2P: how it works, and what was wrong

The bridge streams from the HomeBase 3 and the standalone cameras over a **direct LAN P2P path**. It
used to fall back to Anker's cloud relay most of the time, and lossy cameras froze on a single frame.
Both causes were found and fixed in the SDK; this is the short version so nobody re-derives it.

## How a direct connect actually happens

It is a **cloud-brokered mutual hole punch**, not LAN discovery:

1. The client registers its own `host:port` with the cloud (`LOOKUP_WITH_KEY`, `0xf126`).
2. The cloud tells the station which client port to punch.
3. The station punches that port ±3 (`f141` from its live media port).
4. The client answers with `CHECK_CAM`; the station replies `CAM_ID` (`0xf142`); the session is up.

The HomeBase does **not** answer a bare `LOCAL_LOOKUP` broadcast, so there is no LAN-only discovery
path to fall back on — the cloud lookup is what triggers the punch, even for a purely local session.

## The two bugs

**1. One registered source port.** The cloud only makes the station punch a *subset* of the ports it
has been told about. The SDK registered a single port per connect, so it won the punch ~42% of the
time (11/26 measured); the rest fell back to relay or timed out. Registering several in parallel —
which the official app does — wins ~100% (7/7) in ~110 ms.

**2. Retransmissions were discarded.** The transport is acknowledged and the station resends anything
unacknowledged, but reassembly was forward-only: it skipped past a hole, dropped the half-built frame,
then ignored the resend as "stale". One lost datagram cost a whole frame permanently, which decoders
report as `Could not find ref with POC` / `Error constructing the frame RPS` — a frozen picture. A
capture of the official app shows 414 ACKs against 662 data packets: it depends on those resends.

Both are fixed in the pinned SDK fork and open upstream: [#211][211], [#212][212]. Also upstreamed:
[#213][213] (warm-up options were silently dropped) and [#214][214] (`lanOnlyForStation`, which is how
`lan.force` / P2P-only mode is enforced).

[211]: https://github.com/mega-yfue/eufy-sdk/pull/211
[212]: https://github.com/mega-yfue/eufy-sdk/pull/212
[213]: https://github.com/mega-yfue/eufy-sdk/pull/213
[214]: https://github.com/mega-yfue/eufy-sdk/pull/214

## Ruled out — do not re-investigate

- **macOS Local Network / L2 segment.** Refuted: port 32108 on every station is open and reachable
  both ways (a closed port returns ICMP `ECONNREFUSED`, 32108 does not).
- **`f126` byte constants.** The app uses splitter `00 00` / version `02 05 02 02`, the SDK `00 02` /
  `02 05 01 05`. Both work equally — tested head to head.
- **Registration ordering**, **cloud ports 32101/32102**, **the wider cloud server pool**, and a
  **station cooldown between attempts** (3s/12s/25s all ~1/3). None of them decided anything.
- **TURN/relay rendezvous.** Not in upstream; an earlier unverified patch. It *loses* the race to the
  direct punch and was dropped once the punch became reliable.

`CHECK_CAM` in reply to the cloud's `f140 LOOKUP_ADDR` **is** required — disabling it gives 0 connects.

## Operational notes

- **Streaming quality is `Auto`** (`streamingQuality: 0`) on these cameras and cannot be written from a
  member account (`20004`, owner only; a P2P `2730` write was tried and ignored). The station therefore
  re-picks a resolution on its own, and a mid-session change invalidates an RTSP consumer's negotiated
  SDP — `stream-manager.mjs` detects a geometry change and drops consumers so go2rtc re-negotiates.
- **go2rtc serves RTSP over TCP only** (UDP `SETUP` returns `461`). Point VLC at
  "RTP over RTSP (TCP)" / `--rtsp-tcp`, or its UDP attempt can time out on the largest stream.
- **Transcoding is hardware-only** (`go2rtc.transcode`, see `config.mjs`). Software encoding does not
  hold real time here and takes the stream down when ffmpeg falls behind.

## Debugging

Loopback-only, requires `BRIDGE_DEBUG=1`:

- `GET /debug/p2p` — live sessions and the peer each settled on (`192.168.x` = direct, else relay/WAN)
- `GET /debug/lan` — per-station LAN/relay mode and attempt counts
- `GET /debug/drop?sn=` — tear a station's sessions down so the next open re-runs the lookup
