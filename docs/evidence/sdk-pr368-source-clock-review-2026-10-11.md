# SDK PR 368 evidence retained outside the SDK guide site

The 2026-10-11 review accepted the running-host need, two-model measurements,
unit-neutral field and code scope. Its only blocker was a dated host-specific
evidence page under the SDK’s published `docs/` tree. That page is removed from
the upstream PR and deployment fork; the measurements remain in the PR body and
this host repository. The SDK’s generic live-media guide and field contract stay.

This preserves the evidence required for the SDK commits without publishing a
consumer-specific review log as SDK guidance. The original evidence follows.

---

# Live video source timestamp: need, evidence and limits

## The problem experienced by a running host

A live host forwards SDK video to multiple displays. It needs a media clock that
retains the source cadence. Previously, the public video event discarded the header
timestamp after access-unit assembly. The host had encoded bytes and arrival times,
but no source clock to carry through its timestamped output.

Two fallback choices produced distinct measured faults:

- A guessed constant rate made the outgoing clock advance **18.200 seconds during
  11.879 seconds** of delivery in one sample. A timestamp-scheduled player can
  accumulate delay when its media clock runs ahead of real time.
- Arrival-based timestamps tracked network/process delivery bursts. During a later
  simultaneous source/output observation, source cadence remained approximately
  67 ms while output media increments inherited irregular arrival timing.

The timestamp is needed for normal successful delivery as well as recovery from
loss. A receive-buffer or packet-reassembly correction can improve delivery, but
cannot expose a source clock that the public event has discarded. This PR preserves
that metadata. Buffer sizing, pacing, timestamp conversion and presentation remain
host decisions; none is implemented in the SDK change.

## Five-minute passive, concurrent observation

Captured existing SDK video emissions for 300.002 seconds, with no new media
consumer or live-start, quality or mode command. The observer recorded timing and
metadata, forwarded original events immediately, and restored the emitter on exit.
No media payload or identifying device/account data is included here.

Both tested cameras were attached to a HomeBase. Model identifiers describe device
families, not individual device identifiers.

| Measurement                                                               |    T8425 floodlight camera |        T8214 dual doorbell |
| ------------------------------------------------------------------------- | -------------------------: | -------------------------: |
| Video events                                                              |                      4,501 |                      4,500 |
| Source increment median / P95 / P99                                       |               67 / 68 / 70 |               67 / 71 / 73 |
| Arrival increment median / P95 / P99, ms                                  | 61.191 / 121.814 / 151.873 | 60.446 / 123.789 / 262.927 |
| Largest arrival gap, ms                                                   |                    370.964 |                  1,654.864 |
| Source progression / monotonic seconds, interpreting observed ticks as ms |                   1.000096 |                   1.000113 |
| Missing, zero or negative source increments                               |                          0 |                          0 |

The T8214's largest delivery gap advanced the source word by **65 ticks**.
Subsequent pictures caught up. In the overlapping outgoing-stream observation,
source median increments were 67 for both devices, while outgoing media increment
medians were **61.167 / 60.200 ms**, with P99 **158.445 / 279.178 ms**.
Arrival-based timestamps had represented delivery timing rather than source cadence.

These measurements support millisecond cadence on these tested streams. They do
not establish units on every P2P camera or the source clock's physical origin.
The public field is therefore **`sourceTimestamp`**, with unspecified units and
clock origin, preserving the unsigned 32-bit word without interpretation.

## A real consumer and end-to-end benefit

The host now requests object-mode frames, carries source metadata beside Annex-B
bytes, and emits timestamped media using the observed clock scale. For the final
90-second concurrent source/output gate, all **1,309** T8214 and **1,306** T8425
timestamps in their common spans matched exactly: no missing or extra timestamps.
Outgoing increment distributions matched the source instead of the arrival bursts.

This is a working consumer, not only a hypothetical use. The production fork uses
the earlier `sourceTimestampMs` spelling; this PR's review revision removes `Ms`
because untested models' units are not established. The metadata propagation and
the underlying source word are the same. Host presentation changes and measured
display improvements are separate and must not be attributed to this field alone.

Immutable evidence and implementation references:

- [Host frame subscription](https://github.com/sapireli/eufy-p2p-rtsp-bridge/blob/43ac743/server/src/sdk-adapter.mjs)
- [Host metadata propagation](https://github.com/sapireli/eufy-p2p-rtsp-bridge/blob/43ac743/server/src/stream-manager.mjs)
- [Host timestamped egress](https://github.com/sapireli/eufy-p2p-rtsp-bridge/blob/43ac743/server/src/video-mpegts.mjs)
- [Source/output timing measurements and capture methodology](https://github.com/sapireli/eufy-p2p-rtsp-bridge/blob/43ac743/docs/evidence/source-timestamp-transport-2026-10-11.md)
- [Earlier nominal-clock drift measurements](https://github.com/sapireli/eufy-p2p-rtsp-bridge/blob/43ac743/docs/evidence/live-timing-2026-10-08.md)

## Scope and verification boundaries

The SDK copies the first contributing header's timestamp through assembled units
and live video events. The synthetic regression checks split units and unsigned
32-bit wrap. Existing tests retain byte-assembly coverage. The parser and live
stream scheduling are unchanged.

Live evidence used a production fork containing timestamp propagation alongside
other fixes. It is not an isolated hardware run of the exact PR branch, and it
does not establish that all delivery gaps are socket loss or that this change
repairs upstream pauses. No indoor-camera unit measurement is claimed. Source
clock resets/wrap, conversion to a media timebase and any cross-source comparison
are the host's responsibility. Clock origin, absolute capture/presentation time,
and cross-camera synchronization are not established.
