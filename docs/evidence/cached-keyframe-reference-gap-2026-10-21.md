# Cached keyframe followed by missing reference pictures

## Failure and affected path

An ordinary SDK consumer attaching to a warm `SharedLiveSource` received only
its retained keyframe and then current dependent pictures. The intermediate
pictures were omitted. Starting with a keyframe therefore did not make this
bootstrap sequence independently decodable. The same source behavior applies
to H.264 and H.265; its consequence depends on the decoder.

The bridge already waits for a keyframe before beginning a new HTTP consumer.
It could not distinguish this retained SDK prime from a live keyframe. The
prime opened that gate, and subsequent pictures with absent references entered
the transcoder. An earlier liveness fix preserved the prime's original age but
still allowed it to initialize a decoder; that did not repair reference
continuity. The new repair belongs at SDK consumer attachment.

On the deployed Cherryview/i965 host, a captured invalid HEVC bootstrap caused
a shared video-engine hang. All three VA-API transcoders then stopped producing
output, reached their bounded pending-frame limits, and lost their HTTP/RTSP
producers. This accounts for the measured shared interruption. Independent
upstream camera silences remain a separate issue.

## Captured input and causal replay

The exact input copied from a failing production FFmpeg process contained
120 Matroska packets. Packet 0 was an IDR. Packet 1 at PTS 1.371 s had picture
order count 40 and required absent picture 38. The next intact IDR was packet
11 at PTS 2.034 s. The live FFmpeg process reported `Could not find ref with POC
38`, produced no captured stdout, and was eventually killed after the egress
backlog bound. The retained 00:30:59 GPU dump describes HEVC decoding at this
camera's unique 1600×2304 coded dimensions.

A private filtered fixture preserves the original Matroska header and all
clusters from packet 11 onward verbatim. Its 109 coded payloads, PTS values,
sizes and key flags match the original suffix exactly. It was not reencoded.

With the bridge stopped under an independent service-start guard, the same
installed FFmpeg/driver ran three serial **decode-only** tests:

| Input | Decoded frames | Wall time | Kernel result |
| --- | ---: | ---: | --- |
| Filtered suffix, before | 109 | 2.237 s | No GPU hang |
| Original captured input | 120 | 17.739 s | GPU hang/reset in the replay FFmpeg PID |
| Filtered suffix, after | 109 | 2.271 s | No GPU hang |

The original replay reported the same missing POC 38. Its eventual exit code
was zero after the kernel reset, so exit status alone would incorrectly pass
this test. No GPU scaling, encoding, concurrent decoder or client was needed
to reproduce the hang. The bridge was restored at 00:35:58 UTC.

The dump's reported batch address is immediately after `MI_BATCH_BUFFER_END`;
the engine ring contains the following flush/completion breadcrumb. This
locates the observed stall at the decode-completion boundary. It does not
identify an exact faulty hardware instruction or repair the driver's handling
of arbitrary damaged inputs. The causal replay demonstrates that removing the
broken reference prefix removes this reproducible trigger.

## Installed SDK provenance and corrected live path

A four-second observer attached one additional **local** consumer to the
already active SDK source, without another P2P camera session. Before the fix,
its first delivered picture matched the source's cached keyframe by coded
SHA-256, timestamp and original receipt identity. It received sequence 2345
and then dependent sequence 2361: 15 intervening video receipts were absent.
The 97-packet fixture decoded to 83 pictures in ordinary software mode; strict
decoding failed before producing a frame.

After the SDK fix, the same observer attached while cached sequence 665
existed. That retained picture was not replayed. Delivery began at fresh
keyframe sequence 695, followed by 696 and 697. Both ordinary and strict
software decoding produced all 90 captured pictures without errors.
An earlier 15-second observer expired before seeing the target source and
produced no fixture; it is excluded from the result.

## Repair and verification

SDK commit `e2231c562e439a22a7e16acd11c80cb297cc40a3` sets the existing
per-consumer keyframe gate before warming/attaching and stops priming ordinary
consumers with a lone cached IDR. Explicit recording prebuffer attachment keeps
its complete aligned history. Public consumer fields remain compatible.
This adds no steady-state playback buffer and changes no source quality,
output dimensions, decoder preference or encoder settings. A joining consumer
waits for the next live random-access picture, as required for a valid chain.

