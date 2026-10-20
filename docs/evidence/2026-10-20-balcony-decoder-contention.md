# Balcony decoder starvation and reference-safe queue recovery

## Scope

Fire TVs `192.168.23.58` and `192.168.23.100`, Android app 0.6.5 before the change and 0.6.6/15 after it. Same Front Door, Garage CLE, Balcony, Wyze and battery-only motion-tile configuration. The motion tile was idle during the comparison. No bridge, Eufy SDK, camera-quality or rendition configuration changed.

## Reproduced failure

Both TVs had Front Door and Wyze on `OMX.amlogic.avc.decoder.awesome`; Garage and Balcony were on `OMX.google.h264.decoder`. Balcony has a 1920x1080 compatible H.264 stream. Its per-frame logs reached 55–60 queued access units and roughly 3 seconds from access-unit receipt to decoder output. A later warning on .58 at 12:00:10.925 was:

```text
RTSP T8423T60241702B1: No decoder output for 5928ms
 decoder=OMX.google.h264.decoder receivedUnits=471 decodedFrames=249
 queuedUnits=0 pendingDecodeMediaMs=8883
```

This proves the displayed message was a decoder-progress watchdog, not an assertion that the TV had no installed codec. The source implementation offered each compressed picture to a bounded 60-frame queue and ignored a failed offer. Dropping a reference picture while continuing dependent pictures can prevent output until an independently decodable picture arrives. Queue regressions reproduce that defect without a device.

## Changes and mechanism

1. On video-queue overflow, clear the obsolete picture chain and wait for an actual AVC IDR or HEVC IDR. Configuration-only packets, false keyframe flags and HEVC CRA do not open the gate. Restore cached SPS/PPS/VPS when absent from the recovery IDR. Normal admission preserves the same frame object; capacity stays 60. Audio behavior is unchanged. This does not make an overloaded software decoder faster.
2. After a real hardware resource-exhaustion error, a larger coded picture may reclaim the same hardware decoder from a smaller coded picture. Each stream still attempts hardware before software. Pixel area is a cost heuristic, not a measured frame-rate or complexity model. Serialize hardware initialization through registration so simultaneous requests cannot miss owners still starting. Equal-size pictures do not evict one another. The evicted tile releases its codec before the requester retries, and reconnects through the usual hardware-first/software-fallback path without incrementing the app-wide decoder-failure recovery counter.

The live corrected allocation was Balcony and Wyze in hardware, with smaller compatible Front Door (784x1080) and Garage (960x1080) in software. No stream was removed or reduced in quality.

## Live proof of allocation

On .58 at 12:02:08.366, initialization returned:

```text
Failed to initialize OMX.amlogic.avc.decoder.awesome, error 0xfffffff4
Rebalancing hardware OMX.amlogic.avc.decoder.awesome:
 1920x1080 replaces 1036800 coded pixels
12:02:08.472 Garage: Hardware decoder reassigned to a larger stream
12:02:08.593 Live decoder OMX.amlogic.avc.decoder.awesome started (HARDWARE)
```

`0xfffffff4` is native ENOMEM (-12). Reallocation is restricted to MediaCodec insufficient-resource (1100), ENOMEM (-12), or EBUSY (-16) failures; unsupported-format and other errors retain ordinary fallback. A three-second release wait bounds startup contention. Another concurrent initializer can still win a released slot before a waiting request retries; fallback remains bounded and safe, so the preferred assignment is not guaranteed for every simultaneous workload.

## Controlled comparison

Updated .58 was compared with unchanged .100 against the same running bridge. Between 12:02:08 and 12:02:47, the unchanged TV recorded two Balcony no-output watchdog reconnects. Updated .58 recorded none. Its hardware Balcony queue remained empty in the sampled steady-state reports and frame age settled near 100 ms. Then .100 received the same APK, preserving its preferences.

The measurement below is bridge-ingress to decoder output, not capture-to-photons. Values are conservative upper bounds sampled from five-second status reports; rendered callback timestamps are informational, not scanout proof. Startup ages can include pre-existing source/network delays. Do not interpret a successful short comparison as proof against every future upstream stall.

### Sustained run after the controlled comparison

.58 ran 12:02:08–12:05:03 (about 175 seconds); .100 ran 12:03:07–12:05:02 (about 115 seconds). All four fixed streams advanced with zero no-output watchdog reconnects on either updated TV. The idle battery-motion tile did not exercise an additional decoder.

Median conservative decoder-output age after excluding the first three five-second status samples:

| Stream | .58 | .100 | Decoder |
|---|---:|---:|---|
| Front Door | 277 ms | 315 ms | Software |
| Garage | 303 ms | 341 ms | Software |
| Balcony | 99 ms | 118 ms | Hardware |
| Wyze | Unmeasured (no stamps) | Unmeasured (no stamps) | Hardware |

Balcony's sampled maximum application queue was 0/.58 and 1/.100; its longest reported rendered-frame gaps were 383/733 ms, including startup. The other feeds did not become uniformly smooth: Front Door ended at 1,731/1,178 ms ingress age, and individual gaps reached 2,702 ms on .58 and 2,050 ms on .100. Garage briefly had 12 queued units on .100. These figures are material limits, not a claim that every stall was fixed. No persistent 60-picture queue or no-output reconnect recurred in this short corrected run.

## Tests and limits

- App: 102 JVM tests, zero failures; APK build passed.
- Vendored RTSP client: 31 JVM tests, zero failures, including seven overflow/configuration/concurrency regressions.
- Same 60-frame queue bound; no periodic reconnect, buffer enlargement, resolution reduction or compressed-frame catch-up outside overflow recovery.
- Camera still produces at its existing rate. The change addresses decoder resource allocation and reference-chain loss; upstream pauses remain a separate cause to investigate when observed.
- Software-decoded Front Door/Garage still showed individual frame gaps during the live run. This evidence does not establish perfectly smooth playback for the entire wall.
- Linux was not changed: this issue is in Android MediaCodec allocation and the vendored Android RTSP queue.
