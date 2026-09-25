#!/usr/bin/env bash
# Release verification tests; no root privileges or systemd required.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/bin" "$scratch/eufy-wall-server/bin" "$scratch/eufy-wall-server/server" "$scratch/eufy-wall-client"
cat > "$scratch/bin/uname" <<'EOF'
#!/bin/sh
case $1 in -s) echo Linux ;; -m) echo x86_64 ;; *) exit 2 ;; esac
EOF
chmod +x "$scratch/bin/uname"
cat > "$scratch/bin/gh" <<'EOF'
#!/bin/sh
printf '%s\n' "$@" > "$GH_TEST_LOG"
exit "${GH_TEST_EXIT:-1}"
EOF
chmod +x "$scratch/bin/gh"
export PATH="$scratch/bin:$PATH"
export GH_TEST_LOG="$scratch/gh-args"

digest_of() { local value; value=$(sha256sum "$1"); printf '%s' "${value%% *}"; }
reject() {
  local label=$1; shift
  if "$@" > /dev/null 2>&1; then echo "accepted $label" >&2; exit 1; fi
}

printf 'v1.2.3\n' > "$scratch/eufy-wall-server/VERSION"
printf 'amd64\n' > "$scratch/eufy-wall-server/ARCH"
printf '#!/bin/sh\n' > "$scratch/eufy-wall-server/bin/node"
printf '#!/bin/sh\n' > "$scratch/eufy-wall-server/bin/go2rtc"
printf '// test\n' > "$scratch/eufy-wall-server/server/server.mjs"
printf '// test\n' > "$scratch/eufy-wall-server/server/cli.mjs"
chmod +x "$scratch/eufy-wall-server/bin/node" "$scratch/eufy-wall-server/bin/go2rtc"
archive="$scratch/eufy-wall-bridge-v1.2.3-linux-amd64.tar.gz"
tar -czf "$archive" -C "$scratch" eufy-wall-server
(cd "$scratch" && sha256sum "${archive##*/}" > SHA256SUMS)
server_digest=$(digest_of "$archive")
reject 'a checksum-only artifact without provenance' bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" --verify-only
grep -q '^attestation$' "$GH_TEST_LOG"
grep -q 'sapireli/eufy-p2p-rtsp-bridge/.github/workflows/release.yml' "$GH_TEST_LOG"
grep -q '^refs/tags/v1.2.3$' "$GH_TEST_LOG"
GH_TEST_EXIT=0 bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" --verify-only
touch "$scratch/bundle.jsonl" "$scratch/trusted-root.jsonl"
GH_TEST_EXIT=0 bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" \
  --attestation-bundle "$scratch/bundle.jsonl" --trusted-root "$scratch/trusted-root.jsonl" --verify-only
grep -q '^--bundle$' "$GH_TEST_LOG"
grep -q '^--custom-trusted-root$' "$GH_TEST_LOG"
bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" --trusted-sha256 "$server_digest" --verify-only
reject 'incorrect separately trusted digest' bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" --trusted-sha256 "$(printf '0%.0s' {1..64})" --verify-only
reject 'half of offline trust pair' bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" --attestation-bundle "$scratch/bundle.jsonl" --verify-only

printf '0%.0s' {1..64} > "$scratch/BADSUM"
printf '  %s\n' "${archive##*/}" >> "$scratch/BADSUM"
reject 'a bad checksum' bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/BADSUM" --trusted-sha256 "$server_digest" --verify-only
reject 'contradictory options' bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" --trusted-sha256 "$server_digest" --verify-only --rollback
printf '// replacement\n' > "$scratch/eufy-wall-server/server/server.mjs"
tar -czf "$archive" -C "$scratch" eufy-wall-server
(cd "$scratch" && sha256sum "${archive##*/}" > SHA256SUMS)
reject 'an archive and matching replacement checksum without provenance' bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" --verify-only
printf 'arm64\n' > "$scratch/eufy-wall-server/ARCH"
tar -czf "$archive" -C "$scratch" eufy-wall-server
(cd "$scratch" && sha256sum "${archive##*/}" > SHA256SUMS)
reject 'an archive for the wrong architecture' bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" --trusted-sha256 "$(digest_of "$archive")" --verify-only

# Malicious links can escape a staging directory even when their own names are in it.
# Forge a matching manifest and explicitly pin each digest: the archive safety check
# must still reject the payload before extraction.
for link_type in symlink hardlink character; do
  python3 - "$archive" "$link_type" "$scratch/outside" <<'PY'
