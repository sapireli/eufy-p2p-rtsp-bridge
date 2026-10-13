# Android motion, snapshots and custom walls — 2026-10-13

## Delivered scope

Version 0.6.0 / version code 9 adds motion-following tiles, retained snapshots
while a battery camera is asleep or waking, passive fixed battery tiles, and
persisted custom grids with spans and ordering. The maximum is 36 configured
tiles in a 1–6 by 1–6 grid. This is a configuration limit, not a guarantee that a
particular TV can decode 36 live streams simultaneously. Automatic layouts retain
the existing 70/30 portrait arrangement. Camera source quality, RTSP decoding,
hardware-first/software fallback and the pacing allowance were not changed.

Setup has descriptive tile cards, focus highlights and remote-operated editors.
The live wall supports D-pad selection and OK camera actions, including an explicit
two-minute battery-camera wake. Back returns to setup. Existing saved selection
and bridge preferences migrate without changing order.

## Behavioral verification

The app and vendored receiver unit suites passed: **79 app tests and 14 RTSP
tests**, zero failures, errors or skipped cases. New tests cover motion watch
sets, monotonic dwell and blank boundaries, finite wake expiry, motion preserved
through hello, removed-camera rejection, 36-tile non-overlapping geometry,
explicit spans, invalid coordinates, JSON roundtrip/migration, JPEG allocation
bounds and cache identity. Hold tests cover deferred POST/DELETE ordering,
coalesced renewals, deselection, re-selection and shutdown.

A controlled HTTP/WebSocket fixture ran on the Mac. Its virtual battery cameras
used distinct labeled JPEGs. Hold requests were recorded locally and never
forwarded to the real bridge. Real Front Door and Garage compatible RTSP URLs
were used for decoder transitions. This separates client behavior from an
uncontrolled physical motion event or camera wake latency.

| Device check on Fire TV .85 | Observed result |
| --- | --- |
| Five configured tiles in a custom 3×2 grid | All five slots rendered; two retained stills, a labeled missing still, a waiting motion tile and live Garage |
| Passive startup | Snapshot requests occurred without any camera hold POST |
| A motion, then B after one second, five-second dwell | Motion tile remained on A, then switched to B; A DELETE and B POST followed selection |
| Eight-second inactivity blanking | Motion picture cleared and B hold released |
| Starting → live → idle | Snapshot stayed during starting; real hardware decoder output replaced it; snapshot returned after idle |
| HTTP 503 snapshot refresh | Previously retained picture remained visible |
| Missing snapshot, HTTP 404 | Empty labeled tile; no false “last snapshot” claim |
| Remote OK → timed wake → Home | A hold POST followed the explicit wake; leaving the app sent DELETE |
| Tile editor width change and Save | Two-column span persisted in JSON and rendered across two cells |
| Old A response delayed two seconds, switch to B after 250 ms | B appeared before the old response and remained B afterward; old A did not overwrite the switched tile |

The dwell/blank screenshots were recorded on the first integrated build. The
final build retained the same selection/hold implementation and additionally
passed snapshot transitions, failed-refresh retention, delayed-response
cancellation, span editing and lifecycle cleanup. Earlier integration defects
were corrected before final deployment: hidden stills after live playback,
stale hello mode metadata, null bitmap placeholder labels, cache identity across
motion versions, and queued stale hold requests.

On .58, actual remote keys verified setup focus on Start wall, bridge-entry
Down → Connect, tile-editor and layout-dialog navigation/cancellation, and
return to the live wall. This found and corrected the native EditText D-pad trap.

## Final deployed APK and real playback

APK SHA-256:
`8e9514e6f908ff4fa19030aa5838cd3bdacc383b383f8ddc78c7ad621a3fb957`.
Both TVs received `adb install -r`; no uninstall or data clear was performed.
Both original preference files were confirmed byte-identical afterward. All
temporary .85 test configuration was removed, and both TVs were left on their
original real bridge and two-camera walls. The fixture was stopped, its process
was absent and TCP port 3100 was closed.

| Final real-wall observation | Front Door decoded fps | Garage decoded fps | Max observed decode ingress age |
| --- | ---: | ---: | --- |
| .58, 05:44:39.747–05:45:39.753 UTC | 14.996 | 14.997 | 132 / 144 ms |
| .85, 05:52:40.109–05:53:40.131 UTC | 14.927 | 14.979 | 165 / 146 ms, existing lifetime maxima |

Both cameras used `OMX.amlogic.avc.decoder.awesome`; queues remained zero, with
no recorded errors, releases, pacing drops/resets or new decode deadline misses.
On .85 one informational render-callback deadline counter increased; its
existing 599 ms longest callback gap did not increase. Android API 28 callback
timing is not physical scanout timing. This feature release does not resolve or
claim to resolve the earlier intermittent upstream pauses. Google TV physical
playback and maximum simultaneous live-decoder capacity were not tested here.

## Preserved private evidence

`.evidence/android-wall-features-2026-10-13/` contains unit XML results, final
device metadata/logs, controlled fixture request/event logs and screenshots.
Snapshot images are labeled test JPEGs; real camera video surfaces are black in
these device screenshots. Preferences and fixture owner IDs remain private and
are not checked into the repository.
