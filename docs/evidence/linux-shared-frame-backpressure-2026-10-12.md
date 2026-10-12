# Linux same-frame delivery and server backpressure

The temporary exact-tag go2rtc diagnostic was active from 18:57:20 until
19:19:43 UTC on 2026-10-12. It exposed existing sender occupancy
and synchronous socket-write counts/durations. It changed no buffering policy,
media bytes or write-error handling. See
[diagnostic build and tests](../../server/diagnostics/README.md).

## Actual per-consumer accumulation

The first 600-second API sample had 1,199 rows and zero request failures. Garage
queued as many as **92 whole-picture entries** for the Pi while its socket write
blocked up to **2.598 seconds**. The concurrent Mac consumer stayed at sampled
queue zero and max write 17.5 ms; Fire TV .85 reached queue 15 / write 408.8 ms.
The producer is the same raw FLV complete-picture path, not separate encoders.
Queue capacity alone was previously only a hypothesis; occupied entries were
measured here. Zero sampled occupancy does not exclude intermediate queueing.

The continuation sample, 19:09:08.604–19:19:08.218 UTC, also had 1,199 rows and
zero request failures. Pi Garage reached sampled queue 77 and max write
**4.375 seconds**. Its cumulative synchronous write duration reached 52.838
seconds. Door's sampled queue remained zero / max write 130.4 ms. Neither stream
had a write error or short write. Consequently this run does not demonstrate the
previous RTSP parse failure's proposed partial-write cause.

Server TCP statistics also showed Pi receive-window-limited transmission:
Garage had 85,008 ms (32.3% of busy time) at a sampled point, with RTT 352 ms;
Door 15,408 ms (6.5%), RTT 240 ms. These support downstream backpressure but do
not identify radio, CPU scheduling, RTSP read-chain or driver blocking as its cause.

## Exact picture identity across clients

The Pi's phase trace joined the Mac receipt observer by bridge epoch, frame ID,
source word and ingress monotonic timestamp:

| Picture | Mac ingress-to-receipt bound | Pi ingress-to-depay bound | Pi ingress-to-KMS-return bound |
| --- | ---: | ---: | ---: |
| Garage 152774 | 44.641–65.323 ms | 12,335.566–12,381.200 ms | 12,563.295–12,608.929 ms |
| Door 180861 | 70.080–105.885 ms | 8,155.781–9,045.961 ms | 9,050.451–9,342.472 ms |

Both used bridge epoch `aa064658bab11fe0`. Garage's parser added 1.400 ms,
decoder-output boundary 118.114 ms, post-queue 7.318 ms and sink return 100.897 ms.
The large delay therefore precedes depayload completion on this client-specific
path. It is not a shared producer holding these pictures or a deep decoded queue.
Camera capture time and physical scanout remain unverified.

The hardware output counts were about 15 fps; the existing downstream raw queue
discarded roughly half before KMS. That explains why the earlier post-queue
8-fps count was not a hardware decoder throughput measurement. The presentation
cadence and pre-depay accumulation need distinct follow-up measurements.

## Device and observer limits

The Pi uses its USB BCM43143 Wi-Fi adapter on 2.4 GHz, not Ethernet. At a read-only
sample it reported RSSI −40 dBm and 58.5/72.2 Mbps receive/transmit PHY rates,
power saving off. Those values do not prove effective transport throughput.
The optional DietPi Wi-Fi monitor remained active/enabled; it was not stopped.

The phase observer's contribution must be recorded with its final evidence.
An earlier run without that per-stage observer already measured a >11.679-second
lower-bound tail. CMA free pages were scarce, but include movable/reclaimable
allocations; that is not proof of DMA exhaustion. No memory or network policy
was changed on that basis.

Original go2rtc release ELF `32d616af226bd731678ffde328b94cfb94e30339bfefc469cfb76323144615a6`
was restored and the diagnostic rollback timer cancelled. Pi was restored OFF,
with client inactive/disabled and its temporary diagnostic override removed.
All four root collector handles completed normally.

Private metadata artifacts are under `.evidence/frame-freshness-2026-10-12/`:
`go2rtc-queues-writes-600s.jsonl`, `compat-mac-joint-600s.jsonl`,
`go2rtc-queues-writes-pi-continuation-600s.jsonl`,
`compat-mac-pi-continuation-600s.jsonl`. No decoded pixels are in these artifacts.

SHA-256, in that order:

```text
238fbeea27aadf717b96e0f960e9c0ba3fda3c19aba66afb3ecfd74182bb9dca
58029e5cae8e36ed640b5b163880504c2a0aa0728a51d43893eb9499964f6abf
be90b696e8351ceaa803393bf11081e0980e38f05e4ffc0e8c619d00f871f15b
048fa7e021952bfac2740f44fb33cac465251a5497926e0d7d44f9e723d0bd59
```
