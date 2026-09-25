#!/usr/bin/env bash
# Build a self-contained release archive on Linux. Run after npm ci / Go build.
set -euo pipefail

usage() { echo 'usage: package-release.sh server|client VERSION amd64|arm64|armv7|armv6 OUTPUT_DIR' >&2; exit 2; }
[[ $# -eq 4 ]] || usage
kind=$1 version=$2 arch=$3 output=$4
[[ $version =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([.+_-][A-Za-z0-9.+_-]+)?$ ]] || usage
[[ $arch =~ ^(amd64|arm64|armv7|armv6)$ ]] || usage
repo=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$output"
output=$(cd "$output" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
root="$work/eufy-wall-$kind"
mkdir -p "$root"
printf '%s\n' "$version" > "$root/VERSION"
printf '%s\n' "$arch" > "$root/ARCH"

if [[ $kind == server ]]; then
  [[ $arch == amd64 || $arch == arm64 ]] || { echo 'server releases support amd64 and arm64' >&2; exit 2; }
  [[ -d $repo/server/node_modules/@mega-yfue/eufy-sdk ]] || { echo 'run npm ci in server/ first' >&2; exit 1; }
  [[ -f $repo/server/cli.mjs ]] || { echo 'missing server/cli.mjs' >&2; exit 1; }
  mkdir -p "$root/server" "$root/bin" "$root/deploy"
  cp -a "$repo/server/server.mjs" "$repo/server/src" "$repo/server/config.example.yaml" "$repo/server/package.json" "$repo/server/package-lock.json" "$repo/server/node_modules" "$root/server/"
  # Runtime never invokes npm bin shims; remove these symlinks so installer
  # extraction can reject every link type without exception.
  rm -rf "$root/server/node_modules/.bin"
  cp "$repo/server/cli.mjs" "$root/server/"
  cp "$repo/deploy/eufy-wall-bridge.service" "$repo/deploy/eufy-wall-bridge.env.example" "$repo/deploy/install-server.sh" "$repo/deploy/install-common.sh" "$root/deploy/"
  node_version=24.5.0
  case $arch in amd64) node_arch=x64; node_hash=32edb1f2aeaf8ea0d484af33bf3b5d8330d7d33c9cd8c70f811b8a643822e613; go_hash=32d616af226bd731678ffde328b94cfb94e30339bfefc469cfb76323144615a6 ;;
    arm64) node_arch=arm64; node_hash=313367534186a8551d68b39fbc2a6cc36638e583fb5dc75dcf5da3c6582bff3b; go_hash=359fabade8a7a51e81a55fe6df6b0ef81764a5e1d63179577534eaaa71904b50 ;;
  esac
  node_archive="node-v${node_version}-linux-${node_arch}.tar.xz"
  curl -fsSL --retry 3 -o "$work/$node_archive" "https://nodejs.org/dist/v${node_version}/${node_archive}"
  printf '%s  %s\n' "$node_hash" "$work/$node_archive" | sha256sum -c - >/dev/null
  tar -xJf "$work/$node_archive" -C "$work" "node-v${node_version}-linux-${node_arch}/bin/node"
  cp "$work/node-v${node_version}-linux-${node_arch}/bin/node" "$root/bin/node"
  curl -fsSL --retry 3 -o "$root/bin/go2rtc" "https://github.com/AlexxIT/go2rtc/releases/download/v1.9.14/go2rtc_linux_${arch}"
  printf '%s  %s\n' "$go_hash" "$root/bin/go2rtc" | sha256sum -c - >/dev/null
  chmod 755 "$root/bin/node" "$root/bin/go2rtc"
  printf 'node=%s\ngo2rtc=%s\n' "$node_version" '1.9.14' > "$root/DEPENDENCIES"
  name="eufy-wall-bridge-${version}-linux-${arch}.tar.gz"
elif [[ $kind == client ]]; then
  [[ -f $repo/client/bin/eufy-wall-$arch ]] || { echo "missing client/bin/eufy-wall-$arch; build client first" >&2; exit 1; }
  mkdir -p "$root/deploy"
  cp "$repo/client/bin/eufy-wall-$arch" "$root/eufy-wall"
  cp "$repo/client/config.example.yaml" "$root/config.example.yaml"
  cp "$repo/deploy/eufy-wall.service" "$repo/deploy/install-client.sh" "$repo/deploy/install-common.sh" "$repo/deploy/qualify-client.py" "$root/deploy/"
  chmod 755 "$root/eufy-wall"
  name="eufy-wall-${version}-linux-${arch}.tar.gz"
else
  usage
fi

[[ -z $(find "$root" -type l -print -quit) ]] || { echo 'release payload contains a symlink' >&2; exit 1; }
[[ -z $(find "$root" -type f -links +1 -print -quit) ]] || { echo 'release payload contains a hardlink' >&2; exit 1; }

tar -czf "$output/$name" -C "$work" "eufy-wall-$kind"
(cd "$output" && sha256sum "$name")
