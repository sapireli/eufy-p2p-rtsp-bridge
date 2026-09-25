#!/bin/bash
# Verified per-user macOS release installer; never run with sudo.
set -euo pipefail

die() { echo "install-macos: $*" >&2; exit 1; }
say() { echo "install-macos: $*" >&2; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing $1"; }

artifact= checksums= trusted_sha256= attestation_bundle= trusted_root= verify_only=0 rollback=0
while (($#)); do
  case $1 in
    --artifact|--checksums|--trusted-sha256|--attestation-bundle|--trusted-root)
      (($# >= 2)) || die "$1 requires a value"
      case $1 in
        --artifact) artifact=$2 ;; --checksums) checksums=$2 ;; --trusted-sha256) trusted_sha256=$2 ;;
        --attestation-bundle) attestation_bundle=$2 ;; --trusted-root) trusted_root=$2 ;;
      esac
      shift 2 ;;
    --verify-only) verify_only=1; shift ;;
    --rollback) rollback=1; shift ;;
    *) die "unknown option: $1" ;;
  esac
done
if ((rollback)); then
  [[ -z $artifact && -z $checksums && -z $trusted_sha256 && -z $attestation_bundle && -z $trusted_root && $verify_only -eq 0 ]] || die '--rollback takes no artifact options'
else
  [[ -n $artifact && -n $checksums ]] || die 'supply --artifact and --checksums together'
  [[ -z $attestation_bundle && -z $trusted_root || -n $attestation_bundle && -n $trusted_root ]] || die '--attestation-bundle and --trusted-root must be supplied together'
  [[ -z $trusted_sha256 || -z $attestation_bundle ]] || die 'choose an attestation or a separately trusted digest'
  [[ -z $trusted_sha256 || $trusted_sha256 =~ ^[0-9a-f]{64}$ ]] || die '--trusted-sha256 must be a lowercase SHA-256 digest'
