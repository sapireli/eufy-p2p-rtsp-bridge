# go2rtc RTSP write/framing audit — 2026-10-12

This is a read-only source audit of upstream tagv1.9.14, not a demonstrated cause
of the GaragePi parse error. No go2rtc patch, restart, logging change, or camera
setting was made for this audit.

## Write boundaries

[RTSP packet writer](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/rtsp/consumer.go)
collects interleaved RTP packets until a marker and calls `writeInterleavedData`
with one buffer. The TCP path makes one `conn.Write` call for that batch.

[RTSP response serialization](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/tcp/textproto.go)
constructs the complete response and makes one `Writer.Write` call.
[WriteResponse](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/rtsp/conn.go)
sets a deadline then passes the underlying connection to that serializer.
Consequently a request/response concurrent with RTP is not by itself evidence of
bytes spliced inside a write on the normal Go TCP connection. There is no measured
wire corruption or basis for an additional concurrency lock patch here.

## Unproven failure path

`writeInterleavedData` sets a five-second write deadline and returns a write error.
`packetWriter.flushBuf` does not close the connection or propagate that error; it
resets its byte count and continues. If an actual write returned a partial byte
count with an error, then the advertised interleaved packet length could be left
incomplete before subsequent writes. This is a **candidate requiring proof**, not
an observed short write. Ordinary successful complete writes do not establish it.

The Android library's legacy RTP header reader searches for `$`/channel0 when it
fails to recognize an RTP header. Such scanning is not structured RTSP-response
consumption and could amplify an already corrupted stream. No corresponding
Android parse failure occurred in the preceding ten-minute baseline.

A reproduction must retain packet/control framing metadata and actual write
results/errors, correlating station, bridge, RTSP and client monotonic markers.
Encoded camera payload must remain private. The Pi wall clock was approximately
22seconds slow; its wall timestamp alone cannot align the events.

## Available logging

[API log handler](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/internal/api/api.go)
only supportsGET (read current bounded log memory) andDELETE (clear it). There is
no runtime log-level setter in that handler.

[Module log settings](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/internal/app/log.go)
are loaded at initialization. [RTSP tcpHandler](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/internal/rtsp/rtsp.go)
caches whether trace is enabled when the connection is created. The query parameter
`log_level` changes severity of consumer errors; it does not enable request/response
trace or change the module logger.

A genuine request/response trace therefore requires a deliberately configured
child restart and new connections. That was not done: a speculative restart would
perturb the live delivery path before reproducing an actual failure.

The bridge journal18:13–18:18UTC contained no records. A wider18:00–18:30UTC query
returned124 records but no warning in that target interval. Because write errors
can be ignored, absence of logged warnings is not proof of complete successful
writes. Private journal captures are `/tmp/eufy-go2rtc-pi-error-window.log` and
`/tmp/eufy-go2rtc-wide-journal.jsonl`.
