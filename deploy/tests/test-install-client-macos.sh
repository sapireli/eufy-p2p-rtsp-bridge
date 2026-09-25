#!/bin/bash
# Installer transaction tests on macOS with an isolated HOME and mocked launchctl.
set -euo pipefail
[[ $(uname -s) == Darwin ]] || { echo 'macOS test skipped'; exit 0; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
case $(uname -m) in x86_64) arch=amd64 ;; arm64) arch=arm64 ;; *) echo 'unsupported test host'; exit 1 ;; esac
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
export HOME="$scratch/home"
mkdir -p "$HOME" "$scratch/bin" "$scratch/eufy-wall-client/deploy"
export PATH="$scratch/bin:$PATH"
export LAUNCHCTL_TEST_DIR="$scratch/launchd"
mkdir -p "$LAUNCHCTL_TEST_DIR"
cat > "$scratch/bin/launchctl" <<'EOF'
#!/bin/bash
printf '%s\n' "$*" >> "$LAUNCHCTL_TEST_DIR/calls"
case $1 in
  print)
    [[ -f $LAUNCHCTL_TEST_DIR/loaded ]] || exit 3
    printf 'state = running\n    pid = 12345\n' ;;
  disable) touch "$LAUNCHCTL_TEST_DIR/disabled" ;;
  enable) rm -f "$LAUNCHCTL_TEST_DIR/disabled" ;;
  bootout) rm -f "$LAUNCHCTL_TEST_DIR/loaded" ;;
  bootstrap)
    if [[ -f $LAUNCHCTL_TEST_DIR/fail-next ]]; then rm -f "$LAUNCHCTL_TEST_DIR/fail-next"; exit 1; fi
    touch "$LAUNCHCTL_TEST_DIR/loaded" ;;
  *) exit 2 ;;
esac
EOF
chmod +x "$scratch/bin/launchctl"
cat > "$scratch/bin/gh" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod +x "$scratch/bin/gh"
digest() { shasum -a 256 "$1" | awk '{print $1}'; }
reject() { if "$@" >/dev/null 2>&1; then echo "accepted an invalid installer operation: $*" >&2; exit 1; fi; }
make_archive() {
  local version=$1
  printf '%s\n' "$version" > "$scratch/eufy-wall-client/VERSION"
  printf '%s\n' "$arch" > "$scratch/eufy-wall-client/ARCH"
  printf 'darwin\n' > "$scratch/eufy-wall-client/OS"
  printf 'schema_version: 2\n' > "$scratch/eufy-wall-client/config.example.yaml"
  printf '#!/bin/sh\necho %s\n' "$version" > "$scratch/eufy-wall-client/eufy-wall"
  chmod 755 "$scratch/eufy-wall-client/eufy-wall"
  cp "$repo/deploy/install-client-macos.sh" "$scratch/eufy-wall-client/deploy/"
  local archive="$scratch/eufy-wall-$version-darwin-$arch.tar.gz"
  tar -czf "$archive" -C "$scratch" eufy-wall-client
  (cd "$scratch" && shasum -a 256 "${archive##*/}") >> "$scratch/SHA256SUMS"
  printf '%s' "$archive"
}
install_release() {
  local archive=$1
  bash "$repo/deploy/install-client-macos.sh" --artifact "$archive" --checksums "$scratch/SHA256SUMS" --trusted-sha256 "$(digest "$archive")"
}

archive_a=$(make_archive v1.2.3)
archive_b=$(make_archive v1.2.4)
base="$HOME/Library/Application Support/eufy-wall"
plist="$HOME/Library/LaunchAgents/com.eufy.wall.plist"
reject bash "$repo/deploy/install-client-macos.sh" --artifact "$archive_a" --checksums "$scratch/SHA256SUMS" --verify-only
cat > "$scratch/bin/gh" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$scratch/bin/gh"
bash "$repo/deploy/install-client-macos.sh" --artifact "$archive_a" --checksums "$scratch/SHA256SUMS" --verify-only

