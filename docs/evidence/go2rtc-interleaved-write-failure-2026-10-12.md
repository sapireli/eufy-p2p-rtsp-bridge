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

At this checkpoint, the natural Pi `gst_rtspsrc_loop_interleaved` parse error
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
No successful live reproduction or natural-cause fix is claimed yet.

For upstream, recheck current source and create an isolated branch from its
base. Port the fail-closed transport seam and meaningful regressions without
our temporary diagnostic deployment or unrelated bridge/SDK work. Run upstream's
complete required gate. A production repair must also verify legitimate
receiver reconnection and sibling consumers, preserving native quality and
hardware preference. No upstream PR has been created at this checkpoint.