fi
[[ $(uname -s) == Darwin ]] || die 'macOS is required'
case $(uname -m) in x86_64) arch=amd64 ;; arm64) arch=arm64 ;; *) die "unsupported Mac architecture: $(uname -m)" ;; esac
[[ $HOME == /* && $HOME != *$'\n'* && $HOME != *$'\r'* ]] || die 'HOME must be an absolute path without control characters'
base="$HOME/Library/Application Support/eufy-wall"
releases="$base/releases"
current="$base/current"
previous="$base/previous"
plist="$HOME/Library/LaunchAgents/com.eufy.wall.plist"
label="gui/$(id -u)/com.eufy.wall"
domain="gui/$(id -u)"
command_link="$HOME/.local/bin/eufy-wall"
if [[ $arch == arm64 ]]; then
  brew_bin=/opt/homebrew/bin:/usr/local/bin
  brew_lib=/opt/homebrew/lib:/usr/local/lib
else
  brew_bin=/usr/local/bin:/opt/homebrew/bin
  brew_lib=/usr/local/lib:/opt/homebrew/lib
fi
export PATH="$PATH:$brew_bin:/usr/bin:/bin:/usr/sbin:/sbin"
temp= stage=
cleanup() { [[ -z $stage ]] || rm -rf "$stage"; [[ -z $temp ]] || rm -rf "$temp"; }
trap cleanup EXIT

hash_file() { shasum -a 256 "$1" | awk '{print $1}'; }
fetch() {
  case $1 in
    https://*) need curl; curl -fsSL --retry 3 -o "$2" "$1" ;;
    http://*) die 'artifact URLs must use HTTPS' ;;
    *) [[ -f $1 ]] || die "file not found: $1"; cp "$1" "$2" ;;
  esac
}
atomic_link() {
  local target=$1 link=$2 next="${2}.next.$$"
  ln -s "$target" "$next"
  mv -fh "$next" "$link"
}
atomic_copy() {
  local source=$1 target=$2 next="${2}.next.$$"
  cp -p "$source" "$next"
  mv -fh "$next" "$target"
}
write_marker() {
  printf '%s\n' "$2" > "${1}.next.$$"
  mv -fh "${1}.next.$$" "$1"
}
loaded() { launchctl print "$label" >/dev/null 2>&1; }
pid() { launchctl print "$label" 2>/dev/null | awk '/^[[:space:]]*pid = [0-9]+/ { print $3; exit }'; }
wait_running() {
  local i last= now= steady=0
  for ((i=0; i<20; i++)); do
    now=$(pid)
    if [[ -n $now && $now != 0 && $now == "$last" ]]; then
      ((steady+=1))
      ((steady >= 3)) && return 0
    else
      steady=0
    fi
    last=$now
    sleep 1
  done
  return 1
}
restart_loaded() {
  if loaded; then launchctl bootout "$label" || return 1; fi
  launchctl bootstrap "$domain" "$plist" || return 1
  wait_running || return 1
  "$current/eufy-wall" health >/dev/null
}
restore_previous() {
  local target=$1 snapshot=$2 was_loaded=$3
  [[ -d $target && -f $snapshot ]] || die 'previous binary or launchd plist snapshot is missing'
  if loaded; then launchctl bootout "$label" || die 'could not unload the failed launchd job'; fi
  atomic_link "$target" "$current"
  atomic_copy "$snapshot" "$plist"
  if [[ $was_loaded == 1 ]]; then
    launchctl bootstrap "$domain" "$plist" || die 'prior launchd job could not be restored'
    wait_running || die 'prior launchd job did not remain running'
    "$current/eufy-wall" health >/dev/null || die 'prior wall did not regain frame progress'
  else
    launchctl disable "$label" || die 'could not leave the restored launchd job disabled'
  fi
}
verify_archive() {
  need shasum; need tar; need awk
  temp=$(mktemp -d)
  name=${artifact##*/}; name=${name%%\?*}
  pattern='^eufy-wall-(v?[0-9]+\.[0-9]+\.[0-9]+([.+_-][A-Za-z0-9.+_-]+)?)-darwin-(amd64|arm64)\.tar\.gz$'
  [[ $name =~ $pattern ]] || die "unexpected artifact filename: $name"
  file_version=${BASH_REMATCH[1]}
  fetch "$artifact" "$temp/$name"
  fetch "$checksums" "$temp/SHA256SUMS"
  local count expected actual
  count=$(awk -v file="$name" '$2 == file { n++ } END { print n+0 }' "$temp/SHA256SUMS")
  [[ $count -eq 1 ]] || die "expected exactly one checksum for $name"
  expected=$(awk -v file="$name" '$2 == file { print $1 }' "$temp/SHA256SUMS")
  [[ $expected =~ ^[0-9a-f]{64}$ ]] || die 'invalid SHA-256 value'
  actual=$(hash_file "$temp/$name")
  [[ $actual == "$expected" ]] || die 'SHA-256 mismatch'
  digest=$actual
  if [[ -n $trusted_sha256 ]]; then
    [[ $digest == "$trusted_sha256" ]] || die 'archive does not match the separately trusted digest'
  else
    need gh
    local verify_args=(attestation verify "$temp/$name" --repo sapireli/eufy-p2p-rtsp-bridge \
      --signer-workflow sapireli/eufy-p2p-rtsp-bridge/.github/workflows/release.yml \
      --source-ref "refs/tags/$file_version")
    if [[ -n $attestation_bundle ]]; then
      [[ -f $attestation_bundle && -f $trusted_root ]] || die 'offline attestation bundle and trusted root must be local files'
      verify_args+=(--bundle "$attestation_bundle" --custom-trusted-root "$trusted_root")
    fi
    gh "${verify_args[@]}" >/dev/null || die 'GitHub attestation verification failed'
  fi
  LC_ALL=C tar -tvzf "$temp/$name" | awk '
    substr($0,1,1) != "-" && substr($0,1,1) != "d" { bad=1 }
    END { exit bad }
  ' || die 'archive contains links or special files'
  LC_ALL=C tar -tzf "$temp/$name" | awk '
    $0 !~ /^eufy-wall-client\// && $0 != "eufy-wall-client" { bad=1 }
    $0 ~ /^\// || $0 ~ /(^|\/)\.\.($|\/)/ { bad=1 }
    END { exit bad }
  ' || die 'unsafe archive paths'
  mkdir "$temp/unpacked"
  tar -xzf "$temp/$name" -C "$temp/unpacked"
  payload="$temp/unpacked/eufy-wall-client"
  version=$(cat "$payload/VERSION")
  release_arch=$(cat "$payload/ARCH")
  [[ $(cat "$payload/OS") == darwin && $release_arch == "$arch" && $version == "$file_version" ]] || die 'archive metadata does not match its name or host'
  [[ -x $payload/eufy-wall && -f $payload/config.example.yaml && -f $payload/deploy/install-client-macos.sh ]] || die 'archive is incomplete'
  say "verified $name (SHA-256 $digest)"
}