# Even a matching checksum and pinned digest cannot authorize archive links.
for kind in symlink hardlink; do
  if [[ $kind == symlink ]]; then
    ln -s "$scratch/outside" "$scratch/eufy-wall-client/escape"
  else
    ln "$scratch/eufy-wall-client/VERSION" "$scratch/eufy-wall-client/escape"
  fi
  bad="$scratch/eufy-wall-v1.2.6-darwin-$arch.tar.gz"
  tar -czf "$bad" -C "$scratch" eufy-wall-client
  (cd "$scratch" && shasum -a 256 "${bad##*/}") > "$scratch/BADSUMS"
  if bash "$repo/deploy/install-client-macos.sh" --artifact "$bad" --checksums "$scratch/BADSUMS" --trusted-sha256 "$(digest "$bad")" --verify-only > "$scratch/link-error" 2>&1; then
    echo "accepted $kind archive member" >&2; exit 1
  fi
  grep -q 'archive contains links' "$scratch/link-error"
  rm "$scratch/eufy-wall-client/escape"
done
install_release "$archive_a"
[[ $(cat "$base/current/VERSION") == v1.2.3 && -f $LAUNCHCTL_TEST_DIR/disabled && ! -f $LAUNCHCTL_TEST_DIR/loaded ]]
[[ ! -f $base/config.yaml && -f $base/config.example.yaml ]]
[[ $(readlink "$HOME/.local/bin/eufy-wall") == "$base/current/eufy-wall" ]]
grep -q '/usr/local/bin' "$plist"
grep -q 'DYLD_FALLBACK_LIBRARY_PATH' "$plist"
grep -Fq "$base/current/eufy-wall" "$plist"
before=$(shasum -a 256 "$plist")
calls=$(grep -Ec '^(bootout|bootstrap|disable) ' "$LAUNCHCTL_TEST_DIR/calls")
install_release "$archive_a"
[[ $(grep -Ec '^(bootout|bootstrap|disable) ' "$LAUNCHCTL_TEST_DIR/calls") -eq $calls && $(shasum -a 256 "$plist") == "$before" ]]

# A local plist edit must be snapshotted exactly across upgrade and rollback.
printf '\n<!-- local launchd comment -->\n' >> "$plist"
plutil -lint -s "$plist"
before=$(shasum -a 256 "$plist")

# Simulate a healthy service after eufy-wall setup.
launchctl enable "gui/$(id -u)/com.eufy.wall"
launchctl bootstrap "gui/$(id -u)" "$plist"
install_release "$archive_b"
[[ $(cat "$base/current/VERSION") == v1.2.4 && $(cat "$base/previous/VERSION") == v1.2.3 ]]
[[ -f $LAUNCHCTL_TEST_DIR/loaded ]]
[[ $(shasum -a 256 "$base/previous-plist") == "$before" ]]
b_plist_hash=$(shasum -a 256 "$plist")

# Matching forged manifest cannot replace a digest learned from a trusted channel.
printf 'tampered\n' >> "$archive_b"
(cd "$scratch" && shasum -a 256 "${archive_b##*/}") > "$scratch/FORGEDSUMS"
reject bash "$repo/deploy/install-client-macos.sh" --artifact "$archive_b" --checksums "$scratch/FORGEDSUMS" --trusted-sha256 "$(awk '{print $1; exit}' "$scratch/SHA256SUMS")" --verify-only
[[ $(cat "$base/current/VERSION") == v1.2.4 ]]

# A failing launchd activation restores the exact previous release and plist.
archive_c=$(make_archive v1.2.5)
touch "$LAUNCHCTL_TEST_DIR/fail-next"
reject install_release "$archive_c"
[[ $(cat "$base/current/VERSION") == v1.2.4 && -f $LAUNCHCTL_TEST_DIR/loaded ]]
[[ $(shasum -a 256 "$plist") == "$b_plist_hash" && ! -f $base/.upgrade-pending ]]

# Manual rollback uses the prior release and restores the previous plist.
bash "$repo/deploy/install-client-macos.sh" --rollback
[[ $(cat "$base/current/VERSION") == v1.2.3 && -f $LAUNCHCTL_TEST_DIR/loaded ]]
[[ $(shasum -a 256 "$plist") == "$before" ]]
echo 'macOS installer tests passed'
