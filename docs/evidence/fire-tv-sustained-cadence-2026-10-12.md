# Fire TV sustained cadence and stage timing — 2026-10-12

## Runtime and method

Both TVs ran APK SHA256
`6d77d83296f119f94bee876d1691010327012a1f58f9548e5039e15f7c8b8455`,
with exact original two-camera preferences, native profile probe off, hardware
`OMX.amlogic.avc.decoder.awesome`, and freshness observation only. No camera quality,
player buffer, codec policy, layout, or server setting changed during this capture.

The ten-minute metadata-only observer started1791827960755 and ended1791828560740
(2026-10-12 17:59:20.755–18:09:20.740 UTC). INFO logs were streamed to files,
not periodically overwritten snapshots. No extra RTSP consumer was added.
DevicePIDs18430/17737 stayed unchanged. The observer children exited at the bound.

One additional15-second streamed DEBUG timing tail ran18:01:24.203–18:01:39.588.
INFO streaming continued independently. DEBUG was restored toINFO on both in a
`finally` block. Instrumentation overhead is not asserted to be zero; some decode
age misses occurred around this tail. Sensor freshness and physical panel timing
were not measured. The cameras' printed clocks were not used.

## Ten-minute result

| TV / camera | Decoded frames | Mean decoded fps | Minimum sampled interval fps | Max compressed queue | Decode age >=500ms |
|---|---:|---:|---:|---:|---:|
| .85 Front Door |8982|14.992|4.799|1|54|
| .58 Front Door |8936|15.016|6.600|0|45|
| .85 Garage |8913|14.977|10.000|0|12|
| .58 Garage |8920|14.989|11.198|0|0|

Sample intervals generally span5 seconds; a delayed INFO poll can span6 seconds.
The per-camera first/last INFO samples span595–599 seconds of the600-second window.
There were no warning/error records, playback retries, PID changes, missing stamps,
pacing drops, or freshness blanking. These results rule out a sustained compressed
queue growing over this window. **They do not establish smooth playback:** delivery
slowed and then caught up despite an approximately15fps long-run mean.

The strongest INFO-only common event is Front Door:

- .85:44 newly observed decode-age misses in18:07:49.940–18:07:54.940.
- .58:45 newly observed misses across18:07:48.771–18:07:58.771.

Both clients saw the same camera exceed the500ms bridge-ingress age bound during
that event. This is evidence for a shared upstream contribution; it does not by
itself identify which bridge/transport stage held the frames.

Ten .85 Door misses and all twelve .85 Garage misses accumulated around the DEBUG
tail (including the following INFO interval). Those cannot be presented as an
unperturbed baseline miss rate. The later common Door event occurred withINFO only.

## Stage timing tail

Receive→decode medians were11.2–11.5ms, p95 values26.3–49.2ms. Current received-frame
gaps were already large: Door maxima873ms(.85)/631ms(.58), Garage1069/832ms.
Decoded-frame gaps closely followed received-frame gaps. A fast decoder cannot
remove gaps before input without adding delay, which was not done.

Per-frame ingress→decode age medians were98–106ms; p95 values222–364ms. The maximum
was656ms on .85 Garage. These are bridge-ingress upper-age bounds, not capture age.

Provided codec render timestamps minus receipt time had medians78–83ms. Provided
render timestamp minus requested presentation target had medians52–60ms, p95
values162–286ms, and maxima609–855ms. Thus some output-surface/platform tails also
remain; this measurement is not a claim that the player contributes no delay.

Informational callback dispatch arrived much later than its provided render
nanotimestamp: estimated medians130–152ms and maxima~1.14seconds. The estimate uses
receipt-log wallclock versus monotonic offset, with millisecond log precision.
Older Android can batch or omit render callbacks; callback counts are not a count
of physical displayed pictures or proof of dropped pictures.

The existing presentation-time wait was bounded by67ms, with median0 in this tail.
No extra playback buffer, restart policy, or output discard was introduced.

## Diagnostic correction

Legacy `pendingMediaMs` was receivedPTS minus the latest informational callbackPTS.
Its observed peak800ms on .85 Garage cannot be called a decoder or physical-display
buffer. It is now named `callbackPendingMediaMs`, explicitly scoped to the callback.
The new `pendingDecodeMediaMs` compares receivedPTS with latest decodedPTS. Neither
metric measures sensor freshness. Tests cover a delayed callback with decoder fully
caught up, unknown startup, and independent microsecond→millisecond progress.
This is a telemetry correction, not a playback fix.

## Private artifacts

`.evidence/bridge-network-audit-2026-10-12/tv-sustained-10min/` contains manifest,
streamed INFO logs, summary, complete timing-tail logs and timing summary. Those
metadata files contain device identifiers and remain private. Public evidence here
uses camera names and stage scope only. Sustained smoothness remains unverified
pending shared-event stage correlation and resolution of observed gaps.