import io, sys, tarfile
archive, kind, outside = sys.argv[1:]
with tarfile.open(archive, 'w:gz') as out:
    for name, body in [('eufy-wall-server/VERSION', b'v1.2.3\n'), ('eufy-wall-server/ARCH', b'amd64\n')]:
        item = tarfile.TarInfo(name)
        item.size = len(body)
        out.addfile(item, io.BytesIO(body))
    item = tarfile.TarInfo('eufy-wall-server/escape')
    if kind == 'symlink':
        item.type = tarfile.SYMTYPE
        item.linkname = outside
    elif kind == 'hardlink':
        item.type = tarfile.LNKTYPE
        item.linkname = outside
    else:
        item.type = tarfile.CHRTYPE
        item.devmajor = 1
        item.devminor = 3
    out.addfile(item)
PY
  (cd "$scratch" && sha256sum "${archive##*/}" > SHA256SUMS)
  reject "a $link_type archive member" bash "$repo/deploy/install-server.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" --trusted-sha256 "$(digest_of "$archive")" --verify-only
  [[ ! -e $scratch/outside ]] || { echo "$link_type escaped staging" >&2; exit 1; }
done

printf 'v1.2.3\n' > "$scratch/eufy-wall-client/VERSION"
printf 'amd64\n' > "$scratch/eufy-wall-client/ARCH"
printf '#!/bin/sh\n' > "$scratch/eufy-wall-client/eufy-wall"
chmod +x "$scratch/eufy-wall-client/eufy-wall"
printf 'layout: 1\n' > "$scratch/eufy-wall-client/config.example.yaml"
client_archive="$scratch/eufy-wall-v1.2.3-linux-amd64.tar.gz"
tar -czf "$client_archive" -C "$scratch" eufy-wall-client
(cd "$scratch" && sha256sum "${client_archive##*/}" > CLIENTSUMS)
bash "$repo/deploy/install-client.sh" --artifact "$client_archive" --checksums "$scratch/CLIENTSUMS" --trusted-sha256 "$(digest_of "$client_archive")" --verify-only

# The release pointer operation must be repeatable and allow switching back.
# GNU mv -T is only available on the Linux installer targets.
if mv --help 2>&1 | grep -q -- --no-target-directory; then
  source "$repo/deploy/install-common.sh"
  mkdir -p "$scratch/old" "$scratch/new"
  atomic_link "$scratch/old" "$scratch/current"
  atomic_link "$scratch/new" "$scratch/current"
  [[ $(readlink "$scratch/current") == "$scratch/new" ]] || { echo 'upgrade pointer failed' >&2; exit 1; }
  printf '%s\n' "$scratch/old" > "$scratch/.upgrade-pending"
  if ! needs_activation "$(readlink "$scratch/current")" "$scratch/new" "$scratch/.upgrade-pending"; then
    echo 'interrupted switch was treated as completed' >&2; exit 1
  fi
  rm "$scratch/.upgrade-pending"
  if needs_activation "$(readlink "$scratch/current")" "$scratch/new" "$scratch/.upgrade-pending"; then
    echo 'completed switch was treated as pending' >&2; exit 1
  fi
  atomic_link "$scratch/new" "$scratch/current"
  atomic_link "$scratch/old" "$scratch/current"
  [[ $(readlink "$scratch/current") == "$scratch/old" ]] || { echo 'rollback pointer failed' >&2; exit 1; }

  cat > "$scratch/bin/systemctl" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$SYSTEMCTL_TEST_LOG"
exit 0
EOF
  chmod +x "$scratch/bin/systemctl"
  export SYSTEMCTL_TEST_LOG="$scratch/systemctl.log"
  printf 'old unit with local setting\n' > "$scratch/unit.service"
  chmod 640 "$scratch/unit.service"
  cp -p "$scratch/unit.service" "$scratch/prior-unit.service"
  printf 'new packaged unit\n' > "$scratch/new-unit.service"
  install_unit "$scratch/new-unit.service" "$scratch/unit.service"
  atomic_link "$scratch/new" "$scratch/current"
  wait_active() { return 0; } # service ordering is checked via the mocked systemctl log
  rollback_release_unit "$scratch/old" "$scratch/prior-unit.service" "$scratch/current" "$scratch/unit.service" test-service
  cmp "$scratch/unit.service" "$scratch/prior-unit.service"
  [[ $(readlink "$scratch/current") == "$scratch/old" ]] || { echo 'live rollback did not restore the binary' >&2; exit 1; }
  python3 - "$scratch/unit.service" "$scratch/prior-unit.service" <<'PY'