The fixed SDK bundle was installed at 00:40:30 UTC. Its SHA-256 is
`569200ac0f3a504efba46bca33962e82be96034776542b37f171b6e379d1d0ab`.
All 4,145 SDK tests and the complete SDK verification command passed; all
255 bridge tests passed against that exact bundle. The new bootstrap tests
fail against the original SDK implementation. Hardware-first policy and the
original FFmpeg module hash remain unchanged. Sustained live verification is
recorded below.

## Private artifact hashes and upstream handoff

| Artifact | SHA-256 |
| --- | --- |
| Exact failing producer input | `217abf10cfc4c957a8415225ece5ee8372d61ba117245347556985857bd30008` |
| Verbatim valid suffix | `add430fc1a9bf79e62cd19cfbaea6c61869b078a23e1860ca93d3edee37dbb14` |
| Current GPU dump | `0cb9da825dd989fa438f06cf61463d7efc89603a8187785752c23fa176213585` |
| Live SDK baseline fixture | `d1d8b257b9c7ae492aec1428bc7477e0858c8199bccae0aef01a6373d4690d69` |
| Live SDK fixed fixture | `d591805073530e193ac9ee638488b1acf93e8d1a14b004d5126dbf1921258e64` |

Private artifacts are under `.evidence/three-camera-gpu-fault-2026-10-20/`
and the private `gd-fault-003059`, `gd-latejoin-baseline`, and
`gd-latejoin-fixed-v2` temporary directories. Raw pictures and GPU buffers must
not be included in a public PR. The SDK commit includes its own evidence MD.
An upstream PR should describe invalid late-join reference continuity for all
live consumers/codecs; the observed GPU hang is one concrete downstream
consequence. Existing upstream PR #478 has changes requested, so the SDK's
contribution rules defer opening another PR. This record supports the later
focused handoff.

## Client observation and separate limits

An independent 30-second check at 00:45:49.549–00:46:20.743 UTC found all
three Fire TV process IDs unchanged, every configured stream's rendered count
advancing, and no reconnect, control timeout or player release. Front Door and
Garage Doorbell used hardware decoding and kept up with incoming frames;
sampled queue depths were zero or one. Their periodic decode-age upper-bound
samples were 31–68 ms and 75–445 ms respectively. These are receipt-to-decoder
measurements, not sensor capture-to-screen ages.

Balcony still used software decoding and accumulated client backlog on all
three TVs: maximum sampled queues 29, 59 and 60 access units, with sampled
delay reaching 2.9 seconds. The Wyze software decoder kept up. This is an
unresolved client throughput issue; the bootstrap repair is not evidence that
all playback is smooth or that every individual camera pause is resolved.
GaragePi was reachable but its documented wall service was inactive, so no
Linux presentation verification is claimed from this check.

## Sustained bridge result

The bounded observer completed 601.362 seconds at 00:43:45.007–00:53:46.369 UTC:
292 API/process samples, zero API errors and zero kernel GPU-hang/reset events.
Front Door kept one producer PID and emitted 8,981 pictures; Balcony kept one
producer PID and emitted 14,980. All of those outputs met the existing 500 ms
bridge-receipt budget. Median/max pending counts were 0/2 and 2/4 respectively.
Both remained streaming in every sample. No encoder-frame-backlog failure
appeared in the service journal since the 00:40:30 candidate installation.

Garage Doorbell still had individual source silences and one backwards source
timestamp; its producer changed repeatedly. This is an unresolved upstream
problem, not a clean sustained-camera-playback claim. Across adjacent samples
with the same producer and nondecreasing counters, 7,628 outputs were observed,
7,508 within the receipt budget; 27 reset/missing intervals were excluded.
Those partial-window counts are not total input/output counts or a measurement
of capture-to-screen latency. Its median pending count was one. The recovery
exposure did not reset the GPU or disrupt the stable sibling transcoders.

The accepted change is the SDK bootstrap repair. Both software-decode trials
and the hardware-scaling fallback candidate were rejected and reverted. The
bridge retains the original hardware-first FFmpeg module; no extra playback
buffer or lower source quality was introduced.
