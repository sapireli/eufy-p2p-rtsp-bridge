# Fail closed after incomplete RTSP/TCP writes

## Candidate defect and scope

The official go2rtc **v1.9.14** `pkg/rtsp/consumer.go:writeInterleavedData`
sets a five-second deadline, calls TCP Write, ignores its returned byte count,
and returns the error. Its packetWriter flush clears its buffer even on error;
the caller continues accepting subsequent frames. A TCP timeout can transmit a
prefix of an interleaved message. Appending the next message after that missing
tail desynchronizes the receiver's length-delimited `$` framing. This is a
specific protocol recovery defect, not proof that every Pi parse error has this
cause. No SDK, codec, camera quality, buffering, or deadline change is proposed.

Official source provenance was independently fetched and byte-compared with
the isolated audit checkout. Key SHA-256 values:

| Official v1.9.14 file | SHA-256 |
| --- | --- |
| `pkg/rtsp/consumer.go` | `d8f5a44e14224c335fc9b1af1c9e2b632dd1d4ea6591f25c6ff420fe5e489ab6` |
| `pkg/rtsp/conn.go` | `3024fd657972f70c2463eb99fbd5b3be5b37278e1a6aeeb52a0b4d732468d7f9` |
| `pkg/rtsp/client.go` | `31d7520f98478f37e222a2adbbddc73bdd1ea61605b58790e27b00fe183f10b5` |
| `pkg/tcp/textproto.go` | `ec03263714caa5361c4923efd816ee694e734a67c847856305582c4c32ab3638` |

[Official consumer source](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/rtsp/consumer.go)
and [TCP control serializer](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/tcp/textproto.go)
show that each RTP batch and control response is a single TCP Write. Ordinary
TCP segmentation is valid; the defect is continuing after an incomplete failed
Write, not splitting data across multiple network packets.

## Isolated base failure and candidate proof

The actual packetWriter was driven with synthetic RTP only. A deterministic
connection accepted7 bytes of a36-byte message, then returned an error. The base
appended the next36-byte frame:43 wire bytes, with the first RTP length still
32. The stream now contains the next header inside the first incomplete payload.
The same defect occurs for a short write returning no error. An actual `net.Pipe`
with a20ms test-only write deadline independently accepted the7-byte prefix,
timed out, then received the entire next36-byte frame on the base. This is a
real net.Conn timeout path, not only a mocked return value.

The tracked seven-case regression covers prefix error, short-nil-error,
zero-byte error, normal complete frames, actual net.Pipe prefix timeout,
concurrent queued control response, and redial lifecycle. Five fault cases
fail on the clean official base; complete-write/redial are safety cases that
also pass on the base. The candidate passes all seven, including under race
checking. The RTSP, TCP and core package suites pass. This is scoped package
validation, not the entire upstream repository test gate.

The candidate closes the low-level transport on any failed/incomplete TCP
Write, before allowing another writer to proceed. One per-connection mutex
serializes media, RTSP requests and RTSP responses together; failed transport
state rejects queued writes. Dial resets the flag for a genuinely new socket,
so reconnecting an active RTSP producer is not permanently disabled. The UDP
media path is unchanged. No remainder retry, extra buffer, delay, quality change,
or new timeout is introduced.

**Cleanup safety:** the sender closes `c.conn`, not `c.Close()` or an OnClose
callback. Closing the transport unblocks the ordinary connection read loop,
whose existing consumer removal owns cleanup. No Sender.Close/Wait callback
runs inside its own handler. A queued control-response regression verifies
no append after the partial media write. The existing send-byte metric is not
used as evidence of bytes delivered; the proofs use returned Write counts and
observed synthetic wire bytes.

## Reproducer artifacts and binaries

`server/patches/go2rtc-v1.9.14/` contains independent alternatives:

- `diagnostic-only.patch`: preserves the baseline continuation behavior and logs
  only the first write failure per connection (peer, written/expected integers,
  error string). No packet/pixel/key/account bytes or stream URL are logged.
- `fail-closed-write.patch`: candidate behavior plus the same sparse metadata.
- `interleaved_write_failure_test.go`: copy into the clean source's pkg/rtsp.

Apply **one** patch to a clean v1.9.14 tree, not both on top of each other.
Baseline meaningful failures can be checked by copying only the test file;
then apply the candidate and run:

```sh
go test ./pkg/rtsp ./pkg/tcp ./pkg/core -count=1
go test -race ./pkg/rtsp -run 'TestInterleaved|TestDialResets' -count=1
```

All work was in `/tmp/go2rtc-rtsp-write-audit`; the shared original checkout and
running primary bridge were not changed by this agent. Built linux/amd64 artifacts:

| Build | SHA-256 |
| --- | --- |
| Sparse diagnostic baseline | `d18e6c823ec641f9e7df17ed1753c0d26833ee391cfeea9d6b9aafa7d05b79a5` |
| Fail-closed candidate, including Dial reset | `7f0858a30dfd8d5450b78458f1abb3dc3240f91165191646b23d65cbf785c585` |

Private base/candidate test outputs and official-source identity manifest are
under `.evidence/bridge-network-audit-2026-10-12/`.

## Live evidence boundary and upstream handoff

At the original structural checkpoint, the natural Pi `gst_rtspsrc_loop_interleaved` parse error
has **not** been joined to an actual server partial Write. The bounded source
journal showed no RTSP error, but default go2rtc logging suppresses debug
handle/disconnect diagnostics. The Pi's old RTSP sockets had already closed,
so later socket counters cannot prove their prior send/window behavior.

The root is coordinating an isolated sidecar using the existing compatible
RTSP producer; this shares the SDK source and adds no encoder. A bounded
controlled reader pause can test whether real camera traffic plus backpressure
creates this server partial-write/continuation path. Such a forced timeout is
**controlled reproduction**, not evidence that backpressure caused the earlier
natural failure. Main service/binary/TV ports must remain unchanged during it.
The later controlled trial below supersedes the live-reproduction status. No natural-cause fix is claimed.

For upstream, recheck current source and create an isolated branch from its
base. Port the fail-closed transport seam and meaningful regressions without
our temporary diagnostic deployment or unrelated bridge/SDK work. Run upstream's
complete required gate. A production repair must also verify legitimate
receiver reconnection and sibling consumers, preserving native quality and
hardware preference. No upstream PR has been created at this checkpoint.

## Later controlled actual-camera backpressure proof

The root deployed isolated sidecars using the **same existing Garage compatible
RTSP producer**, not another SDK source or encoder. Main bridge service/binary,
TV ports, camera quality and producer policy stayed unchanged. This was a
controlled Mac reader experiment, not Pi validation or a spontaneous fault.
The reader requested an effective16,384-byte receive buffer, read for about8s,
then deliberately stopped reading for20s and resumed. Both builds used the same
existing source, sidecar RTSP port, packet-size query, pause, and buffer setting.
These reader settings were test-only, not new production defaults.

| Sidecar | Binary | Actual write failure (UTC) | Written/expected bytes | After reader resumed |
| --- | --- | --- | ---: | --- |
| Diagnostic baseline, PID28387 | `d18e6c82…` | **21:31:46.171547** | **280,873/538,481** | `ValueError` |
| Fail-closed candidate, PID28473 | `7f0858a3…` | **21:36:42.244886** | **232,966/544,576** | `ConnectionResetError` |

Both journal messages report `i/o timeout`. The baseline left257,608 expected
bytes unwritten, then retained its continuation behavior; the controlled reader
failed after its20s pause. **Only the exception type was recorded for that
baseline run**, so the exact malformed-message reason is unknown. Do not label
it as a specific GStreamer parse error or reconstruct a missing exception text.
The candidate left311,610 bytes unwritten and closed its transport. After resume,
the reader reported a connection reset rather than continuing the corrupted
TCP stream. The source-backed fail-closed path and isolated regression establish
that no later frame is appended on that failed transport.

The baseline reader paused at elapsed8.004294s and resumed28.013080s; candidate
8.457962s/28.462806s. Raw pixels, RTP bodies and dynamic exception messages were
not saved. The helper's later allowlisted-static-reason field is
`unclassified-no-payload-logged` for the reset. Source-word stamps, where used,
remain bridge-ingress/media-timeline metadata, not sensor capture clocks.

**Scope:** this is a real camera + real TCP + deliberately imposed backpressure
witness of partial writes and candidate recovery behavior. It supports the
protocol-preserving repair. It does **not** prove the earlier natural Pi parse
error was caused by partial writes, nor quantify normal camera smoothness,
production receiver reconnection, or sibling stability. Those remain separate
live gates; the root's normal-reader45s check and protected deployment were still
pending when this documentation update was made. No SDK change is involved.

Private provenance:

- `rtsp-write-baseline-controlled.jsonl`, SHA-256
  `9c6eb17ccb79c764cd65a9585015dd69a03aba81f96da3b127ed04a68725c4e0`.
- `rtsp-write-candidate-controlled.jsonl`, SHA-256
  `2acda89c48e4e0fc3d3ef62747ffd358c2ee75f78c8836848a6ea083c484c6d7`.
- `rtsp-write-controlled-sidecar-journal.jsonl` (bounded baseline unit), SHA-256
  `d0d8fb504825a663317028a9bd07e19d8069304d1e84b734413d84b5020bf2d9`.
