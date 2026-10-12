# Temporary go2rtc queue/write observation

`go2rtc-queue-observation.patch` applies to official go2rtc tag `v1.9.14`.
It adds metadata only, enabled with `EUFY_GO2RTC_OBSERVE=1`:

- Existing sender queue length/capacity in `/api/streams`.
- Per-RTSP-connection write calls, actual bytes, errors, short writes, total/max
  synchronous socket-write elapsed time.

It does not resize a queue, discard references, change a deadline, handle a write
error differently, or change media bytes. Zero occupancy at a sampled instant is
not proof that the queue was empty between samples. Queue entries may be RTP
packets or whole frames depending on the input; do not equate counts with time
without identifying that producer's actual codec/path.

The purpose is to locate the observed Linux accumulation and distinguish it from
per-client server queueing. The existing ignored-write-error hypothesis is still
unproven for the earlier Pi parse failure.

Reproduction:

```sh
git clone --branch v1.9.14 --depth 1 https://github.com/AlexxIT/go2rtc.git
cd go2rtc
patch -p1 < /path/to/go2rtc-queue-observation.patch
go test ./pkg/core ./pkg/rtsp
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o go2rtc-observe .
```

The actual official tag tarball used locally has SHA-256
`e3d59e553dfd0085889a2956281cfc0fd78bb7b4d6269d1c90c217d2ffcf2c7b`.
The focused suites passed and the Linux amd64 build completed on Go 1.27.1.
Built diagnostic ELF SHA-256:
`41b5ea1f51d87ac65904c1ac83e664e2b2bc7eba98cc5c0bed2a77385ca0898e`.
The current unmodified live release binary is
`32d616af226bd731678ffde328b94cfb94e30339bfefc469cfb76323144615a6`.

A deployment must retain that release binary, bound the observation window and
automatically restore it. Record activation, matched live resource/consumer IDs,
the actual observer scope, artifact hashes and final restoration in the evidence
directory. The diagnostic binary is not a committed production dependency.
