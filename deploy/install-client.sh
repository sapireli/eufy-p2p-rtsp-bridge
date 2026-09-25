#!/usr/bin/env bash
# Install a verified, versioned eufy-wall binary on Debian or Raspberry Pi OS.
set -euo pipefail
source "$(dirname "$0")/install-common.sh"
parse_install_args "$@"
arch=$(linux_arch)
base=/opt/eufy-wall
service=eufy-wall

if ((rollback)); then
  [[ $EUID -eq 0 ]] || die 'run rollback as root'
  need systemctl
  prior_active=0
  systemctl is-active --quiet "$service" && prior_active=1 || true
  if [[ -f $base/.upgrade-pending ]]; then
    pending_target=$(cat "$base/.upgrade-pending")
    prior_active=$(cat "$base/.upgrade-active" 2>/dev/null || echo 1)
    [[ -d $pending_target ]] || die "pending upgrade has no prior binary: $pending_target"
    rollback_release_unit "$pending_target" "$base/.upgrade-unit" "$base/current" "/etc/systemd/system/$service.service" "$service" "$prior_active"
    atomic_link "$pending_target" "$base/previous"
    rm -f "$base/.upgrade-pending" "$base/.upgrade-active" "$base/.upgrade-unit"
    say 'interrupted client upgrade rolled back to its prior running service'
    exit 0
  fi
  [[ -L $base/previous ]] || die 'no previous client binary is available'
  target=$(readlink "$base/previous")
  [[ -x $target/eufy-wall ]] || die "previous client binary is missing: $target"
  unit_snapshot="$base/previous-unit.service"
  if [[ ! -f $unit_snapshot ]]; then
    say 'prior installed unit snapshot is missing; using packaged unit defaults'
    unit_snapshot="$target/deploy/eufy-wall.service"
  fi
  rollback_release_unit "$target" "$unit_snapshot" "$base/current" "/etc/systemd/system/$service.service" "$service" "$prior_active"
  say "rolled back to $(cat "$target/VERSION")"
  exit 0
fi

verify_release client "$arch"
[[ -x $payload/eufy-wall && -f $payload/config.example.yaml ]] || die 'client archive is incomplete'
if ((verify_only)); then say "release $version is valid for this host"; exit 0; fi
[[ $EUID -eq 0 ]] || die 'run install as root (or use --verify-only)'
need systemctl; need useradd; need install; need mv; need readlink
[[ -f /etc/os-release ]] || die 'missing /etc/os-release'
source /etc/os-release
[[ ${ID:-} == debian || ${ID:-} == ubuntu || " ${ID_LIKE:-} " == *' debian '* ]] || die 'Debian or Raspberry Pi OS is required'
ensure_debian_packages gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-libav libdrm-tests
require_gstreamer_elements appsink appsrc compositor watchdog videoconvert videoscale videorate
require_gstreamer_app_library

release="$base/releases/$version-$arch"
install -d -m 755 "$base/releases"
if [[ -e $release ]]; then
  [[ -f $release/.archive-sha256 && $(cat "$release/.archive-sha256") == "$archive_hash" ]] || die "release $release already exists with different content"
else
  stage="$base/releases/.stage-$$"
  trap 'rm -rf "$stage" "$temp"' EXIT
  cp -a "$payload" "$stage"
  printf '%s\n' "$archive_hash" > "$stage/.archive-sha256"
  chown -R root:root "$stage"
  mv "$stage" "$release"
fi

id -u wall >/dev/null 2>&1 || useradd --system --shell /usr/sbin/nologin --groups video,render wall
if [[ ! -e /etc/eufy-wall.example.yaml ]]; then
  install -m 644 "$release/config.example.yaml" /etc/eufy-wall.example.yaml
  say 'installed /etc/eufy-wall.example.yaml; run eufy-wall setup or apply YAML before starting'
fi

current_release= old_current= old_active=0
[[ -L $base/current ]] && current_release=$(readlink "$base/current")
systemctl is-active --quiet "$service" && old_active=1 || true
if [[ -f $base/.upgrade-pending ]]; then
  old_current=$(cat "$base/.upgrade-pending")
  upgrade_active=$(cat "$base/.upgrade-active" 2>/dev/null || echo 1)
  [[ $old_current == none || -d $old_current ]] || die 'pending upgrade names a missing previous release'
  [[ $old_current == none ]] && old_current=
else
  old_current=$current_release
  upgrade_active=$old_active
fi
if ! needs_activation "$current_release" "$release" "$base/.upgrade-pending" && cmp -s "$release/deploy/eufy-wall.service" "/etc/systemd/system/$service.service" && [[ -L /usr/local/bin/eufy-wall && $(readlink /usr/local/bin/eufy-wall) == "$base/current/eufy-wall" ]]; then
  say "$version already installed; leaving service running"; exit 0
fi
if [[ -z $old_current && -f /usr/local/bin/eufy-wall && ! -L /usr/local/bin/eufy-wall ]]; then
  legacy="$base/releases/legacy-$(date -u +%Y%m%d%H%M%S)"
  install -d -m 755 "$legacy"
  cp -p /usr/local/bin/eufy-wall "$legacy/eufy-wall"
  printf 'legacy\n' > "$legacy/VERSION"
  old_current=$legacy
fi

if [[ -f /etc/systemd/system/$service.service && ! -e $base/legacy.service ]]; then
  cp -p "/etc/systemd/system/$service.service" "$base/legacy.service"
fi
if [[ ! -f $base/.upgrade-pending ]]; then
  if [[ -f /etc/systemd/system/$service.service ]]; then
    cp -p "/etc/systemd/system/$service.service" "$base/.upgrade-unit"
  else
    rm -f "$base/.upgrade-unit"
  fi
  write_marker "$base/.upgrade-active" "$upgrade_active"
  if [[ -n $old_current ]]; then write_marker "$base/.upgrade-pending" "$old_current"
  else write_marker "$base/.upgrade-pending" none; fi
fi
install_unit "$release/deploy/eufy-wall.service" "/etc/systemd/system/$service.service"
atomic_link "$release" "$base/current"
install -d -m 755 /usr/local/bin
ln -sfn "$base/current/eufy-wall" /usr/local/bin/eufy-wall
systemctl daemon-reload

if ((upgrade_active || old_active)); then
  if ! systemctl restart "$service" || ! wait_active "$service" || ! /usr/local/bin/eufy-wall health; then
    say 'new client failed to stay active; restoring previous binary'
    if [[ -n $old_current ]]; then
      rollback_release_unit "$old_current" "$base/.upgrade-unit" "$base/current" "/etc/systemd/system/$service.service" "$service"
    else
      systemctl stop "$service" || true
      die 'new client failed; no prior binary was installed'
    fi
    rm -f "$base/.upgrade-pending" "$base/.upgrade-active"
    rm -f "$base/.upgrade-unit"
    if [[ -n $old_current ]] && ! /usr/local/bin/eufy-wall health; then
      die 'upgrade failed; previous binary restored but frame progress did not recover'
    fi
    die 'upgrade failed; inspect journalctl -u eufy-wall'
  fi
fi
if [[ -n $old_current ]]; then atomic_link "$old_current" "$base/previous"; fi
if [[ -f $base/.upgrade-unit ]]; then copy_unit_exact "$base/.upgrade-unit" "$base/previous-unit.service"; fi
rm -f "$base/.upgrade-pending" "$base/.upgrade-active"
rm -f "$base/.upgrade-unit"
say "installed client $version for $arch; $( ((upgrade_active || old_active)) && echo upgraded-running-service || echo run-eufy-wall-setup-then-enable-service )"
