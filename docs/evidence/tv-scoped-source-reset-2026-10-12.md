# Fire TV gates for scoped source recovery

These are read-only INFO observations on both TVs. No app installation, launch,
debug setting, preference, camera-quality, or buffer change was made. Both app
PIDs remained `.85` **20526** and `.58` **19463**. All observed players used
`OMX.amlogic.avc.decoder.awesome`. This measures decoder progress and transport
diagnostics, not physical screen presentation or sustained smoothness.

## Initial deployment gate

The production owner deployed SDK scoped-recovery/bridge changes at **23:28:41
UTC**, with Node PID **31548**. The subsequent TV gate ran
**23:29:40.290–23:30:25.501 UTC**.

| TV | Front Door decoded-output increase | Garage increase |
| --- | --- | --- |
| `.85` | +584 | +617 |
| `.58` | +576 | +598 |

There were no errors, read timeouts, retries, counter resets, or new definite
ingress-age misses above 500 ms in this window. Sampled queues were zero.
Latest ingress→decode bounds were Door 133–140 / 66–73 ms, Garage 98–102 /
68–75 ms. The planned deployment restart precedes this observation window.

## Controlled Front Door source reset: ownership survived; timing was not unchanged

TV observation ran **23:31:01.482–23:31:46.667 UTC**. The owner invoked the public
SDK reset for Front Door at **23:31:13.280–23:31:13.295 UTC**, returning true.
The owner reported all three prior public P2P session objects remained identical
and connected afterward. Garage's source UUID stayed unchanged, with no idle/error
state; Front Door received a new source UUID. See the separate owner-side record
in `camera-recovery-ownership-2026-10-12.md` for SDK/source assertions.

Front Door had one expected player counter reset on each TV. Its replacement
hardware decoder reached its first informational render callback at
**23:31:18.057 / 23:31:18.156**, approximately 2.035 seconds after each replacement
player was created. Each new player reached **441 decoded outputs** by the final
sample. No target read-timeout/error was logged.

Garage did **not** recreate its player or report a timeout/error. Its decoder
outputs continued **1900→2510** (`.85`) and **1921→2519** (`.58`), with sampled
queues zero. However, **16 new definite ingress→decode age misses occurred on
each TV**, joined to exactly the same Garage frame IDs **7359–7374** in server
epoch `fa606ba1a53ca431`:

| Metric | `.85` | `.58` |
| --- | --- | --- |
| Sparse outlier wall-time range | 23:31:14.513–23:31:15.020 | 23:31:14.608–23:31:15.114 |
| Decode-age lower bounds | 508.443–737.242 ms | 507.638–738.618 ms |
| Complete-AU receipt→decoder output | 8.157–18.569 ms | 10.208–20.258 ms |
| Inferred AU-receipt lower bounds, minimum / maximum | 497.282 / 726.133 ms | 495.531 / 727.173 ms |
| Informational render-timestamp gap maximum, before→after | 1249→1565 ms | 1233→1549 ms |

Receipt bounds above subtract the logged receipt→decode elapsed time from the
same-sample decode-age lower bound. Clock drift over that interval is less than
0.05 ms. Fifteen of the sixteen frames per TV were already above 500 ms under
that inferred receipt bound; the first crossed the deadline during normal short
decoding. This is substantial delivery/egress age before decoder receipt, not
evidence of a deep compressed-input decoder queue. Informational render-timestamp
gap maxima do not establish a physical panel freeze.

### Essential observer confound

The owner ran Inspector `queryObjects` **before** the reset; its collection/heap
walk cost and possible GC pause were not measured. Source API rows showed the
same Garage sequence 4312/frame 7301 at request starts **23:31:12.240** (age 378 ms)
and **23:31:13.273** (age 1485 ms). Those row timestamps identify request starts,
not exact handler returns, and cannot supply precise intervention chronology.
At the 23:31:14.373 request-start row, Garage last ID was 7374 with receipt
mono-us **47669224895**, age 468 ms; stale-egress count rose **13→27**, with the
cumulative maximum **565→728 ms**. A later row was fresh again. The owner-side
window had Garage input **+667**, output **+665**, stale **+14**, unmapped zero.

There was no native wire capture in this window. The shared late IDs and short
decode durations locate the age upstream of decoding, but do not distinguish
SDK receipt/readable delivery, encoder/egress holds, wire delivery, or production.
It is **not defensible to attribute this burst to the camera reset, station,
or GPU**, or to claim timing was unaffected. Ownership isolation passed while
this temporal gate remained confounded.

## Rollback boundary and final ordinary gate

The original deployment rollback **did fire at 23:32:43–23:32:44**, despite an
earlier stop attempt, restoring the prior deployment under Node PID **31753**.
This was an intentional guard action, not a natural source/TV disconnect.
The owner protected and re-promoted the candidate at **23:35:43.769 UTC**,
Node PID **31925**, then verified all three sources healthy.

The final TV observation ran **23:36:27.554–23:37:12.763 UTC**, with **no Inspector
query, camera reset, or further deployment action inside the window**:

| TV | Front Door decoder outputs | Garage decoder outputs | Latest age bounds: Door / Garage |
| --- | --- | --- | --- |
| `.85` | 501→1104 | 530→1131 | 46–54 / 59–64 ms |
| `.58` | 500→1104 | 500→1101 | 49–55 / 59–64 ms |

All four used hardware AVC decoding, sampled queues zero, no errors/timeouts/
retries/counter resets, and no new definite 500 ms misses. After the parallel
owner-side gate, the new rollback **timer and service** were both stopped and
verified inactive; Node 31925 and the promoted artifacts remained installed.
This quiet ordinary window does not resolve the confounded reset-time burst or
establish sustained physical smoothness.

## Private provenance and closure

All owned logcat collector handles completed and closed. Artifacts are under
`.evidence/frame-freshness-2026-10-12/`:

| Artifact | SHA-256 |
| --- | --- |
| `tv-scoped-reset-deploy45/summary.json` | `a736eb53cde3db72e4a4267d8a78a626f6699d02eb6ff460ea0809d16d0b49e2` |
| `tv-scoped-front-reset45/exact-garage-late-join.json` | `581d70ca91b6424362d0a77ee752712258f56dae22e3ca39180ef108bb219a95` |
| `tv-scoped-front-reset45/summary.json` | `66f43fc83ee50687786931967ff129ca4387f042230f7a6ac4a9dc3fa6b98689` |
| `tv-scoped-reset-final45/summary.json` | `60ffb9cc8b13fac14fb94ae8728c985456145711a6fb659958b7b6ed8fa88f62` |
| `tv-scoped-reset-final45/manifest.json` | `4acd517e6de830f6cdbf266d6e1c06646f2d87d326053e17ed7c705f41c4a4c3` |
