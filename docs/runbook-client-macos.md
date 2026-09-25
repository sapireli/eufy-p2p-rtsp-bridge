# macOS client runbook

The macOS client is a per-user windowed wall. It uses `sink: window`, Homebrew GStreamer, and a launchd agent in the logged-in user's GUI session. It does not use systemd, apt, DRM planes, or `sudo`. Intel (`amd64`) and Apple Silicon (`arm64`) archives are built, but neither display profile has a recorded live measurement yet.

## Install a verified release

Install [Homebrew GStreamer](https://formulae.brew.sh/formula/gstreamer) with `brew install gstreamer`. The current formula includes the GStreamer plugins. Confirm `gst-launch-1.0`, `gst-inspect-1.0`, `compositor`, `autovideosink`, and `watchdog` are available. Install a recent [GitHub CLI](https://cli.github.com/) and authenticate it as your login user (`gh auth login` or `GH_TOKEN`) for online attestation lookup. Run the commands below in Terminal in that user's desktop session, with no `sudo`.

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

If the Mac cannot reach GitHub, verify the archive and attestation on a connected trusted machine, then transfer the archive, `SHA256SUMS`, installer, and verified SHA-256 through a separate trusted channel. On the Mac, use the two installer commands above with `--trusted-sha256 VERIFIED_DIGEST`; no network is needed once Homebrew GStreamer is present. The installer also supports a transferred `--attestation-bundle BUNDLE --trusted-root ROOT` pair for local `gh` verification. A checksum copied alongside an unverified archive is not a trust root.

These archives have GitHub workflow attestations but currently lack a configured Apple Developer ID signing and notarization step. [Apple requires Developer ID signing for Gatekeeper distribution](https://developer.apple.com/developer-id/). Treat the Mac archives as test artifacts until that release gate and clean-host Gatekeeper test are completed; do not instruct users to bypass Gatekeeper. Once signed artifacts are published as release assets, use the same verify-first sequence with the published `SHA256SUMS` instead of generating a local manifest.

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

Download and verify the next tag, then run that archive's installer with the same `--artifact`, `--checksums`, and trusted digest options. It snapshots the previous plist and active release, switches the `current` link, restarts a loaded agent, and waits for a stable launchd PID. If activation fails, it restores the old binary and exact plist. Run `bash eufy-wall-client/deploy/install-client-macos.sh --rollback` to select the previous release later. The installer preserves an unloaded agent's state. Configuration apply has its own YAML backup and health rollback in the client binary.

Inspect `~/Library/Application Support/eufy-wall/wall.err.log`, `eufy-wall status --json`, and `launchctl print "gui/$(id -u)/com.eufy.wall"` if the window does not appear. A stable launchd PID alone does not prove fresh decoded or displayed frames. Use the client setup probe and observe the actual window after every Mac upgrade; record Intel and Apple Silicon results separately.

## Qualification evidence

No Intel or Apple Silicon 30-minute live run, sleep/wake result, source-loss recovery, codec-switch result, or Gatekeeper clean-host install has been recorded. Record OS and chip, GStreamer version, codecs and sizes, tile count, displayed FPS/drops, CPU and memory, source-loss/recovery times, and whether unaffected tiles continue rendering before marking either profile supported.

| Mac / OS | Streams / codec / size | Window FPS / drops / CPU | Source loss and sleep/wake | Gatekeeper install |
| --- | --- | --- | --- | --- |
| Intel / unverified | | | | |
| Apple Silicon / unverified | | | | |
