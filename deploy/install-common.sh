#!/usr/bin/env bash
# Shared, sourced by the two release installers. Linux/GNU utilities are required.
set -euo pipefail

die() { echo "install: $*" >&2; exit 1; }
say() { echo "install: $*" >&2; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing $1"; }

parse_install_args() {
  artifact= checksums= attestation_bundle= trusted_root= trusted_sha256= verify_only=0 rollback=0 no_apt=0
  while (($#)); do
    case $1 in
      --artifact) (($# >= 2)) || die '--artifact requires a path or HTTPS URL'; artifact=$2; shift 2 ;;
      --checksums) (($# >= 2)) || die '--checksums requires a path or HTTPS URL'; checksums=$2; shift 2 ;;
      --attestation-bundle) (($# >= 2)) || die '--attestation-bundle requires a path'; attestation_bundle=$2; shift 2 ;;
      --trusted-root) (($# >= 2)) || die '--trusted-root requires a path'; trusted_root=$2; shift 2 ;;
      --trusted-sha256) (($# >= 2)) || die '--trusted-sha256 requires a SHA-256 digest'; trusted_sha256=$2; shift 2 ;;
      --verify-only) verify_only=1; shift ;;
      --rollback) rollback=1; shift ;;
      --no-apt) no_apt=1; shift ;;
      *) die "unknown option: $1" ;;
    esac
  done
  if ((rollback)); then
    [[ -z $artifact && -z $checksums && -z $attestation_bundle && -z $trusted_root && -z $trusted_sha256 && $verify_only -eq 0 ]] || die '--rollback takes no artifact options'
  else
    [[ -n $artifact && -n $checksums ]] || die 'supply --artifact and --checksums together'
    [[ -z $attestation_bundle && -z $trusted_root || -n $attestation_bundle && -n $trusted_root ]] || die '--attestation-bundle and --trusted-root must be supplied together'
    [[ -z $trusted_sha256 || -z $attestation_bundle ]] || die 'choose either offline attestation or a separately trusted SHA-256 digest'
    [[ -z $trusted_sha256 || $trusted_sha256 =~ ^[0-9a-f]{64}$ ]] || die '--trusted-sha256 must be a lowercase 64-character hex digest'
  fi
}

linux_arch() {
  [[ $(uname -s) == Linux ]] || die 'Linux is required'
  case $(uname -m) in
    x86_64) echo amd64 ;; aarch64) echo arm64 ;; armv7l) echo armv7 ;; armv6l) echo armv6 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
}

fetch_input() {
  local source=$1 destination=$2
  case $source in
    https://*) need curl; curl -fsSL --retry 3 --output "$destination" "$source" ;;
    http://*) die 'artifact URLs must use HTTPS' ;;
    *) [[ -f $source ]] || die "file not found: $source"; cp "$source" "$destination" ;;
  esac
}

