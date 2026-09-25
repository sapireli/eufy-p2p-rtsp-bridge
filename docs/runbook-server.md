# Server runbook: eufy-wall-bridge

## Release install targets and files

The release workflow publishes `eufy-wall-bridge-vX.Y.Z-linux-amd64.tar.gz` and `...-arm64.tar.gz`, plus `SHA256SUMS`. These cover Debian x86-64 and 64-bit ARM architecture families; a Raspberry Pi 1 cannot run this server artifact. Packaging and synthetic installer checks do not establish clean-host or live-camera support, and no server device or soak profile has been qualified yet. Each archive carries Node 24.5.0, go2rtc 1.9.14, the pinned production npm dependencies, the bridge, a systemd unit, an example config, and the installer. The host needs Debian/Ubuntu with systemd; the installer adds `ffmpeg` and CA certificates from apt when absent. The target does not run npm or build a Git dependency.

Choose a release tag from [GitHub Releases](https://github.com/sapireli/eufy-p2p-rtsp-bridge/releases). Replace `vX.Y.Z` below with that exact tag. On the server:

```sh
(
set -e
VERSION=vX.Y.Z
ARCH=$(dpkg --print-architecture) # amd64 or arm64
BASE="https://github.com/sapireli/eufy-p2p-rtsp-bridge/releases/download/$VERSION"
FILE="eufy-wall-bridge-$VERSION-linux-$ARCH.tar.gz"
curl -fL -o "$FILE" "$BASE/$FILE"
curl -fL -o SHA256SUMS "$BASE/SHA256SUMS"
grep "  $FILE\$" SHA256SUMS | sha256sum -c -
gh attestation verify "$FILE" --repo sapireli/eufy-p2p-rtsp-bridge \
  --signer-workflow sapireli/eufy-p2p-rtsp-bridge/.github/workflows/release.yml \
  --source-ref "refs/tags/$VERSION"
DIGEST=$(sha256sum "$FILE" | cut -d ' ' -f 1)
tar -xzf "$FILE" eufy-wall-server/deploy/install-server.sh eufy-wall-server/deploy/install-common.sh
bash eufy-wall-server/deploy/install-server.sh --artifact "$FILE" --checksums SHA256SUMS --trusted-sha256 "$DIGEST" --verify-only
sudo bash eufy-wall-server/deploy/install-server.sh --artifact "$FILE" --checksums SHA256SUMS --trusted-sha256 "$DIGEST"
)
```

The fail-fast command block verifies provenance before extracting scripts. The root installer then pins the exact verified archive digest, so it does not need the user's GitHub credentials. `SHA256SUMS` alone is insufficient: someone who can replace the archive can replace its checksum file. The signer workflow and tag must match this repository's release workflow and the archive version. If verification fails or the archive changes afterward, installation stops.
On a fresh host, install `ca-certificates`, `curl`, `tar`, and `coreutils`, plus a recent GitHub CLI using its [official Linux instructions](https://github.com/cli/cli/blob/trunk/docs/install_linux.md). Confirm `gh attestation verify --help` works; an older Debian package may lack the command. GitHub CLI's API lookup may require `gh auth login` or `GH_TOKEN`; authenticate as the user running the verification step, not as root. See the [GitHub CLI authentication guide](https://cli.github.com/manual/gh_auth_login).

For an offline server, use a connected trusted machine to download and verify the release as above, then run `gh attestation download "$FILE" -R sapireli/eufy-p2p-rtsp-bridge` and `gh attestation trusted-root > trusted_root.jsonl`. Transfer the archive, manifest, resulting `sha256:*.jsonl` bundle, trusted root, GitHub CLI, and verified installer scripts to the server over a trusted channel. On the server, replace the final install command with:

```sh
sudo bash eufy-wall-server/deploy/install-server.sh --artifact "$FILE" --checksums SHA256SUMS \
  --attestation-bundle sha256:ARTIFACT_DIGEST.jsonl --trusted-root trusted_root.jsonl --no-apt
```

Run the same command without `sudo` and with `--verify-only` first. The bundle and trusted root must be transferred from the trusted machine; untrusted copies could forge their own signing root. If GitHub CLI is unavailable on the target, verify the attestation on the connected machine and pass the resulting archive SHA-256 through a *separate trusted channel*. The target command is:

```sh
TRUSTED_SHA256=PASTE_VERIFIED_DIGEST_FROM_TRUSTED_MACHINE
sudo bash eufy-wall-server/deploy/install-server.sh --artifact "$FILE" --checksums SHA256SUMS \
  --trusted-sha256 "$TRUSTED_SHA256" --no-apt
```

Do not derive `TRUSTED_SHA256` from the transferred `SHA256SUMS` file. The target checks the archive against both values. `--no-apt` requires all OS packages to be installed from a local mirror or cache before switching releases. GitHub documents [offline attestation verification](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline).

The installer stores releases under `/opt/eufy-wall-bridge/releases/`, points `/opt/eufy-wall-bridge/current` at the active release, and leaves `/etc/eufy-wall-bridge.yaml`, `/etc/eufy-wall-bridge.env`, and `/var/lib/eufy-wall-bridge/` intact on upgrade. A fresh install writes `/etc/eufy-wall-bridge.example.yaml` for reference and leaves active `/etc/eufy-wall-bridge.yaml` absent, so a failed first apply has no fictitious working config to restore. It stays stopped and disabled until setup succeeds. Reinstalling the same release and checksum leaves a running service alone. An upgrade preserves a stopped service's state; if it was running, the installer restarts it and checks both a stable process and the HTTP health response. It restores the prior binary and exact prior unit file if either check fails. Run `sudo bash eufy-wall-server/deploy/install-server.sh --rollback` to select the previous release later. For an existing repo-based service, its old unit is retained at `/opt/eufy-wall-bridge/legacy.service` for rollback.

### Clean-host install acceptance

This sequence still needs a Debian amd64 systemd VM or host, two verified release archives (A then B), and a live Eufy account/LAN for the setup and health gate. Record OS image, architecture, both artifact digests, command exit codes, unit hash, service state, and `/healthz` result with each trial. The current synthetic installer tests establish checksum, archive safety, and rollback helper behavior; they do not establish a working Debian service.

1. Snapshot a clean VM. Install A with the verified command above. Confirm `/opt/eufy-wall-bridge/current/VERSION` is A, `/etc/eufy-wall-bridge.example.yaml` and `.env` exist, active `/etc/eufy-wall-bridge.yaml` is absent, the env mode is `0600`, and `systemctl is-active --quiet eufy-wall-bridge` and `systemctl is-enabled --quiet eufy-wall-bridge` both return nonzero. Repeat the same install and confirm the unit hash and service state do not change.
2. Complete `sudo eufy-bridge setup` with a test account and camera, or apply a validated hand-written YAML and credentials, then enable/start the service. Confirm `eufy-bridge status --json` reports `live.ok: true` and a camera is visible. This is the live-device gate; a VM without that account and LAN cannot pass it.
3. Save `sha256sum /etc/systemd/system/eufy-wall-bridge.service` and the active release path. Install verified B while the service runs, confirm B is active and healthy, then run B's installer with `--rollback`. Confirm A is active and healthy and the unit hash matches the saved hash. Repeat from a stopped service and confirm the upgrade leaves it stopped.
4. Restore the VM snapshot for an offline trial. Preinstall the OS packages, transfer a separately verified archive and digest, disconnect external network, and run `--trusted-sha256 DIGEST --no-apt --verify-only` before the install. Confirm the same first-install state and config permissions. A wrong trusted digest must fail without changing the active release or unit.

No result from this clean-host sequence has been recorded yet. Keep VM installer acceptance separate from camera soak and display qualification.

## First setup

Run `sudo eufy-bridge setup` for the interactive path; it enables the service after login and health checks succeed. For manual setup, first set `EUFY_EMAIL`, `EUFY_PASSWORD`, and `EUFY_COUNTRY` in `/etc/eufy-wall-bridge.env`, then apply your own YAML. The environment file is root-owned and mode `0600`; its installed example contains only commented credentials. Use a dedicated Eufy account; sharing the phone app account can evict the bridge session. The installer keeps `/etc/eufy-wall-bridge.example.yaml` separate and does not replace an existing active config. `config example` and `config explain` need no access to credentials; run validation and apply with `sudo` so they can read the installed environment file. The CLI accepts custom YAML files and standard input:

```sh
eufy-bridge config example > bridge.yaml
# Replace the example LAN, camera serials, and any credentials with your own values.
sudo eufy-bridge config validate bridge.yaml
sudo eufy-bridge config apply bridge.yaml
# Or from a workstation: cat bridge.yaml | ssh server 'sudo eufy-bridge config apply -'
sudo systemctl enable eufy-wall-bridge
sudo eufy-bridge doctor
sudo eufy-bridge status
```

See [server config reference](config-server.md) for every field and defaults. For a fresh manual install, set `lan.cidr` in the YAML, then run `sudo systemctl enable --now eufy-wall-bridge` after successful apply. Keep the HTTP and RTSP ports on a trusted LAN; they do not authenticate remote clients.

When auth requests 2FA or captcha, use the local CLI wizard. For a manual recovery, check `curl -fsS http://127.0.0.1:3000/auth/status`. Challenge answers belong in POST bodies; do not put them in URL query strings or shell history. A captured session is stored under `/var/lib/eufy-wall-bridge/`.

## Check and recovery

```sh
sudo systemctl status eufy-wall-bridge
sudo journalctl -u eufy-wall-bridge -n 100 --no-pager
curl -fsS http://127.0.0.1:3000/healthz
curl -fsS http://127.0.0.1:3000/api/cameras
ffplay rtsp://SERVER_IP:8554/STREAM_KEY
```

The `healthz` endpoint answers before authentication; inspect `auth.state`, `stalled`, and `go2rtc` in its body. `api/cameras` requires successful authentication. The `STREAM_KEY` comes from camera inventory, not necessarily the serial. The bridge restarts stalled camera feeds and systemd restarts a failed process. A camera omitted because its SDK description failed at boot is retried every 30 seconds; when it recovers, the bridge adds its RTSP stream and refreshes connected clients' inventory without restarting healthy feeds. If an upgrade is unstable, run the installer with `--rollback`, then inspect its journal. A config apply failure should restore the previous YAML; `eufy-bridge status` reports the apply result.

`lan.force: true` rejects nonprivate P2P peers and blocks peers outside `lan.cidr`. If a station stays on a WAN route, add its LAN IP under `lan.station_addresses`. The HTTP port (default `3000`) and RTSP port (`8554`) trust the LAN; never port-forward them. The go2rtc API binds to loopback and its WebRTC listener is disabled by the bridge.

For a battery camera, `mode: always` requires an explicit `power_override: always-on` claim. Set it only when that device's power supply can sustain streaming. A fixed `on_motion` tile sleeps between events; an `on_demand` tile takes a bounded hold while visible. Camera codec comes from the live feed or the bridge's configured declaration, and the client needs a matching decoder unless go2rtc transcodes.

## Verified devices and soak evidence

No device or soak result has been recorded in this repository yet. Do not treat a model, stream count, codec, or recovery time as qualified from this runbook alone. Record model, serial prefix, power mode, codec, resolution, dual view, LAN peer, 30-minute live run, camera/station/network restart, and recovery time before adding a supported-device row.

| Model / serial prefix | Power | Codec / size | Dual view | LAN peer | 30-minute run | Recovery result |
| --- | --- | --- | --- | --- | --- | --- |
| Awaiting measured result | | | | | | |
