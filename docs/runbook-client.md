# Client runbook: eufy-wall

## Release install

The release workflow publishes Linux binaries for amd64, arm64, armv7, and armv6. The release archive includes the binary, installer, systemd unit, and YAML example. No Go build or repo checkout is required on the display host. The installer uses apt for GStreamer and DRM tools when absent; for offline install, preinstall those packages from an OS mirror or cache and pass `--no-apt`.

Get a tag from [GitHub Releases](https://github.com/sapireli/eufy-p2p-rtsp-bridge/releases). On the display host, replace `vX.Y.Z` and choose the matching architecture. `dpkg --print-architecture` reports `armhf` for both 32-bit Pi variants: use `armv6` on Pi 1/Zero and `armv7` on Pi 2/3/4 running 32-bit OS.

```sh
VERSION=vX.Y.Z
ARCH=arm64 # or amd64, armv7, armv6
BASE="https://github.com/sapireli/eufy-p2p-rtsp-bridge/releases/download/$VERSION"
FILE="eufy-wall-$VERSION-linux-$ARCH.tar.gz"
curl -fL -o "$FILE" "$BASE/$FILE"
curl -fL -o SHA256SUMS "$BASE/SHA256SUMS"
grep "  $FILE\$" SHA256SUMS | sha256sum -c -
gh attestation verify "$FILE" --repo sapireli/eufy-p2p-rtsp-bridge \
  --signer-workflow sapireli/eufy-p2p-rtsp-bridge/.github/workflows/release.yml \
  --source-ref "refs/tags/$VERSION"
tar -xzf "$FILE" eufy-wall-client/deploy/install-client.sh eufy-wall-client/deploy/install-common.sh
bash eufy-wall-client/deploy/install-client.sh --artifact "$FILE" --checksums SHA256SUMS --verify-only
sudo bash eufy-wall-client/deploy/install-client.sh --artifact "$FILE" --checksums SHA256SUMS
```

The installer repeats that provenance check before extraction. `SHA256SUMS` is an extra integrity check and cannot authenticate an archive if someone replaces both files.

For an offline display host, verify the archive on a connected trusted machine, then run `gh attestation download "$FILE" -R sapireli/eufy-p2p-rtsp-bridge` and `gh attestation trusted-root > trusted_root.jsonl`. Transfer the verified installer scripts, archive, manifest, `sha256:*.jsonl` bundle, trusted root, and GitHub CLI over a trusted channel. On the target, use:

```sh
sudo bash eufy-wall-client/deploy/install-client.sh --artifact "$FILE" --checksums SHA256SUMS \
  --attestation-bundle sha256:ARTIFACT_DIGEST.jsonl --trusted-root trusted_root.jsonl --no-apt
```

Run the same command without `sudo` and with `--verify-only` first. If GitHub CLI is unavailable on the Pi, verify the attestation on the connected machine and pass its archive SHA-256 through a *separate trusted channel*:

```sh
TRUSTED_SHA256=PASTE_VERIFIED_DIGEST_FROM_TRUSTED_MACHINE
sudo bash eufy-wall-client/deploy/install-client.sh --artifact "$FILE" --checksums SHA256SUMS \
  --trusted-sha256 "$TRUSTED_SHA256" --no-apt
```

Do not read `TRUSTED_SHA256` from the copied manifest. The offline host checks the archive against this pinned value. For either offline path, preinstall GStreamer and DRM packages from an OS mirror or cache; `--no-apt` fails before switching releases if they are missing. See GitHub's [offline attestation guide](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline).

The installer uses `/opt/eufy-wall/releases/VERSION-ARCH`, `/opt/eufy-wall/current`, and `/usr/local/bin/eufy-wall`. It preserves `/etc/eufy-wall.yaml` on upgrades. A repeat install of the same checksum leaves a running service alone. An upgrade of a running service switches the symlink, restarts, checks it stays active, and restores the old binary on failure. Later run `sudo bash eufy-wall-client/deploy/install-client.sh --rollback` to choose the previous binary. A pre-release `/usr/local/bin/eufy-wall` is copied into a `legacy-*` release first.

## First setup and manual YAML

The binary owns local config commands. The guided `sudo eufy-wall setup` wizard is planned and is not available yet. To provide a hand-authored YAML file, use the validator and safe apply path:

```sh
eufy-wall config example > wall.yaml
eufy-wall config validate wall.yaml
eufy-wall layout preview wall.yaml
sudo eufy-wall config apply wall.yaml
# Or: cat wall.yaml | ssh pi 'sudo eufy-wall config apply -'
eufy-wall doctor
sudo systemctl status eufy-wall
```

The default config path is `/etc/eufy-wall.yaml`. Legacy rendering flags remain available: `eufy-wall -config /etc/eufy-wall.yaml -dry-run` prints the planned GStreamer command, and `-print-layout` prints placement. `config recover` is run as root in the unit's `ExecStartPre`; if an apply was interrupted, it restores the previous YAML before starting the wall. See [client config reference](config-client.md) and [layout reference](layouts.md) for keys, presets, custom rectangles, and examples.

For a manual first config, set `rtsp_base` to `rtsp://SERVER_IP:8554`, select cameras by serial under `tiles`, and choose a layout. The client may need a `bridge_url` for motion and power inventory; set it explicitly. Use `eufy-wall doctor` to inspect the actual output, GStreamer elements, and decoder before starting the service. A screen can be described on a logical 32×32 canvas, but this says nothing about the host's decoder or DRM plane capacity.

## Display and decoder diagnostics

On Raspberry Pi OS Lite, KMS usually needs `dtoverlay=vc4-kms-v3d` in `/boot/firmware/config.txt`. Check the current firmware, kernel, and connected output before changing that file. The installer does not modify boot configuration; reboot after a manual KMS change. Ethernet is preferred for a wall with multiple live feeds.

```sh
eufy-wall doctor --json
ls /sys/class/drm/
modetest -M vc4 -p
gst-inspect-1.0 v4l2h264dec
gst-inspect-1.0 v4l2h265dec
sudo journalctl -u eufy-wall -n 100 --no-pager
```

For `sink: planes`, each active tile needs a usable DRM plane for the chosen output. Counting plane IDs alone is insufficient; run a render probe. `sink: compositor` avoids explicit plane IDs but may restart the full wall when one source changes, so use a planes profile for motion-heavy layouts until compositor isolation is verified. For an H.265 passthrough source, the display host must have an H.265 decoder. If it does not, configure server-side hardware transcode where supported or use a different display host. Do not assume every Pi model decodes H.265.

Two physical outputs should run separate wall instances with separate config files and output selectors; each output has its own DRM CRTC and plane routing. The service unit starts the default `/etc/eufy-wall.yaml` only. Additional instances need separate units.

On macOS, the Go binary can preview layouts with GStreamer installed through Homebrew and `sink: window`, `decoder: software`, and an explicit screen size. The Linux release installer and KMS sinks do not apply there.

## Recovery

If the screen is black, inspect `eufy-wall doctor`, `journalctl`, output ownership (`video` and `render` groups), and whether another compositor holds DRM. A missing GStreamer decoder or watchdog plugin needs its corresponding OS package. A camera that stops delivering frames should be detected by the runtime progress watchdog; confirm the recovery in the journal. If an upgrade fails, use the installer `--rollback` and inspect the current binary symlink with `readlink /opt/eufy-wall/current`.

## Measured profile table

No Pi stream count, frame rate, CPU, dropped-frame, or recovery measurements are recorded yet. Release archive availability does not imply hardware qualification. Record the actual Pi/CPU, OS and kernel, display mode, codecs and sizes, sink and planes, 30-minute live result, dropped frames, and recovery under camera/station/network restarts before marking a profile supported.

| Device / OS / kernel | Output | Streams / codec / size | Sink / planes | FPS / drops / CPU | Soak and recovery |
| --- | --- | --- | --- | --- | --- |
| Awaiting measured result | | | | | |
