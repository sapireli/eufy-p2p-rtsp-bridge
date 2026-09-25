#!/bin/bash
# Exercise the real packaged CLI and stopped-service installer path without
# changing the login user's launchd state.
set -euo pipefail
[[ $(uname -s) == Darwin ]] || { echo 'macOS test skipped'; exit 0; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
case $(uname -m) in x86_64) arch=amd64 ;; arm64) arch=arm64 ;; *) echo 'unsupported Mac architecture' >&2; exit 1 ;; esac
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
export HOME="$scratch/home"
mkdir -p "$HOME" "$scratch/bin" "$scratch/dist"
export PATH="$scratch/bin:$PATH"
export DYLD_FALLBACK_LIBRARY_PATH="/usr/local/lib:/opt/homebrew/lib${DYLD_FALLBACK_LIBRARY_PATH:+:$DYLD_FALLBACK_LIBRARY_PATH}"
cat > "$scratch/bin/launchctl" <<'EOF'
#!/bin/bash
case $1 in
  print-disabled) printf 'disabled services = {\n    "com.eufy.wall" => disabled\n}\n' ;;
  print) exit 3 ;;
  disable) exit 0 ;;
  *) echo "unexpected launchctl command: $*" >&2; exit 1 ;;
esac
EOF
chmod 755 "$scratch/bin/launchctl"

archive() {
  local version=$1 name
  "$repo/deploy/package-release.sh" client "$version" "darwin-$arch" "$scratch/dist" >/dev/null
  name="$scratch/dist/eufy-wall-$version-darwin-$arch.tar.gz"
  (cd "$scratch/dist" && shasum -a 256 "${name##*/}") >> "$scratch/dist/SHA256SUMS"
  printf '%s' "$name"
}
install_archive() {
  local file=$1 digest
  shift
  digest=$(shasum -a 256 "$file")
  bash "$repo/deploy/install-client-macos.sh" --artifact "$file" \
    --checksums "$scratch/dist/SHA256SUMS" --trusted-sha256 "${digest%% *}" "$@"
}
first=$(archive v0.0.0-integration)
second=$(archive v0.0.1-integration)
base="$HOME/Library/Application Support/eufy-wall"
plist="$HOME/Library/LaunchAgents/com.eufy.wall.plist"
install_archive "$first" --verify-only
[[ ! -e $base && ! -e $plist ]]
install_archive "$first"
[[ $(cat "$base/current/VERSION") == v0.0.0-integration && ! -e $base/config.yaml ]]
[[ -f $base/config.example.yaml && -L $HOME/.local/bin/eufy-wall ]]
plutil -lint -s "$plist"
plist_hash=$(shasum -a 256 "$plist")
install_archive "$first"
[[ $(shasum -a 256 "$plist") == "$plist_hash" ]]

binary="$HOME/.local/bin/eufy-wall"
"$binary" config example > "$scratch/example.yaml"
"$binary" config validate "$scratch/example.yaml" >/dev/null
cat > "$scratch/manual.yaml" <<'EOF'
schema_version: 2
bridge_url: http://127.0.0.1:3000
rtsp_base: rtsp://127.0.0.1:8554
layout: custom
canvas: {cols: 32, rows: 32}
screen: {width: 320, height: 180}
sink: window
decoder: software
tiles:
  - id: test-camera
    camera: TESTCAMERA123
    rect: {x: 0, y: 0, w: 32, h: 32}
EOF
"$binary" config validate "$scratch/manual.yaml" >/dev/null
"$binary" config validate - < "$scratch/manual.yaml" >/dev/null
"$binary" layout preview "$scratch/manual.yaml" > "$scratch/layout.txt"
grep -q 'canvas 32x32; 1 tile(s)' "$scratch/layout.txt"
printf 'schema_version: 2\nunknown_field: true\n' > "$scratch/invalid.yaml"
if "$binary" config apply - < "$scratch/invalid.yaml" >/dev/null 2>&1; then
  echo 'accepted invalid manual YAML' >&2; exit 1
fi
[[ ! -e $base/config.yaml ]]

install_archive "$second"
[[ $(cat "$base/current/VERSION") == v0.0.1-integration ]]
[[ $(cat "$base/previous/VERSION") == v0.0.0-integration ]]
bash "$repo/deploy/install-client-macos.sh" --rollback
[[ $(cat "$base/current/VERSION") == v0.0.0-integration ]]
[[ $(shasum -a 256 "$plist") == "$plist_hash" ]]
echo 'packaged macOS client integration passed (launchd stubbed)'