if ((rollback)); then
  [[ $EUID -ne 0 ]] || die 'run as the login user, never with sudo'
  need launchctl
  if [[ -f $base/.upgrade-pending ]]; then
    target=$(cat "$base/.upgrade-pending")
    prior_loaded=$(cat "$base/.upgrade-loaded" 2>/dev/null || echo 1)
    [[ $target != none ]] || die 'first install has no prior binary to restore; rerun the installer'
    restore_previous "$target" "$base/.upgrade-plist" "$prior_loaded"
    atomic_link "$target" "$previous"
    rm -f "$base/.upgrade-pending" "$base/.upgrade-loaded" "$base/.upgrade-plist"
    say "rolled back interrupted upgrade to $(cat "$target/VERSION")"
  else
    [[ -L $previous ]] || die 'no previous release is available'
    target=$(readlink "$previous")
    was_loaded=0; loaded && was_loaded=1
    restore_previous "$target" "$base/previous-plist" "$was_loaded"
    say "rolled back to $(cat "$target/VERSION")"
  fi
  exit 0
fi

verify_archive
if ((verify_only)); then say "release $version is valid for this Mac"; exit 0; fi
[[ $EUID -ne 0 ]] || die 'run as the login user, never with sudo'
need launchctl; need plutil; need mv; need cp
for tool in gst-launch-1.0 gst-inspect-1.0; do need "$tool"; done
for element in compositor autovideosink watchdog; do
  gst-inspect-1.0 --exists "$element" >/dev/null 2>&1 || die "missing GStreamer element $element; run 'brew install gstreamer'"
done
for library in libgstreamer-1.0.dylib libglib-2.0.dylib; do
  [[ -f /opt/homebrew/lib/$library || -f /usr/local/lib/$library ]] || die "missing Homebrew $library; run 'brew install gstreamer'"
done
[[ ! -e $current || -L $current ]] || die "$current exists but is not a release symlink"
if [[ -L $current && ! -d $(readlink "$current") && ! -f $base/.upgrade-pending ]]; then
  die "$current points to a missing release"
fi
[[ ! -e $command_link || -L $command_link ]] || die "$command_link exists and is not a symlink"
if [[ -L $command_link && $(readlink "$command_link") != "$current/eufy-wall" ]]; then
  die "$command_link points to another program"
fi
if [[ ! -L $current && -f $plist ]]; then die 'existing launchd plist needs manual migration before release install'; fi

mkdir -p "$releases" "$base" "$HOME/Library/LaunchAgents" "$HOME/.local/bin"
chmod 700 "$base"
release="$releases/$version-$arch"
if [[ -e $release ]]; then
  [[ -f $release/.archive-sha256 && $(cat "$release/.archive-sha256") == "$digest" ]] || die 'release path exists with different content'