verify_release() {
  local kind=$1 wanted_arch=$2
  need sha256sum; need tar; need awk
  temp=$(mktemp -d)
  trap 'rm -rf "$temp"' EXIT
  local archive_name=${artifact##*/}
  archive_name=${archive_name%%\?*}
  local pattern
  if [[ $kind == server ]]; then
    pattern='^eufy-wall-bridge-(v?[0-9]+\.[0-9]+\.[0-9]+([.+_-][A-Za-z0-9.+_-]+)?)-linux-(amd64|arm64)\.tar\.gz$'
  else
    pattern='^eufy-wall-(v?[0-9]+\.[0-9]+\.[0-9]+([.+_-][A-Za-z0-9.+_-]+)?)-linux-(amd64|arm64|armv7|armv6)\.tar\.gz$'
  fi
  [[ $archive_name =~ $pattern ]] || die "unexpected artifact filename: $archive_name"
  local file_version=${BASH_REMATCH[1]}
  fetch_input "$artifact" "$temp/$archive_name"
  fetch_input "$checksums" "$temp/SHA256SUMS"
  local expected count actual
  count=$(awk -v file="$archive_name" '$2 == file { n++ } END { print n+0 }' "$temp/SHA256SUMS")
  [[ $count -eq 1 ]] || die "expected exactly one checksum for $archive_name"
  expected=$(awk -v file="$archive_name" '$2 == file { print $1 }' "$temp/SHA256SUMS")
  [[ $expected =~ ^[0-9a-f]{64}$ ]] || die 'invalid lowercase SHA-256 value'
  actual=$(sha256sum "$temp/$archive_name")
  actual=${actual%% *}
  [[ $actual == "$expected" ]] || die "SHA-256 mismatch for $archive_name"
  archive_hash=$actual
  # The manifest can be replaced with the artifact. Require a separate trust root.
  if [[ -n $trusted_sha256 ]]; then
    [[ $actual == "$trusted_sha256" ]] || die 'artifact does not match the separately trusted SHA-256 digest'
    say 'artifact matches separately trusted SHA-256 digest'
  else
    need gh
    local verify_args=(attestation verify "$temp/$archive_name" --repo sapireli/eufy-p2p-rtsp-bridge \
      --signer-workflow sapireli/eufy-p2p-rtsp-bridge/.github/workflows/release.yml \
      --source-ref "refs/tags/$file_version")
    if [[ -n $attestation_bundle ]]; then
      [[ -f $attestation_bundle && -f $trusted_root ]] || die 'offline attestation bundle and trusted root must be local files'
      verify_args+=(--bundle "$attestation_bundle" --custom-trusted-root "$trusted_root")
    fi
    gh "${verify_args[@]}" >/dev/null || die 'artifact provenance verification failed'
    say 'GitHub attestation verified for this repository, release workflow, and tag'
  fi
  # Accept only regular files and directories. Symlinks/hardlinks can escape
  # the extraction directory even when every displayed member path is safe.
  LC_ALL=C tar -tvzf "$temp/$archive_name" | awk '
    substr($0,1,1) != "-" && substr($0,1,1) != "d" { bad=1 }
    END { exit bad }
  ' || die 'archive contains links or special files'
  # Reject absolute paths, parent traversal, and entries outside the expected top-level folder.
  LC_ALL=C tar -tzf "$temp/$archive_name" | awk -v prefix="eufy-wall-$kind/" '
    $0 !~ ("^" prefix) && $0 != substr(prefix,1,length(prefix)-1) { bad=1 }
    $0 ~ /^\// || $0 ~ /(^|\/)\.\.($|\/)/ { bad=1 }
    END { exit bad }
  ' || die 'unsafe archive paths'
  mkdir "$temp/unpacked"
  tar -xzf "$temp/$archive_name" -C "$temp/unpacked" --no-same-owner --no-same-permissions
  payload="$temp/unpacked/eufy-wall-$kind"
  version=$(cat "$payload/VERSION")
  release_arch=$(cat "$payload/ARCH")
  [[ $version =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([.+_-][A-Za-z0-9.+_-]+)?$ ]] || die 'invalid version in artifact'
  [[ $release_arch == "$wanted_arch" ]] || die "artifact is $release_arch, host is $wanted_arch"
  [[ $archive_name == *"-${version}-linux-${release_arch}.tar.gz" ]] || die 'filename does not match artifact metadata'
  say "verified $archive_name (SHA-256 $archive_hash)"
}

ensure_debian_packages() {
  local missing=() package
  need dpkg-query
  for package in "$@"; do
    dpkg-query -W -f='${Status}' "$package" 2>/dev/null | grep -q '^install ok installed$' || missing+=("$package")
  done
  ((${#missing[@]})) || return 0
  say "missing OS packages: ${missing[*]}"
  ((no_apt == 0)) || die 'install OS packages from a local apt mirror/cache, then retry without --no-apt'
  need apt-get
  DEBIAN_FRONTEND=noninteractive apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}"
}

atomic_link() {
  local target=$1 link=$2
  local next="${link}.next.$$"
  ln -s "$target" "$next"
  mv -Tf "$next" "$link"
}

# A link may already point at the new release after power loss, while the service
# still runs the old process. The pending marker keeps retries from treating it
# as a finished install.
needs_activation() {
  local current=$1 desired=$2 marker=$3
  [[ $current != "$desired" || -f $marker ]]
}

wait_active() {
  local service=$1 seconds=${2:-20} i pid previous= steady=0
  for ((i=0; i<seconds; i++)); do
    if systemctl is-active --quiet "$service"; then
      pid=$(systemctl show "$service" --property=MainPID --value)
      if [[ -n $pid && $pid != 0 && $pid == "$previous" ]]; then
        ((steady+=1))
        ((steady >= 5)) && return 0
      else
        steady=0
      fi
      previous=$pid
    else
      steady=0
      previous=
    fi
    sleep 1
  done
  return 1
}
