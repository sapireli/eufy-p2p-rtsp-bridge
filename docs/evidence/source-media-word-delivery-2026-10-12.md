# Source media time stays continuous across upstream delivery pauses

## Same-window result

A passive 600-second NIC capture ran **2026-10-12 19:57:14.121711–20:07:14.121867
UTC**, with 495,912 metadata records, zero tap drops, missing timestamps or
truncations. A concurrent two-URL compatible RTSP observer retained 9,005 Door
and 9,000 Garage stamped pictures, no missing/invalid stamps and one unchanged
server epoch. It added consumers to existing producers; no Inspector hooks,
source-quality changes, stream-selector changes or observer-issued SDK commands
were used. All owned capture sockets/RTSP consumers closed at completion.

The bridge watchdog deliberately disconnected all feeds at 19:57:17; that
already-documented forced-relogin boundary is excluded from the source-pause
analysis below. The selected events occur several minutes after recovery.
Running SDK was a9424bf with deployed bundle
`48372212a4e16a45fd2601febc5239e21f503339303ad975e79aca78583cddb5`.
The later poll-completion SDK deployment is outside this capture window.

| Largest overlapping event | Door | Garage |
| --- | ---: | ---: |
| Physical VIDEO quiet | 1049.623535 ms | 668.669212 ms |
| Consecutive transport sequences | 58000→58001 | 35589→35590 |
| Quiet start UTC | 20:03:39.006075 | 20:03:03.531226 |
| Quiet end UTC | 20:03:40.055698 | 20:03:04.199896 |
| Associated stamped bridge-ingress gap | 1133.727 ms | 711.185 ms |
| Original source-word advance | **66 ms** | **68 ms** |
| RTP advance | 5940 ticks = **66 ms** | 6120 ticks = **68 ms** |
| Resume pictures within 500 ms | 14 | 13 |
| Observed ingress span of those pictures | 486.020 ms | 499.704 ms |
| Source media advance over that span | **868 ms** | **802 ms** |

The largest five overlapping quiet intervals on each camera all had only
62–73 ms of source-word advancement across the associated ingress gap, while
physical quiet lasted 491–1050 ms. They did **not** skip forward by the whole
wall-time silence in their media labels. Resume pictures then arrived faster
than their labeled media time advanced.

Door's first resume picture increased relative ingress-minus-source lag by
1067.727 ms versus the preceding picture. Within the first second, 25 pictures
arrived across 969.856 ms while media advanced 1603 ms; the relative change fell
to 434.583 ms. Within two seconds, 46 pictures across 1975.533 ms advanced 2999 ms,
leaving 44.260 ms relative change. Garage's corresponding relative change rose
643.185 ms at resume, then fell to 33.057 ms after 25 pictures spanning 991.872 ms
and 1602 ms media advance. Subsequent smaller jitter changes that residual again;
this is not a permanent zero-lag claim.

This establishes continuous media labels followed by burst delivery **upstream
of bridge ingress**. The bridge/player cannot receive pictures while the physical
stream is quiet. It does not identify whether camera encoding, HomeBase forwarding,
peer send-window/ACK acceptance or another upstream queue held those pictures.
It also does not prove sensor capture time, constant absolute camera age or an
end-user display-age bound. Neither source-clock nor burned-in clock capture
semantics are independently validated here.

## Alignment and exact source/RTP association

The passive tap records the same host's realtime and monotonic anchors at start
and end. Their offset difference was 16.624 microseconds over 600 seconds. The
stamp's ingressMonoUs maps into that host domain, permitting interval alignment.
This is interval correlation by camera/session, not a byte-exact UDP→access-unit
join: incoming logical media headers were not retained. The RTSP samples themselves
pair each original source word, ingress timestamp, global frame ID and RTP timestamp
from its SEI; RTP advancement matched source-word advancement exactly at the
reported boundaries. Global frame-ID differences are not per-camera loss counts.

The Door source word advanced 600269 ms over 599748.623 ms of ingress time in the
whole observation (about +868 ppm); Garage advanced 599934 ms over 599949.106 ms
(about −25 ppm). This clock/rate difference is another reason not to interpret
an absolute word-to-host subtraction as sensor age. Millisecond-to-second burst
changes within one camera are reported as media-time progression only.

Earlier captures do not cover the earlier large events: the exact-180 RTSP
artifact's retained stamped samples begin 19:28:27, after Garage's 9.808-second
physical gap ended 19:28:13.737. The later Door 19:35 gap is outside it too.
These events cannot be assigned source-word behavior retrospectively from
nonoverlapping files.

## Next falsifiable boundary

A nondisruptive next capture can retain only the fixed 16-byte incoming logical
header fields (command ID, declared logical length, channel and type), plus the
existing sequence/ACK metadata. If a quiet interval interrupts an already-started
known-size 1300 video unit, a transfer/forwarding/send-window hold is implicated
for that unit. If the prior unit was complete and the next header arrives only
after silence, production-versus-forwarding remains unresolved. Audio1301 shares
the VIDEO transport type; command identities matter when deciding whether the
silence interrupts a picture or the entire media subchannel. No encrypted body,
raw camera pixels or SDK method hook is required to observe that boundary.

Current files lack those incoming headers, so this document does not declare
keyframe/fragment-boundary causation. ACK submission at the bridge NIC is not
proof of peer receipt/acceptance. No ACK, NAS, camera quality, stream type, buffer
or retry-timer change is justified solely by this result.

The later native metadata artifact does retain logical headers. Its
[offline boundary reconstruction](udp-logical-pause-boundary-2026-10-12.md)
classified all seven selected pauses as beginning after complete known logical
units, with no selected incomplete 1300 transfer. This narrows that later window,
but still leaves production versus forwarding/window scheduling unresolved.

## Private provenance

| Artifact | SHA-256 |
| --- | --- |
| `.evidence/bridge-network-audit-2026-10-12/sourceword-nic-600.jsonl` | `3dd01b0bcc1d7bf9dc3d70c4d2ae20d249faa4ee544a6101078550f782149e5e` |
| `.evidence/frame-freshness-2026-10-12/compat-mac-sourceword-nic-600s.jsonl` | `28ded5d0bea025afa542398938fb7b094c6b5ee53696e4c1779794625ff57787` |
| `.evidence/bridge-network-audit-2026-10-12/sourceword-nic-mac-analysis.json` | `0defd04cd4d099a23ddea05b208df0d46c1401fe983ca005a2524457c44ab0e1` |

The analysis retains the exact selected source words, ingress timestamps, frame
IDs, RTP timestamps, packet boundaries and resume-window counts. The files are
metadata-only and private; the proof has not been converted into an SDK patch.