- `rtsp-write-candidate-sidecar-journal.jsonl` (candidate PID-specific), SHA-256
  `702b6d3f200660825c5d11b1f3bbf28e4ce314e093deba30d3e27ce1d839dcb2`.

Reader records/helper are under private `.evidence/frame-freshness-2026-10-12/`;
journals are under `.evidence/bridge-network-audit-2026-10-12/`. The exact full
binary hashes and upstream source hashes are recorded above. A future upstream
PR should include both the synthetic base-failing regression and this bounded
controlled witness, with the spontaneous-cause limitation intact.

## Ordinary disconnected-viewer ownership regression

A focused offline regression exercises the real `Stream.AddConsumer`,
`Stream.RemoveConsumer`, `Producer.GetTrack`, `core.Receiver`, and sender fan-out
paths with one synthetic internal producer and three viewers. Removing the
disconnected viewer leaves the two healthy viewers receiving the next nine
sequence-identified frames from the same producer. Removing one of those still
leaves the final viewer receiving the next frame; the producer stops exactly
once only after the final viewer is removed. The removed viewer receives no
later frame. **The regression passes with race checking.**

The fixture is `consumer_lifecycle_test.go`; copy it into `internal/streams`.
The tag's existing `stream_test.go` does not compile: it expects one return
from New/GetOrPatch/Patch whose production signatures return two. The first
whole-package attempt failed on those four existing signature mismatches;
it is not a passing full-package gate. The focused race result instead used
all unchanged non-test Go files in that directory plus only this regression,
via `go test -race -count=1 <non-test files> consumer_lifecycle_test.go`.
That explicit source-scoped command passed. No unrelated upstream tests or
production lifecycle files were changed to make it pass.
It verifies ordinary cleanup ownership, not the exact chronology or cause of
the later21:47 real-camera interruption. No lifecycle source fix was needed or
claimed. No live intervention, new capture, or network-fault experiment was
performed for this test.

## Normal paired path and reproducible update gate

A later45-second non-paused Mac observation received **the exact same688 unique
frame IDs** through the main Garage stream and candidate sidecar, no extras or
missing IDs. Both paths had zero invalid/missing stamps, abandoned units, and
transport-invalid frames, with identical23,168,338 received bytes. Upper ingress
age median/max were123.425/247.690ms main and125.239/252.422ms candidate.
This is same-window delivery equivalence, not a comparison to an earlier
source window or a sensor/presentation-clock guarantee. Private paired artifact
`rtsp-write-main-candidate-paired-45.jsonl` records the exact ID join; SHA-256
`8322d9395da24610d8b7ccbfae80ae6e4052a5cebcbd83245c8517b0044512db`. The prior
single candidate run with571 frames was a different source window and is not an
FPS or loss comparison.

The new offline builder downloaded the pinned official source archive, applied
the tracked patch, copied the regressions and passed RTSP/TCP/core plus focused
streams lifecycle gates. With Go1.27.1 it cross-built **exactly**
`7f0858a30dfd8d5450b78458f1abb3dc3240f91165191646b23d65cbf785c585`,
the reviewed candidate. An actual cache-only install into a temporary path also
verified this SHA and wrote immutable provenance. The installer/cache regression
passes seven fault/success cases: bad archive checksum, changed patch, corrupted
cached binary, wrong manifest SHA, malformed manifest, injected final rename
failure, and verified compiler-free cached install. Shell syntax checks pass.
No full installer, main-service restart, or live binary replacement was run by
this agent for these gates. Root deployment/rollback and real Pi reliability
remain separate, including the later21:47 interruption under investigation.

## Production trial withdrawn; stock default restored

The user reported many new Fire TV disconnects during the production candidate
trial, which were absent in their prior experience. The root restored the
original go2rtc binary (SHA prefix`32d616`) at **22:00:47 UTC on12 October2026**
and canceled the candidate rollback timer. This checkpoint is reported by the
root deployment owner; this documentation task performed no live change.
There is no quantified before/after user-disconnect rate or established cause
here. The later Garage interruption is not explained by the synthetic ordinary
viewer-removal regression.

The structural partial-write proof, controlled backpressure test, paired688-frame
sidecar equivalence and reproducible build remain valid within their scopes.
They do **not** establish that the candidate is safe as a production default.
Consequently the installer returns to the stock release path; the updater skips
candidate compilation/cache transfer unless `GO2RTC_WRITE_FIX=1` is explicit,
and forwards that choice to the remote installer. The builder/reproducer remain
available for a separately authorized future trial; no ordinary update may
silently reintroduce the withdrawn candidate.

Focused verification exercised both installer-selection paths and both updater
paths with stubbed external commands, plus failed stock-download preservation
and invalid flag rejection. The gating test and shell syntax checks pass.
No live installation, source restart, fault test, capture, or broad rerun was
performed for this correction. Production regression causation remains unresolved.