import os, sys
active, prior = os.stat(sys.argv[1]), os.stat(sys.argv[2])
assert active.st_mode & 0o777 == 0o640, 'prior unit mode was not restored'
assert (active.st_uid, active.st_gid) == (prior.st_uid, prior.st_gid), 'prior unit ownership was not restored'
PY
  grep -q '^daemon-reload$' "$SYSTEMCTL_TEST_LOG"
  grep -q '^restart test-service$' "$SYSTEMCTL_TEST_LOG"
  # Rolling back a deliberately stopped service must restore files without starting it.
  : > "$SYSTEMCTL_TEST_LOG"
  install_unit "$scratch/new-unit.service" "$scratch/unit.service"
  atomic_link "$scratch/new" "$scratch/current"
  rollback_release_unit "$scratch/old" "$scratch/prior-unit.service" "$scratch/current" "$scratch/unit.service" test-service 0
  cmp "$scratch/unit.service" "$scratch/prior-unit.service"
  [[ $(readlink "$scratch/current") == "$scratch/old" ]] || { echo 'stopped rollback did not restore the binary' >&2; exit 1; }
  if grep -Eq '^(restart|start) ' "$SYSTEMCTL_TEST_LOG"; then echo 'stopped rollback started the service' >&2; exit 1; fi
  write_marker "$scratch/.upgrade-active" 0
  [[ $(cat "$scratch/.upgrade-active") == 0 ]] || { echo 'stopped state marker was not saved' >&2; exit 1; }
  write_marker "$scratch/.upgrade-active" 1
  [[ $(cat "$scratch/.upgrade-active") == 1 ]] || { echo 'running state marker was not replaced' >&2; exit 1; }
  reject 'rollback without a prior unit snapshot' bash -c 'source "$1"; rollback_release_unit "$2" "$3" "$4" "$5" test-service' \
    bash "$repo/deploy/install-common.sh" "$scratch/new" "$scratch/missing-unit.service" "$scratch/current" "$scratch/unit.service"
fi

grep -q '^ExecStartPre=+/usr/local/bin/eufy-wall config recover$' "$repo/deploy/eufy-wall.service"
grep -q '^ExecStartPre=+/usr/local/bin/eufy-bridge config recover$' "$repo/deploy/eufy-wall-bridge.service"
if grep -q '^EUFY_PASSWORD=' "$repo/deploy/eufy-wall-bridge.env.example"; then echo 'sample password would be loaded as a real secret' >&2; exit 1; fi
if grep -q 'systemctl enable' "$repo/deploy/install-server.sh" "$repo/deploy/install-client.sh"; then echo 'fresh installer would enable an unconfigured service' >&2; exit 1; fi
if grep -Eq 'install .* /etc/eufy-wall(-bridge)?\.yaml' "$repo/deploy/install-server.sh" "$repo/deploy/install-client.sh"; then
  echo 'fresh installer would create active example YAML, making first-apply rollback unsafe' >&2; exit 1
fi
grep -Fq '/etc/eufy-wall-bridge.example.yaml' "$repo/deploy/install-server.sh"
grep -Fq '/etc/eufy-wall.example.yaml' "$repo/deploy/install-client.sh"
grep -Fq 'export BRIDGE_CONFIG=${BRIDGE_CONFIG:-/etc/eufy-wall-bridge.yaml}' "$repo/deploy/install-server.sh"
grep -Fq 'require_gstreamer_elements intervideosrc intervideosink' "$repo/deploy/install-client.sh"
cat > "$scratch/bin/gst-inspect-1.0" <<'EOF'
#!/bin/sh
[ "$1" = --exists ] || exit 2
[ "$2" != "${GST_MISSING_ELEMENT:-}" ]
EOF
chmod +x "$scratch/bin/gst-inspect-1.0"
source "$repo/deploy/install-common.sh"
require_gstreamer_elements intervideosrc intervideosink
for element in intervideosrc intervideosink; do
  export GST_MISSING_ELEMENT=$element
  reject "missing $element element" bash -c 'source "$1"; require_gstreamer_elements intervideosrc intervideosink' bash "$repo/deploy/install-common.sh"
done
unset GST_MISSING_ELEMENT
grep -q 'if ((upgrade_active || old_active)); then' "$repo/deploy/install-server.sh"
grep -q 'if ((upgrade_active || old_active)); then' "$repo/deploy/install-client.sh"
echo 'installer verification tests passed'
