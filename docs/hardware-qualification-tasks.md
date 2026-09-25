# Remaining hardware qualification tasks

Status: open. Resume this checklist before declaring the plug-and-play release production ready. The software CI and synthetic RTSP trials recorded in the [setup plan](plug-and-play-setup-plan.md) do not measure physical pixels, Raspberry Pi decoder capacity, or real Eufy camera behavior.

## Test inventory and evidence

- [ ] Arrange access to Raspberry Pi 1, 3, 4, and 5 display hosts; a Debian amd64 display host; Intel and Apple Silicon Macs; a Debian bridge host; and a real Eufy test account with wired, battery, and dual-lens cameras. Record exact OS/kernel, model, RAM, output, and network link for each. Keep credentials and full serials out of committed evidence.
- [ ] Choose a tagged, provenance-verified release artifact for each architecture. Record tag, Git revision, artifact SHA-256, installer version, GStreamer version, go2rtc version, and any firmware or decoder packages. The current draft branch and passing CI are not substitutes for a tagged release.
- [ ] Prove automatic decoding prefers hardware for each codec when supported and falls back cleanly to software when hardware is absent. Probe the exact configured codec and profile; record the selected GStreamer element, device/driver, decoded frames, CPU use, and visible output. A plugin listing or software-only CI render is insufficient. Report the fallback visibly in `doctor` and startup logs.
- [ ] Inject a hardware decoder that advertises an element but rejects the active codec or profile on each physical host. The native compositor/window now switches only the affected tile to software after recent compressed input reaches its decoder and decode fails or stalls; adversarial tests cover this and distinguish a publisher outage. Qualify this with actual hardware, including a rejection before any compressed buffer reaches the decoder. The planes runtime still needs equivalent per-tile detection and fallback. A changed source retries hardware; a same-source automatic return needs a bounded successful probe before implementation.
- [ ] Before each fault trial, write the maximum acceptable time to fresh frames and the allowed interruption to unaffected tiles. Measure both with wall status counters **and** an observer watching the display. Record a failed or unmeasured result rather than inferring visible output from pipeline counters.
- [ ] Save one scrubbed report and brief observation log per host under `docs/evidence/hardware/`, then update the measured profile tables in the [Linux/Pi runbook](runbook-client.md), [Mac runbook](runbook-client-macos.md), and [server runbook](runbook-server.md). Include commands, start/end times, tile count, codecs and sizes, output mode, frame rate/drops, CPU/RSS/temperature, recovery times, and any failure. Do not commit RTSP URLs with credentials, account data, raw camera logs, or full device serials.

## Display host matrix

| Host | Required run | Result |
| --- | --- | --- |
| Raspberry Pi 1 / armv6 | One supported H.264 layout, KMS/plane or compositor probe, 30-minute soak, faults below | Unverified |
| Raspberry Pi 3 / armv7 or arm64 image | Measured two-tile layout, decoder/plane probe, 30-minute soak, faults below | Unverified |
| Raspberry Pi 4 / arm64 | Multi-tile layout, H.264/H.265 where the host supports it, 30-minute soak, faults below | Unverified |
| Raspberry Pi 5 / arm64 | Multi-tile HEVC hardware decode and H.264 software fallback, 30-minute soak, faults below | Unverified; Pi 5 cannot hardware decode H.264 |
| Debian x86-64 / amd64 | Multi-tile physical display, 30-minute soak, faults below | Unverified |
| Intel Mac / amd64 | Observe actual window pixels, clean-host launchd/Gatekeeper, sleep/wake, 30-minute live camera run | One-tile soak and four-tile VideoToolbox synthetic trials; unverified |
| Apple Silicon Mac / arm64 | Local window/RTSP run, codec switch, source loss, clean-host launchd/Gatekeeper, sleep/wake, 30-minute run | CI builds/tests only; unverified |

For every Linux/Pi row:

- [ ] Follow the [client install and qualification runbook](runbook-client.md) from a fresh image. Test verified online artifact A, repeat install, upgrade to B, rollback to A, and an offline install with a separately trusted digest. Confirm config, service state, and live frames after each change.
- [ ] Run `eufy-wall doctor --json` and the installed `deploy/qualify-client.py` with a LAN RTSP source. Record connected DRM output, chosen mode, usable planes, decoder elements, probe FPS/drops, and probe process CPU/RSS. A single-stream `fakesink` result is only a decode check; observe the actual wall on HDMI/DRM separately.
- [ ] Apply one hand-written YAML through both a file and stdin over SSH. Then run the interactive wizard from a fresh config. Confirm both routes produce a working two-camera wall and that a bad candidate restores the previous YAML and visible wall. For two outputs, exercise named instances and confirm each renders on its intended connector.
- [ ] Build an arbitrary nonoverlapping 32×32 layout. Compare editor, PNG, and physical display placement at 1920×1080 and one odd-size output. Check tile edges, aspect ratio, blank gaps, and readable diagnostics for a missing decoder or unusable plane.
- [ ] Watch a 30-minute wall run and record frame progress, visible drops, CPU/RSS/temperature, and any thermal throttling. Repeat the layout at a stream count the host can sustain; document the tested maximum rather than assuming the logical grid implies 1,024 streams. On Pi 1, explicitly record any unsupported codec or layout limit.

For both Mac rows:

- [ ] Follow the [macOS runbook](runbook-client-macos.md) as a fresh desktop user. Verify a signed and notarized artifact through normal Gatekeeper behavior; install A, repeat, upgrade to B, and roll back while a window is running. Confirm exact launchd state and visible frames after each action. Do not bypass Gatekeeper.
- [ ] Observe the actual window during a 30-minute live RTSP run, source loss/recovery, a mixed H.264/H.265 motion switch, and a compositor or client restart. Record visible pixels/drops and status counters separately. Test sleep/wake and login/logout recovery with the launchd agent.

## Real bridge and camera matrix

- [ ] On a clean Debian bridge host, follow the [server runbook](runbook-server.md) with a test Eufy account: verified artifact A, setup wizard, manual YAML file/stdin, bad-apply rollback, upgrade B/rollback A, and offline install. Confirm `/healthz`, authenticated inventory, RTSP playback, unit state, and secret-file permissions without recording credentials.
- [ ] Run a 30-minute real-camera wall with at least two cameras. Include a battery `on_motion` tile that sleeps between events, a visible fixed `on_demand` tile whose bounded hold refreshes, and a dual-lens portrait feed. Record power policy, wake latency, hold release, codec, resolution, and source generation. Do not force continuous battery streaming unless its power override is intentionally supported.
- [ ] Switch a `motion: latest` tile between real H.264 and H.265 cameras while another tile keeps rendering. Record decoder selection, time to fresh frames, and whether the unaffected tile drops or freezes. If the hardware cannot decode H.265, record that limit and test only a supported transcode path.
- [ ] Run the fault matrix below with the real bridge and cameras. Check that inventory refreshes after reconnect and that stale on-demand holds are released. Record each device and network failure separately; a local synthetic RTSP publisher does not establish camera or HomeBase recovery.

| Fault to inject, one at a time | Evidence to capture |
| --- | --- |
| Client network loss and return; DNS/RTSP timeout or packet stall | Time from link/source return to fresh frames, retry behavior, unaffected tile progress |
| Bridge process, go2rtc process, and client process restart | systemd/launchd restart, active config, frame progress, no duplicate holds |
| One tile pipeline and the compositor fail or stop producing frames | Frozen-picture detection, affected tile recovery, unaffected tile continuity |
| Camera power loss/error and station/HomeBase restart | Camera state, wake/reconnect time, RTSP resumption, inventory and hold state |
| Auth expiry or 2FA/captcha challenge | Clear operator diagnostic, safe recovery, no challenge answer in URLs or logs |
| Config apply or binary upgrade interrupted at each transaction phase | Previous service/config restored, journal/status reason, safe retry |

## Release decision

- [ ] Attach the measured evidence and mark each runbook profile supported only when its installation, layout, soak, and recovery checks pass. Document any reduced stream/codec limits per host.
- [ ] Configure Apple Developer ID signing and notarization, then verify both Mac archives on clean hosts with Gatekeeper enabled. This is a separate public Mac distribution gate.
- [ ] Keep the draft PR open until the real-device checks, clean-host usability, and release artifact checks above are complete. The current passing CI establishes software behavior on its runners, not hardware support.
