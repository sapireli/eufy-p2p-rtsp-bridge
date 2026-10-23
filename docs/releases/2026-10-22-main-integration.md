# Main integration and deployment

Runtime source: `6f996db95a51be40c68e0c5fdcdb1b334e9e755c`.
Deployment performed 2026-10-23 UTC (2026-10-22 local time).

## Integration

Fast-forwarded the 66 release commits from `fix/balcony-decoder-recovery`
onto main, then merged dependency PR #15 (`ws` 8.22.0). Server tests:
325 passed, none failed or skipped. Android: 111 tests passed. Linux:
`go test ./...` and off-device ARMv6 crossbuild passed.

Only main remains as a remote branch. Merged docs and release branches were
removed. Unmerged work was preserved under these pushed tags rather than
included in production without review:

- `archive/2026-10-22/local/chore/upstream-ha-bridge`
- `archive/2026-10-22/remote/chore/upstream-ha-bridge`
- `archive/2026-10-22/plan/plug-and-play-setup`
- `archive/2026-10-22/experiment/linux-arrival-admission-20261013`
- `archive/2026-10-22/feat/pr280-bridge-integration`

PR #9 was closed with its archive location. The setup branch contains substantial
alternate implementation work; the vendor branches would regress the current
poll-completion watchdog if merged wholesale. Dirty worktrees and unpublished
power scripts were left intact. These archived changes are not represented as
merged features.

## Deployment

- Homebridge: source revision recorded, representative source and lockfile hashes
  matched the integrated tree; `ws` 8.22.0 installed. SDK remains pinned to
  `48e4f2f1d0dd5d255d928766cdae585bec6076d0`. Authentication and go2rtc healthy;
  settings bytes unchanged. Private rollback backup:
  `/var/backups/eufy-wall/main-6f996db`.
- Fire TVs `.58`, `.85`, `.100`: fresh integrated build installed and APK hash
  verified. Existing version 0.7.0/code 19 was reproducible from this source:
  `7f444e7789cad4cd8ec0088c3e8d9b38c6075c04d85ebff5b15574e7d6d13cc7`.
  Wall preferences and pairing files remained byte-identical.
- GaragePi: ARMv6 binary installed, SHA-256
  `477d60bb8cb13306807719b80ceed5157ba6e274210d00e60c0c04e56c4c9960`.
  Configuration and pairing preserved; authenticated settings command succeeded.
  Client was inactive and remained inactive, so this is not a playback test.
- DietPi `.128`: SSH timed out twice. Deployment remains incomplete there.

Garage Doorbell initially supplied no media after the bridge restart, then
recovered without a second restart. During the first 60-second TV observation,
all four fixed feeds advanced on all three TVs: Front Door +799–808 frames,
Garage Doorbell +805–820, Balcony +1269–1279, Wyze +822–824. No recorded RTSP
timeouts, player releases, control-loss events or counter resets occurred.
Front Door and Garage Doorbell used hardware decoding; the other two feeds used
software fallback. Balcony had 15–20 queued frames at the end of this window,
so these measurements do not establish perfect latency or eliminate the
previously observed upstream media pauses. Decoder counters are not physical
screen or capture-to-screen verification.

The final snapshot at 02:53:34 UTC extended observation to 240 seconds. All
four fixed feeds continued advancing on all three TVs, with unchanged app PIDs,
preferences and APK hashes. Total rendered-frame deltas were Front Door
+3330–3418, Garage Doorbell +3395–3441, Balcony +5741–5787 and Wyze +3502–3518.
All final decoder queues were zero; the earlier Balcony queues cleared rather
than growing throughout this check. Final recent logs contained no RTSP
timeouts, player releases or control disconnects. The intervening period was
checked by final counters and logs, not a continuous screen recording.
