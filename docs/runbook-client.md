# Client runbook: eufy-wall

## Release install

The release workflow publishes Linux binaries for amd64, arm64, armv7, and armv6. These cover the requested Debian x86-64 and Raspberry Pi 1, 3, 4, and 5 architecture families; this packaging matrix is not a hardware performance qualification. The release archive includes the binary, installer, systemd unit, and YAML example. No Go build or repo checkout is required on the display host. The installer uses apt for GStreamer and DRM tools when absent and checks the `intervideosrc` and `intervideosink` elements used by per-tile compositor pipelines. For offline install, preinstall those packages from an OS mirror or cache and pass `--no-apt`.

Get a tag from [GitHub Releases](https://github.com/sapireli/eufy-p2p-rtsp-bridge/releases). On the display host, replace `vX.Y.Z` and choose the matching architecture. `dpkg --print-architecture` reports `armhf` for both 32-bit Pi variants: use `armv6` on Pi 1/Zero and `armv7` on Pi 2/3/4 running 32-bit OS.

```sh
(
set -e
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
DIGEST=$(sha256sum "$FILE" | cut -d ' ' -f 1)
tar -xzf "$FILE" eufy-wall-client/deploy/install-client.sh eufy-wall-client/deploy/install-common.sh
bash eufy-wall-client/deploy/install-client.sh --artifact "$FILE" --checksums SHA256SUMS --trusted-sha256 "$DIGEST" --verify-only
sudo bash eufy-wall-client/deploy/install-client.sh --artifact "$FILE" --checksums SHA256SUMS --trusted-sha256 "$DIGEST"
)
```

The fail-fast command block verifies provenance before extracting scripts. The root installer pins that verified archive digest and rejects a later change without needing the user's GitHub credentials. `SHA256SUMS` is an extra integrity check and cannot authenticate an archive if someone replaces both files.
On a fresh host, install `ca-certificates`, `curl`, `tar`, and `coreutils`, plus a recent GitHub CLI using its [official Linux instructions](https://github.com/cli/cli/blob/trunk/docs/install_linux.md). Confirm `gh attestation verify --help` works. GitHub CLI's API lookup may require `gh auth login` or `GH_TOKEN` as the verifying user; see its [authentication guide](https://cli.github.com/manual/gh_auth_login). On a Pi where GitHub CLI is unavailable, use the offline trusted digest flow below.

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

The installer uses `/opt/eufy-wall/releases/VERSION-ARCH`, `/opt/eufy-wall/current`, and `/usr/local/bin/eufy-wall`. It preserves `/etc/eufy-wall.yaml` on upgrades. A fresh install writes `/etc/eufy-wall.example.yaml` for reference and leaves active `/etc/eufy-wall.yaml` absent, so a failed first apply cannot restore an unconfigured example. It stays stopped and disabled until setup succeeds. A repeat install of the same checksum leaves a running service alone. An upgrade preserves a stopped service's state; if it was running, the installer switches the symlink, restarts, checks service and compositor frame progress with `eufy-wall health`, and restores the old binary and exact prior unit file on failure. Later run `sudo bash eufy-wall-client/deploy/install-client.sh --rollback` to choose the previous binary. A pre-release `/usr/local/bin/eufy-wall` is copied into a `legacy-*` release first.

### Clean-host install acceptance

Use a Debian amd64 systemd VM or host and two verified client archives (A then B). Record the OS image, artifact digests, active release, unit hash, service state, and command exit codes. This installer trial does not qualify a decoder or screen: the render and recovery trial below needs physical display hardware.

1. Snapshot a clean VM. Install A using the verified command above. Confirm `/opt/eufy-wall/current/VERSION` is A, `/etc/eufy-wall.example.yaml` exists, active `/etc/eufy-wall.yaml` is absent, and `systemctl is-active --quiet eufy-wall` and `systemctl is-enabled --quiet eufy-wall` both return nonzero. Repeat A and confirm the unit hash and service state stay unchanged.
2. Validate a hand-written YAML with `eufy-wall config validate wall.yaml`. The `config apply` and `setup` health gates require a reachable bridge and fresh frames; exercise each with real LAN feeds and record whether rollback restores the prior YAML on a deliberately bad candidate. A VM without a bridge or display cannot pass the visual gate.
3. Save `sha256sum /etc/systemd/system/eufy-wall.service`. On a host with a healthy running wall, install B and verify the active release, stable service, and fresh frames on screen. Run B's installer with `--rollback`; confirm A, the saved unit hash, and fresh frames. Repeat from a deliberately stopped service and confirm it stays stopped.
4. Restore the VM snapshot for an offline trial. Preinstall GStreamer and DRM packages, transfer a separately verified archive and digest, disconnect external network, and run `--trusted-sha256 DIGEST --no-apt --verify-only` before installing. A wrong digest must fail before the release pointer or unit changes.

No result from this clean-host sequence has been recorded yet. The existing synthetic installer tests cover integrity and rollback helpers, not a live Debian systemd install.

## First setup and manual YAML

The binary owns local config commands. `sudo eufy-wall setup` applies a healthy configuration and enables the service. To provide a hand-authored YAML file, use the same validator and safe apply path:

```sh
eufy-wall config example > wall.yaml
# Replace the example bridge URL and camera serials; choose this host's layout and output.
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
gst-inspect-1.0 --exists intervideosrc
gst-inspect-1.0 --exists intervideosink
sudo journalctl -u eufy-wall -n 100 --no-pager
```

For `sink: planes`, each active tile needs a usable DRM plane for the chosen output. Counting plane IDs alone is insufficient; run a render probe. The compositor now has per-tile source recovery in the native renderer, but its isolation and frame progress still need measurements on each Pi and Debian display profile. For an H.265 passthrough source, the display host must have an H.265 decoder. If it does not, configure server-side hardware transcode where supported or use a different display host. Do not assume every Pi model decodes H.265.

Two physical outputs should run separate wall instances with separate config files and output selectors; each output has its own DRM CRTC and plane routing. The service unit starts the default `/etc/eufy-wall.yaml` only. Additional instances need separate units.

For Intel and Apple Silicon, follow the separate [macOS client runbook](runbook-client-macos.md) for the user-level launchd installer, Homebrew GStreamer, and window sink. The Linux installer and KMS commands on this page do not apply there.

## Repeatable hardware qualification

The release installs `/opt/eufy-wall/current/deploy/qualify-client.py` for Raspberry Pi 1, 3, 4, 5 and Debian x86-64 trials. It requires Python 3.7 or later, GStreamer with [fpsdisplaysink](https://gstreamer.freedesktop.org/documentation/debugutilsbad/fpsdisplaysink.html) (`gstreamer1.0-plugins-bad`), `gst-launch-1.0`, and `modetest` for plane inventory. It changes no configuration and never restarts a service or network connection. It reads model, OS, kernel, connected DRM outputs and modes, numeric plane IDs, and GStreamer element availability. Its own single-stream RTSP probe records advertised codec and frame size when available, cumulative rendered and dropped frames, FPS, and the probe process's CPU and resident memory. The default `fakesink` measures decode progress without taking the display; `kmssink` displays that one stream and needs the wall service stopped. Neither probe proves that a multi-tile layout works.

Use an unauthenticated LAN RTSP URL from your bridge. The prompt keeps it out of shell history; GStreamer receives it in a process argument that local users may be able to see while the probe runs. The script rejects URLs with embedded credentials and never saves the URL or raw GStreamer log. It prints a Markdown row and writes an allowlisted JSON report; review the report before sharing it.

```sh
printf 'Bridge RTSP URL: '
IFS= read -r QUALIFY_RTSP_URL
export QUALIFY_RTSP_URL
python3 /opt/eufy-wall/current/deploy/qualify-client.py \
  --duration 60 --output "$HOME/eufy-wall-probe.json"
unset QUALIFY_RTSP_URL
```

For the physical display probe, check that `eufy-wall` is active and that the operator has reserved a 60-second screen interruption. This example always starts the service again, even when the probe fails. The `wall` service account has the installed video and render group access.

```sh
printf 'Bridge RTSP URL: '
IFS= read -r QUALIFY_RTSP_URL
export QUALIFY_RTSP_URL
(
  set -e
  sudo systemctl is-active --quiet eufy-wall
  trap 'sudo systemctl start eufy-wall' EXIT
  sudo systemctl stop eufy-wall
  sudo --preserve-env=QUALIFY_RTSP_URL -u wall \
    python3 /opt/eufy-wall/current/deploy/qualify-client.py \
      --sink kmssink --duration 60 --output /tmp/eufy-wall-kms-probe.json
)
unset QUALIFY_RTSP_URL
```

For a 30-minute soak, use `--duration 1800` while watching the actual wall separately. A successful single-stream probe is still marked **unverified**. Record the configured tile count, codecs, sizes, connector, sink, and visual frame continuity in the trial notes. The JSON's CPU and memory numbers cover only the probe's `gst-launch-1.0` process, not the full wall, bridge, or kernel decoder.

For recovery, add `QUALIFY_BRIDGE_URL=http://SERVER_IP:3000` and `--recovery-event bridge_restart --recovery-duration 120` to the first command. Wait for “Bridge baseline ready” before restarting `eufy-wall-bridge` on the server. The watcher times the first failed `/healthz` poll through two successful polls and records whether the wall service remained active. For `network_interruption`, disconnect the client network link manually for at most 10 seconds, reconnect it, and use that event name. The script does not initiate either fault. Its recovery number is HTTP reachability, not resumed video. Watch the display and separately record the time to fresh frames and whether unaffected tiles kept rendering.

Camera and station power cycles are manual gates: with an operator present, cycle one device at a time, note the exact start/return times, and confirm fresh frames and motion holds after recovery. The harness does not switch device power. Never mark a profile qualified from the generated row alone; attach the report and visual/recovery notes to a measured row below.

## Recovery

If the screen is black, inspect `eufy-wall doctor`, `journalctl`, output ownership (`video` and `render` groups), and whether another compositor holds DRM. A missing GStreamer decoder, watchdog, `intervideosrc`, or `intervideosink` needs its corresponding OS package; on Debian these intervideo elements come from `gstreamer1.0-plugins-bad`. A camera that stops delivering frames should be detected by the runtime progress watchdog; confirm the recovery in the journal. If an upgrade fails, use the installer `--rollback` and inspect the current binary symlink with `readlink /opt/eufy-wall/current`.

## Measured profile table

No Pi stream count, frame rate, CPU, dropped-frame, or recovery measurements are recorded yet. Release archive availability does not imply hardware qualification. Record the actual Pi/CPU, OS and kernel, display mode, codecs and sizes, sink and planes, 30-minute live result, dropped frames, and recovery under camera/station/network restarts before marking a profile supported.

| Device / OS / kernel | Output | Streams / codec / size | Sink / planes | FPS / drops / CPU | Soak and recovery |
| --- | --- | --- | --- | --- | --- |
| Raspberry Pi 1 / unverified | | | | | |
| Raspberry Pi 3 / unverified | | | | | |
| Raspberry Pi 4 / unverified | | | | | |
| Raspberry Pi 5 / unverified | | | | | |
| Debian x86-64 / unverified | | | | | |
