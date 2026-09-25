#!/usr/bin/env bash
# Runs only on an isolated Ubuntu CI runner with real systemd. The binary is a
# service stub so this checks install transactions, not video or hardware.
set -euo pipefail
[[ ${CI:-} == true && $(uname -s) == Linux ]] || { echo 'requires an isolated Linux CI runner' >&2; exit 1; }
[[ $(uname -m) == x86_64 ]] || { echo 'requires an amd64 CI runner' >&2; exit 1; }
. /etc/os-release
[[ ${ID:-} == ubuntu ]] || { echo 'requires an Ubuntu CI runner' >&2; exit 1; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
scratch=$(mktemp -d)
chmod 755 "$scratch"
cleanup() {
  sudo systemctl disable --now eufy-wall.service eufy-wall@left.service >/dev/null 2>&1 || true
  sudo rm -f /etc/systemd/system/eufy-wall.service /etc/systemd/system/eufy-wall@.service \
    /usr/local/bin/eufy-wall /etc/eufy-wall.example.yaml
  sudo rm -rf /opt/eufy-wall
  sudo systemctl daemon-reload || true
  rm -rf "$scratch"
}
for path in /opt/eufy-wall /etc/systemd/system/eufy-wall.service \
  /etc/systemd/system/eufy-wall@.service /usr/local/bin/eufy-wall \
  /etc/eufy-wall.yaml /etc/eufy-wall.example.yaml; do
  [[ ! -e $path && ! -L $path ]] || { echo "CI runner is not clean: $path" >&2; exit 1; }
done
trap cleanup EXIT
touch "$scratch/events"
chmod 666 "$scratch/events"

make_archive() {
  local version=$1 root="$scratch/eufy-wall-client" name
  rm -rf "$root"
  mkdir -p "$root/deploy"
  printf '%s\n' "$version" > "$root/VERSION"
  printf 'amd64\n' > "$root/ARCH"
  printf 'schema_version: 2\n' > "$root/config.example.yaml"
  cp "$repo/deploy/eufy-wall.service" "$repo/deploy/eufy-wall@.service" "$root/deploy/"
  printf '# %s\n' "$version" >> "$root/deploy/eufy-wall@.service"
  printf '#!/bin/sh\nVERSION=%q\nLOG=%q\n' "$version" "$scratch/events" > "$root/eufy-wall"
  cat >> "$root/eufy-wall" <<'EOF'
printf '%s\n' "$VERSION $*" >> "$LOG"
case ${1:-} in
  config) [ "${2:-}" = recover ] ;;
  health) [ "$VERSION" != v1.2.5 ] || [ "${2:-}" != --instance ] ;;
  *) while :; do sleep 30; done ;;
esac
EOF
  chmod 755 "$root/eufy-wall"
  name="eufy-wall-$version-linux-amd64.tar.gz"
  tar -czf "$scratch/$name" -C "$scratch" eufy-wall-client
  (cd "$scratch" && sha256sum "$name") >> "$scratch/SHA256SUMS"
  printf '%s' "$scratch/$name"
}
install_archive() {
  local file=$1 hash
  hash=$(sha256sum "$file")
  sudo bash "$repo/deploy/install-client.sh" --artifact "$file" --checksums "$scratch/SHA256SUMS" \
    --trusted-sha256 "${hash%% *}" --no-apt
}
first=$(make_archive v1.2.3)
second=$(make_archive v1.2.4)
failing=$(make_archive v1.2.5)
install_archive "$first"
[[ $(cat /opt/eufy-wall/current/VERSION) == v1.2.3 ]]
[[ ! -e /etc/eufy-wall.yaml ]]
sudo systemctl enable --now eufy-wall.service eufy-wall@left.service
sudo systemctl is-active --quiet eufy-wall.service eufy-wall@left.service
first_template=$(sha256sum /etc/systemd/system/eufy-wall@.service)
install_archive "$first"
[[ $(sha256sum /etc/systemd/system/eufy-wall@.service) == "$first_template" ]]

install_archive "$second"
[[ $(cat /opt/eufy-wall/current/VERSION) == v1.2.4 ]]
sudo systemctl is-active --quiet eufy-wall.service eufy-wall@left.service
grep -q '^v1.2.4 health$' "$scratch/events"
grep -q '^v1.2.4 health --instance left$' "$scratch/events"
sudo bash "$repo/deploy/install-client.sh" --rollback
[[ $(cat /opt/eufy-wall/current/VERSION) == v1.2.3 ]]
[[ $(sha256sum /etc/systemd/system/eufy-wall@.service) == "$first_template" ]]
sudo systemctl is-active --quiet eufy-wall.service eufy-wall@left.service
grep -q '^v1.2.3 health --instance left$' "$scratch/events"

# A named instance can fail while the default unit remains healthy. The
# installer must restore both the prior binary and the exact prior template.
if install_archive "$failing"; then echo 'accepted a failing named instance' >&2; exit 1; fi
[[ $(cat /opt/eufy-wall/current/VERSION) == v1.2.3 ]]
[[ $(sha256sum /etc/systemd/system/eufy-wall@.service) == "$first_template" ]]
sudo systemctl is-active --quiet eufy-wall.service eufy-wall@left.service
grep -q '^v1.2.5 health$' "$scratch/events"
grep -q '^v1.2.5 health --instance left$' "$scratch/events"
[[ ! -e /opt/eufy-wall/.upgrade-pending ]]
echo 'Linux systemd installer transaction passed (video stubbed)'