else
  stage="$releases/.stage-$$"
  cp -R "$payload" "$stage"
  printf '%s\n' "$digest" > "$stage/.archive-sha256"
  mv "$stage" "$release"
  stage=
fi
[[ -f $base/config.example.yaml ]] || cp "$release/config.example.yaml" "$base/config.example.yaml"

old= was_loaded=0
[[ -L $current ]] && old=$(readlink "$current")
loaded && was_loaded=1
if [[ -f $base/.upgrade-pending ]]; then
  old=$(cat "$base/.upgrade-pending")
  [[ $old == none || -d $old ]] || die 'pending upgrade refers to a missing release'
  [[ $old == none ]] && old=
  was_loaded=$(cat "$base/.upgrade-loaded" 2>/dev/null || echo 1)
fi

# Generate a launchd job with stable paths, including Homebrew tool and library lookup.
xml_escape() { local value=$1; value=${value//&/&amp;}; value=${value//</&lt;}; value=${value//>/&gt;}; printf '%s' "$value"; }
new_plist="$temp/com.eufy.wall.plist"
cat > "$new_plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.eufy.wall</string>
  <key>ProgramArguments</key><array>
    <string>$(xml_escape "$current/eufy-wall")</string>
    <string>-config</string><string>$(xml_escape "$base/config.yaml")</string>
  </array>
  <key>WorkingDirectory</key><string>$(xml_escape "$base")</string>
  <key>EnvironmentVariables</key><dict>
    <key>PATH</key><string>$(xml_escape "$brew_bin:/usr/bin:/bin:/usr/sbin:/sbin")</string>
    <key>DYLD_FALLBACK_LIBRARY_PATH</key><string>$(xml_escape "$brew_lib")</string>
  </dict>
  <key>StandardOutPath</key><string>$(xml_escape "$base/wall.out.log")</string>
  <key>StandardErrorPath</key><string>$(xml_escape "$base/wall.err.log")</string>
  <key>Umask</key><integer>63</integer>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
EOF
plutil -lint -s "$new_plist" || die 'generated launchd plist is invalid'
if [[ -L $current && $(readlink "$current") == "$release" && ! -f $base/.upgrade-pending ]] && cmp -s "$new_plist" "$plist" && [[ -L $command_link ]]; then
  say "$version already installed; leaving launchd state unchanged"
  exit 0
fi
if [[ ! -f $base/.upgrade-pending ]]; then
  if [[ -f $plist ]]; then cp -p "$plist" "$base/.upgrade-plist"; else rm -f "$base/.upgrade-plist"; fi
  write_marker "$base/.upgrade-loaded" "$was_loaded"
  if [[ -n $old ]]; then write_marker "$base/.upgrade-pending" "$old"; else write_marker "$base/.upgrade-pending" none; fi
fi
if [[ -z $old && $was_loaded == 0 ]]; then
  launchctl disable "$label" || die 'could not disable unconfigured launchd job'
fi
atomic_copy "$new_plist" "$plist"
chmod 600 "$plist"
atomic_link "$release" "$current"
atomic_link "$current/eufy-wall" "$command_link"
if [[ $was_loaded == 1 ]]; then
  if ! restart_loaded; then
    say 'new release failed to stay running; restoring previous release'
    if [[ -n $old ]]; then
      restore_previous "$old" "$base/.upgrade-plist" 1
      rm -f "$base/.upgrade-pending" "$base/.upgrade-loaded" "$base/.upgrade-plist"
      die 'upgrade failed; previous release restored'
    fi
    die 'new launchd job failed and no previous release exists'
  fi
fi
if [[ -n $old ]]; then atomic_link "$old" "$previous"; fi
if [[ -f $base/.upgrade-plist ]]; then atomic_copy "$base/.upgrade-plist" "$base/previous-plist"; fi
rm -f "$base/.upgrade-pending" "$base/.upgrade-loaded" "$base/.upgrade-plist"
say "installed $version for macOS $arch; run $command_link setup before starting the wall"
