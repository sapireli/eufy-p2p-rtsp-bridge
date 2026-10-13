# Native UDP observer failure diagnostics and artifact preservation

## Known failure boundary

The parent reported a bounded 45-second metadata observer run returning exit7, after which a private export command evaluated failure before copying the binary metadata artifact. No authoritative saved orchestration script was found in the targeted private evidence directory; the parent confirmed the command was likely ephemeral. This change does not claim to reproduce or identify that missing script.

The observer previously combined `MSG_TRUNC` and `MSG_CTRUNC` in one `truncations` counter. A failed aggregate count cannot tell which receive buffer was insufficient. It does not establish source UDP loss, camera production delay, or SDK loss.

## Distinct native diagnostics; unchanged strict gate

`udp-headers.c` now reports:

- `dataTruncations`: received messages marked `MSG_TRUNC`.
- `controlTruncations`: received messages marked `MSG_CTRUNC`.
- `truncations`: number of messages with either flag. A message with both increments this aggregate once and both separate counters once.
- Maximum returned data/control lengths and actual supplied buffer sizes.
- Explicit `ioError`, separate from dropped packets, missing timestamps, and truncation.

Any flagged message is still skipped; any aggregate truncation, packet-socket drop, missing timestamp, or IO failure still returns exit7. No buffer size, BPF snapshot bound, parser acceptance, timing, privacy rule, or strict validity gate is relaxed.

The [Linux recvmsg contract](https://man7.org/linux/man-pages/man2/recvmsg.2.html) distinguishes discarded datagram data from discarded ancillary data. The inspected [Linux v6.18 packet-socket implementation](https://github.com/torvalds/linux/blob/v6.18/net/packet/af_packet.c) trims a normal packet-socket skb to the filter snapshot before receive; receive sets `MSG_TRUNC` if that skb still exceeds the supplied data capacity and emits socket control messages separately. This is a source audit, not proof of the exact deployed kernel behavior.

Here the BPF snapshot is at most128 bytes and the supplied data buffer128 bytes. The ancillary buffer holds one `CMSG_SPACE(sizeof(struct timespec))` timestamp. Therefore a normal filtered message should fit the advertised data capacity. Additional or differently-sized ancillary messages could exhaust control capacity; a larger effective receive skb could exhaust data capacity. These are distinct possible mechanisms, **neither identified as the actual failure cause**. The old merged statistic and lost artifact cannot retrospectively distinguish them. Returned lengths do not necessarily reveal the original required sizes.

## Failure-preserving wrapper

Run the reusable wrapper on the observer host with a **fresh destination**:

```sh
python3 capture-udp-metadata.py /absolute/native-udp-headers eth0 192.0.2.1 45 /private/new-capture --video-header
```

The example address is documentation-only; no capture was run in this task. Optional `--trigger-port` retains the native bounded event-trigger behavior. The requested duration remains1..45 seconds.

The wrapper creates a private0700 destination and temporary staging directory, runs the trusted metadata observer with child umask077, then exports every available fixed metadata artifact, stderr statistics, stdout, and separate exit/manifest records **before** evaluating native success. Final files are0600 and existing destinations/files are never overwritten. Only the native tool's fixed40/56-byte metadata output is exported; packet snapshots and media bytes are not stored.

A nonzero exit, timeout, absent statistics, or partial last record remains invalid but its available artifacts survive. Export failure preserves the staging directory and records its path. Staging is removed only after completed export. The wrapper returns the native failure status (or a strict invalid/export failure status), so artifact preservation does not turn a failed capture into a valid one.

When invoked remotely, the caller must retrieve the retained destination even after a nonzero wrapper exit, then inspect `capture.exit` and `manifest.json`. It must not use an exception-producing success check before that retrieval. This wrapper preserves artifacts on its execution host; it is not an SSH transfer implementation.

## Offline verification and limits

Eight host-only tests pass, including a real synthetic subprocess/CLI returning7, metadata/statistics/exit export before cleanup, strict invalidity despite preserved output, timeout and partial-record retention, destination collision, export failure retaining staging, actual native flag classification, and actual native statistics-format compilation. Existing actual parser/video-header privacy fixtures also pass. Synthetic subprocess bytes are fixed metadata zeros, not camera payload.

The whole Linux observer was not compiled or run on a device during this task. No bridge/SDK/client/configuration/session/interface/device changes or new live probes were made. A future bounded probe must still meet the existing CPU/drop/truncation gate before its timing evidence is treated as valid.
