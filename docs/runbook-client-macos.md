# macOS client runbook

The macOS client is a per-user windowed wall. It uses `sink: window`, Homebrew GStreamer, and a launchd agent in the logged-in user's GUI session. It does not use systemd, apt, DRM planes, or `sudo`. Intel (`amd64`) and Apple Silicon (`arm64`) archives are built. Intel has bounded local H.264 recovery trials and a 30-minute synthetic window soak; neither display profile has a completed release qualification run.

## Install a verified release

Install [Homebrew GStreamer](https://formulae.brew.sh/formula/gstreamer) with `brew install gstreamer`. The current formula includes the GStreamer plugins. The installer requires GStreamer 1.20 or later; check `gst-inspect-1.0 --version` before setup. It also checks `gst-launch-1.0`, `compositor`, `autovideosink`, `watchdog`, `appsink`, `appsrc`, `videoconvert`, `videoscale`, and `videorate`. Auto decoding prefers VideoToolbox's hardware-only `vtdec_hw` for each codec, then uses libav software decode if that element is unavailable. The app elements relay each tile's frames to a persistent compositor input; video conversion, copying, and composition still use CPU. The installer checks `libgstapp-1.0.dylib` for the native relay. Install a recent [GitHub CLI](https://cli.github.com/) and authenticate it as your login user (`gh auth login` or `GH_TOKEN`) for online attestation lookup. Run the commands below in Terminal in that user's desktop session, with no `sudo`.

For test deployments, open the [tagged release workflow run](https://github.com/sapireli/eufy-p2p-rtsp-bridge/actions/workflows/release.yml) and copy its run ID. Mac archives are uploaded as Actions artifacts named `client-macos-amd64` and `client-macos-arm64`. They are deliberately excluded from the public stable release assets until Developer ID signing, notarization, and clean-host checks pass. Replace `RUN_ID` and `vX.Y.Z` below with that run and its tag. The fail-fast subshell verifies GitHub provenance before generating a local checksum file or extracting installer scripts.

```sh
(
set -e
VERSION=vX.Y.Z
RUN_ID=PASTE_TAGGED_WORKFLOW_RUN_ID
case "$(uname -m)" in x86_64) ARCH=amd64 ;; arm64) ARCH=arm64 ;; *) exit 1 ;; esac
FILE="eufy-wall-$VERSION-darwin-$ARCH.tar.gz"
gh run download "$RUN_ID" -n "client-macos-$ARCH" -R sapireli/eufy-p2p-rtsp-bridge
gh attestation verify "$FILE" --repo sapireli/eufy-p2p-rtsp-bridge \
  --signer-workflow sapireli/eufy-p2p-rtsp-bridge/.github/workflows/release.yml \
  --source-ref "refs/tags/$VERSION"
DIGEST=$(shasum -a 256 "$FILE" | awk '{print $1}')
shasum -a 256 "$FILE" > SHA256SUMS
tar -xzf "$FILE" eufy-wall-client/deploy/install-client-macos.sh
bash eufy-wall-client/deploy/install-client-macos.sh \
  --artifact "$FILE" --checksums SHA256SUMS --trusted-sha256 "$DIGEST" --verify-only
bash eufy-wall-client/deploy/install-client-macos.sh \
  --artifact "$FILE" --checksums SHA256SUMS --trusted-sha256 "$DIGEST"
)
```

The rootless installer stores versioned binaries under `~/Library/Application Support/eufy-wall/releases/`, points `current` to the active release, and links `~/.local/bin/eufy-wall` to `current/eufy-wall`. Add `~/.local/bin` to your shell `PATH`, or call the binary by its full path. It copies `config.example.yaml` for reference but leaves active `config.yaml` absent. It writes `~/Library/LaunchAgents/com.eufy.wall.plist` with stable paths, Homebrew binary and library lookup, and private logs under the application support directory. A fresh install disables the launchd label until a successful setup or config apply. Repeating the same archive and digest leaves the service alone.

If the Mac cannot reach GitHub, verify the archive and attestation on a connected trusted machine, then transfer the archive, `SHA256SUMS`, installer, and verified SHA-256 through a separate trusted channel. On the Mac, use the two installer commands above with `--trusted-sha256 VERIFIED_DIGEST`; no network is needed once Homebrew GStreamer is present. A checksum copied alongside an unverified archive is not a trust root.

For local attestation verification instead, obtain an offline bundle and trusted root on that connected machine with `gh attestation download "$FILE" -R sapireli/eufy-p2p-rtsp-bridge` and `gh attestation trusted-root > trusted_root.jsonl`, then transfer both through a trusted channel. On the Mac, use `--attestation-bundle 'sha256:ARTIFACT_DIGEST.jsonl' --trusted-root trusted_root.jsonl` in place of `--trusted-sha256 "$DIGEST"` for both installer commands. This path requires a GitHub CLI version with offline attestation support; the installer rejects a missing half of the pair. The bundle and trusted root must come from the connected trusted machine, since an attacker who supplies their own root can forge verification.

These archives have GitHub workflow attestations but currently lack a configured Apple Developer ID signing and notarization step. [Apple requires Developer ID signing for Gatekeeper distribution](https://developer.apple.com/developer-id/). Treat the Mac archives as test artifacts until that release gate and clean-host Gatekeeper test are completed; do not instruct users to bypass Gatekeeper. Once signed artifacts are published as release assets, use the same verify-first sequence with the published `SHA256SUMS` instead of generating a local manifest.

### Clean-host installer acceptance

This remains unrun on both architectures. Use a fresh login user on each physical Mac, two independently verified tagged artifacts (A and B), and a reachable local test RTSP stream. Record OS/chip, artifact digests, installer exits, `current` target, plist hash, launchd enabled/loaded state, window frame progress, and any Gatekeeper prompt. CI runs transaction tests and an integration test of the packaged binary and manual YAML with a fake `launchctl`; neither exercises the real user's launchd session or satisfies this gate.

1. Install verified A. Confirm `config.yaml` is absent, `config.example.yaml` is present, `current/VERSION` is A, the plist and `~/.local/bin/eufy-wall` use stable `current` paths, and `launchctl print-disabled "gui/$(id -u)"` reports `com.eufy.wall` disabled. Reinstall A and confirm the plist hash and launchd state do not change.
2. Run setup or `config apply` with a valid local stream. Confirm the agent is enabled and loaded, a window opens, and its frame counter advances. Deliberately apply a config that fails its frame probe; confirm the previous YAML and running window return.
3. Save the plist hash and install B while the window is running. Confirm B is active with advancing frames, then use B's installer `--rollback` and confirm A, the exact prior plist hash, and advancing frames. Repeat the upgrade while the agent is deliberately unloaded; it must remain unloaded.
4. Repeat A on a separate clean user with network disconnected after transferring an independently verified artifact and digest. Run `--verify-only` first. A wrong digest must fail before creating a release or plist. Signing and notarization must be configured before a clean-host Gatekeeper installation can pass the public distribution gate.

## Setup and custom YAML

Run the versioned binary from your logged-in desktop session:

```sh
"$HOME/Library/Application Support/eufy-wall/current/eufy-wall" setup
"$HOME/Library/Application Support/eufy-wall/current/eufy-wall" doctor --json
"$HOME/Library/Application Support/eufy-wall/current/eufy-wall" status --json
launchctl print "gui/$(id -u)/com.eufy.wall"
```

The wizard writes `~/Library/Application Support/eufy-wall/config.yaml`, starts the agent, and enables it for later logins only after its health check succeeds. For hand-written YAML, start with `eufy-wall config example > wall.yaml`, select `sink: window` and a supported decoder, then run `eufy-wall config validate wall.yaml`, `eufy-wall layout preview wall.yaml`, and `eufy-wall config apply wall.yaml`. Standard input works through `config apply -`. See [client config reference](config-client.md) and [layout reference](layouts.md). The launchd plist points to the stable `current` binary and the active config path. It does not hold a copy of your YAML.

## Upgrade and rollback

Download and verify the next tag, then run that archive's installer with the same `--artifact`, `--checksums`, and trusted digest options. It snapshots the previous plist and active release, switches the `current` link, restarts a loaded agent, waits for a stable launchd PID, then runs `eufy-wall health` to verify display and live-tile frame progress. If activation or frame progress fails, it restores the old binary and exact plist. Run `bash eufy-wall-client/deploy/install-client-macos.sh --rollback` to select the previous release later. The installer preserves an unloaded agent's state. Configuration apply has its own YAML backup and health rollback in the client binary.

Inspect `~/Library/Application Support/eufy-wall/wall.err.log`, `eufy-wall status --json`, `eufy-wall health`, and `launchctl print "gui/$(id -u)/com.eufy.wall"` if the window does not appear. Use the client setup probe and observe the actual window after every Mac upgrade; record Intel and Apple Silicon results separately.

## Qualification evidence

An independent Intel window recovery trial ran locally on macOS 15.8 (24H23), x86_64, Intel Core i5-8500 3.00 GHz, and Homebrew GStreamer 1.28.7_1. MediaMTX 1.21.1 served an FFmpeg 9.0.2 H.264 `testsrc2` stream at 640×360 and 15 fps over TCP RTSP. A one-tile, 32×32 full-canvas config used `sink: window`, software decode, and a 320×180 window. Two publisher stop/restart cycles each held the source absent for 45 seconds through multiple failed reconnects. The pipeline output counter advanced 33→708 and 965→1637 during the outages, with worst sampled output-frame age 1.39 seconds. Decoded frames resumed on source generations 4 and 7, about 17 seconds after each publisher restart. The client and publisher exited cleanly. A prior four-second smoke also opened the window and reported 17 output and 15 decoded frames.

An independent 30-minute Intel window soak used the same host, local MediaMTX, and a 640×360, 15 fps H.264 `testsrc2` source with one 320×180 window tile. Across 180 ten-second samples, output frames advanced 33→27,033 and decoded frames 16→27,020. Minimum sampled output and decoded rates were 14.5 and 14.4 fps; maximum sampled output-frame age was 0.752 seconds. Peak client RSS was 70,232 KiB (68.6 MiB), maximum sampled CPU was 6.1%, and there were no source retries or health failures. The client exited cleanly after SIGTERM. The [committed result summary](evidence/macos-intel-2026-09-25-soak.json) records the exact measured fields and scope.

A separate Intel four-tile window trial on this host used `decoder: auto` and a 1280×720 window with four 640×360, 15 fps H.264 local RTSP feeds. FFmpeg looped public [MDN flower](https://interactive-examples.mdn.mozilla.net/media/cc0-videos/flower.mp4), [W3C Sintel](https://media.w3.org/2010/05/sintel/trailer.mp4), [W3C Big Buck Bunny](https://media.w3.org/2010/05/bunny/trailer.mp4), and [Wikimedia bees](https://commons.wikimedia.org/wiki/File:Bees_on_flowers.webm) clips through MediaMTX. The client resolved both codec choices to `vtdec_hw`; all four tiles reached `playing`. Between 13:56:11 and 14:00:04 UTC, its output advanced to 3,497 frames and each tile decoded at least 3,481 frames without a source generation change. A separate local 30-frame HEVC file probe through `h265parse ! vtdec_hw ! videoconvert` completed. This tests the VideoToolbox path and status counters on one Intel Mac; GPU utilization, visible pixels, 30-minute four-tile stability, and Apple Silicon remain unmeasured.

After adding native decoder-input monitoring, an opt-in `EUFY_RTSP_HARDWARE_TEST=1 go test ./internal/gstnative -run TestRendererMacHardwareRTSP -count=1 -v` also passed on this Intel Mac against a local H.264 MediaMTX publisher. It selected `vtdec_hw` and advanced both decoded and compositor output counters beyond 10 frames in 2.48 seconds. This uses a headless test sink; the decoder-rejection fallback and visible window still need a physical fault trial.

The frame counter is upstream of the macOS display sink, so these trials prove pipeline progress rather than pixels on the screen. The synthetic soak does not verify a real Eufy camera, a clean-host installer, physical display continuity, or sleep/wake. No Apple Silicon 30-minute live run or Gatekeeper clean-host install has been recorded. Record OS and chip, GStreamer version, codecs and sizes, tile count, displayed FPS/drops, CPU and memory, source-loss/recovery times, and whether unaffected tiles continue rendering before marking either profile supported.

During local race testing with a headless `fakesink`, one of 30 startup attempts decoded 121 and 151 source frames while the compositor remained `PLAYING` with zero output for ten seconds. The renderer now requires black-frame compositor output within five seconds before opening camera sources. It cycles its GStreamer pipeline once if startup stalls and returns a setup error if the second attempt also produces no output. Deterministic tests cover recovery and the two-failure exit; 30 subsequent headless stress runs passed. An Intel window soak after this change ran for 30 minutes across 180 ten-second samples: output advanced 33→27,033, decoded frames 15→27,017, both sampled rates stayed at or above 14.9 fps, maximum sampled frame age was 0.485 seconds, peak RSS was 69,924 KiB, maximum sampled CPU was 6.8%, and no errors occurred. This soak preceded the Darwin startup-thread fix below.

The first rebuilt window trial produced no status or RTSP connection before its 25-second timeout. A traced reproduction stalled in one of ten fresh-home starts before renderer entry. The process sample and macOS log showed AppKit repeatedly raising `NSInternalInconsistencyException: nextEventMatchingMask should only be called from the Main Thread!`. The Darwin client now locks the Go main goroutine to the process startup OS thread during package initialization, before Cocoa starts. All 40 subsequent fresh-home window starts reached status in 4.64–5.09 seconds and decoded more than five frames in 6.69–7.13 seconds. A clean build of commit `3ec5f5f` passed a bounded window recovery trial with two 45-second RTSP publisher outages: compositor output advanced 33→708 and 963→1,638 while the publisher was absent, and decoding resumed on source generations 4 and 7 about 17 seconds after each restart. The [startup recovery evidence](evidence/macos-intel-2026-09-25-startup-recovery.json) records the source revision, binary digest, and measurement limits.

A clean build of revision `7ee4d95` (SHA-256 `25d63f62da7da39c853a00efcbad70643132dd02e521637350c8c90d488ffd1c`) then passed a 30-minute Intel window soak. All 180 ten-second samples kept the tile playing on source generation 1: output advanced 33→27,033, decoded frames 16→27,016, minimum sampled output and decode rates were both 14.9 fps, and maximum sampled output-frame age was 0.111 seconds. Peak RSS was 66,400 KiB (64.8 MiB), maximum sampled CPU was 8.2%, the harness reported no failures, and the client exited with status 0 after SIGTERM. The local bridge was intentionally absent because the tile used a direct RTSP URL, so the log contains expected event-WebSocket retries. The Homebrew GStreamer plugin scanner also logged GI typelib and duplicate GTK-class warnings at startup; frame progress was unaffected. The [final-source soak evidence](evidence/macos-intel-2026-09-25-final-source-soak.json) records the binary digest and exact trial scope. The headless compositor stall's underlying GStreamer scheduling cause and a clean-host Mac service restart from such a stall remain unverified. Keep both in the physical qualification fault matrix.

A bounded Intel mixed-codec trial used a clean build of revision `3051ddd` (SHA-256 `82c3a558979e5bc89c07a3bf0e517c631b6e3bb68c2f2e8dd922a6c4d81d3251`) with two window tiles and three local MediaMTX/FFmpeg publishers at 320×180 and 15 fps. A local bridge WebSocket stub selected H.264, then H.265, then H.264 again for a `motion: latest` tile. MediaMTX confirmed a TCP reader on its H.265 track. The H.265 generation reached `playing` and 17 decoded frames within an observed 2.03 seconds; the H.264 return reached 29 decoded frames within 3.07 seconds. The fixed H.264 peer stayed on generation 1 and advanced 15→135 decoded frames across both switches; compositor output advanced 32→153 and then 198 during the final hold. Status is written every two seconds, so these observed switch times include its sampling delay. The client exited cleanly.

An isolated native fault trial then stopped only a headless test compositor pipeline after both synthetic live tiles decoded. Its output watchdog reported a fatal freeze in 176–252 ms with a 250 ms threshold; a newly constructed renderer produced 6–7 output frames and six decoded frames on each tile within 386–411 ms. Ten repeated trials and three focused race runs passed. This exercises native fault detection, cleanup, and recreation; it does not exercise launchd restarting a Mac window after a compositor failure. The [mixed-codec and fault evidence](evidence/macos-intel-2026-09-25-codec-compositor-fault.json) records both binary digests, counters, source method, and limits.

| Mac / OS | Streams / codec / size | Window FPS / drops / CPU | Source loss and sleep/wake | Gatekeeper install |
| --- | --- | --- | --- | --- |
| Intel / macOS 15.8, unverified | Local H.264 one-tile soak at 640×360, 15 fps; bounded H.264→H.265→H.264 motion switch with fixed H.264 peer at 320×180, 15 fps | Final-source 30-minute soak: output and decode ≥14.9 fps, peak client CPU 8.2%, RSS 64.8 MiB; displayed FPS/drops unmeasured | Two 45-second outages: output +675 frames in each; decode recovered about 17 seconds after each restart. Motion switch decoded both codecs with peer generation unchanged. Sleep/wake unmeasured | Unmeasured |
| Apple Silicon / unverified | | | | |
