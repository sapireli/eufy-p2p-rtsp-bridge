# Reviewed SDK contract deployment — 2026-10-11

## Revisions and scope

SDK fork `eufy-wall`: `9b1cc3d969b4569b3389bc3f908c2d7982a08543`, a GitHub-verified
signed commit applying PR [368](https://github.com/mega-yfue/eufy-sdk/pull/368)
head `070ff3070b4f23a81673e2c87f6f5c9394f336ac` to the existing fork.
Bridge runtime revision: `3859909`.

The SDK exposes the raw unsigned clock as `sourceTimestamp`, without claiming a
universal time unit or clock origin. The bridge consumes the renamed field and
retains its measured millisecond interpretation. No buffering, quality, LAN policy,
hardware preference, encoding or client pacing changes were made in this update.

## Build and deployed identity

- SDK full `npm run verify`: **234 files, 4,096 tests passed**.
- Bridge full gate: **123 tests passed locally and on the Atom host**, none skipped.
- The strengthened SDK contract gate was rerun on both hosts: **9 tests passed**.
  It requires `sourceTimestamp: unit.sourceTimestamp` and rejects the old spelling.
- Built and packed the exact fork checkout; external runtime dependencies are unchanged.
- Deployed SDK bundle SHA-256: `3a15c296a54311782f242aa5446b23971671e4ee6016d43ecb927f4ec7fc3fc8`; identical to the local build.
- Both root and installed dependency lockfiles pin the exact fork commit.
- All **20 checked runtime source/manifest files** match the checkout by SHA-256.
- Rollback: `/opt/eufy-wall-bridge/sdk-backup-before-9b1cc3d-20261011`.

Bridge service on `192.168.23.158` restarted as PID **1323**. Health reported
authentication OK, go2rtc running, three cameras streaming and zero stalls.
Front Door and Garage FFmpeg processes retained VA-API decode, scale and H.264
encode, source-timestamped MPEG-TS, passthrough frame timing and `1:90000`
encoder time base. The temporary loopback-only inspector was closed after capture.

## Live source and RTSP checks

A 30-second passive observation of actual SDK video events captured **1,663**
frames. Every frame had a valid unsigned 32-bit `sourceTimestamp`:

| Stream | Codec / native geometry | Frames | Median source step | Source/wall ratio |
| --- | --- | ---: | ---: | ---: |
| Front Door | H.264 / 1600×2200 | 460 | 67 | 1.0213 |
| Garage | H.265 / 1280×1440 | 452 | 67 | 1.0028 |
| Balcony | H.265 / 1280×720 | 751 | 40 | 1.0012 |

These are delivery-window ratios, including startup/catch-up variation, not
claims that each camera clock runs at precisely that ratio indefinitely.
The capture was passive: no second camera session was opened.

A separate concurrent 30-second RTSP probe excluded three settling seconds:

| Stream | Stable picture timestamps | RTP/wall ratio | RTP FPS | Timestamp regressions |
| --- | ---: | ---: | ---: | ---: |
| Front Door | 405 | 0.9997 | 15.0 | 0 |
| Garage | 404 | 1.0022 | 15.0 | 0 |

This probe used the numeric address because this Mac's default resolver could
not resolve the LAN name. Client saved URLs remain hostname based. These checks
verify renamed metadata delivery and progressing output clocks; they do not
measure glass-to-glass latency or prove every incoming pause is eliminated.

## Client checks and DNS boundary

GaragePi's service was initially inactive. Started it for verification without
changing boot enablement or its power-control script. `homebridge.lan` resolved
to the bridge. Both real KMS camera planes advanced framebuffers:
plane 98: **680 → 679 → 680**, plane 109: **674 → 676 → 678**.
Its existing binary, native pacing plugin and hostname configuration were unchanged.

Both Fire TV viewer apps were already running, but their UI showed
**“Bridge disconnected • retrying”** and no video surfaces. On both devices,
`ping homebridge.lan` failed with **unknown host**, while reaching the numeric
bridge address worked. The LAN router answered the A query correctly at this
same checkpoint. TV `.85` network diagnostics listed both the LAN router and
public Google IPv4/IPv6 DNS resolvers. Public resolvers cannot provide this LAN
record; name resolution is a separate playback blocker. The source of those extra
resolvers and its role in the failure were not established.

No Fire TV presentation success is claimed for this checkpoint. Retained the
requested hostname settings; no numeric-address workaround or DNS policy changes
were applied. The bridge and GaragePi live checks above succeeded independently.

## Latest review synchronization

The reviewer accepted the broader need and unit-neutral contract, and requested
removal of the dated host evidence page from the SDK’s published guide tree.
PR head `2c846e78fa7e684fe1b78f3ece13876cb37b7d71` removes only that page.
The deployment fork is now `869ef01f86eb0effcc481fcd384376f491067726`, with
the same removal. Both new commits have verified signatures. Evidence is retained
in this repository’s `sdk-pr368-source-clock-review-2026-10-11.md` and the PR body.

Full verification passed again: **4,020 PR tests**, **4,096 fork tests**. Rebuilt
and repacked the fork: its entire published archive is byte-for-byte identical to
the already deployed runtime, SHA-256
`cf8bca483a15e0d78d1fe61339fa2bfc34ec39c25ad66a6b166fa515abb7426d`.
Therefore this documentation-only synchronization requires no playback interruption.
Updated the repository lockfile, deployed root and installed lockfiles, source
mirror and revision marker to the latest fork commit. The installed bundle hash
remains the verified value above.

Final health: service active, authenticated, go2rtc running, both main cameras
streaming, `stalled: false`. Garage’s historical stall counter had reached 2
since the initial checkpoint; this is not a claim that upstream pauses are gone.
The hostname investigation was stopped at the user’s request. No router or client
DNS settings were changed.
