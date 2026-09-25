#!/usr/bin/env bash
# Install a verified, versioned eufy-wall binary on Debian or Raspberry Pi OS.
set -euo pipefail
source "$(dirname "$0")/install-common.sh"
parse_install_args "$@"
arch=$(linux_arch)
base=/opt/eufy-wall
service=eufy-wall
template=/etc/systemd/system/eufy-wall@.service

active_named_services() {
  local output unit rest
  output=$(systemctl list-units --type=service --state=active --plain --no-legend 'eufy-wall@*.service') ||
    die 'could not inspect running wall instances'
  named_units=()
  while read -r unit rest; do
    [[ -z $unit ]] && continue
    [[ $unit =~ ^eufy-wall@[A-Za-z0-9_-]+\.service$ ]] || die "unexpected wall instance unit: $unit"
    named_units+=("$unit")
  done <<< "$output"
}

restore_template() {
  local snapshot=$1
  if [[ -f $snapshot ]]; then copy_unit_exact "$snapshot" "$template"
  else rm -f "$template"; fi
  systemctl daemon-reload
}

restart_named_services() {
  local unit instance
  for unit in "${named_units[@]}"; do
    instance=${unit#eufy-wall@}; instance=${instance%.service}
    systemctl restart "$unit" || return 1
    wait_active "$unit" || return 1
    /usr/local/bin/eufy-wall health --instance "$instance" || return 1
  done
}

if ((rollback)); then
  [[ $EUID -eq 0 ]] || die 'run rollback as root'
  need systemctl
  active_named_services
  prior_active=0
  systemctl is-active --quiet "$service" && prior_active=1 || true
  if [[ -f $base/.upgrade-pending ]]; then
    pending_target=$(cat "$base/.upgrade-pending")
    prior_active=$(cat "$base/.upgrade-active" 2>/dev/null || echo 1)
    [[ -d $pending_target ]] || die "pending upgrade has no prior binary: $pending_target"
    template_snapshot="$base/.upgrade-template"
    [[ -f $template_snapshot ]] || template_snapshot="$pending_target/deploy/eufy-wall@.service"
    (( ${#named_units[@]} == 0 )) || [[ -f $template_snapshot ]] || die 'prior release has no unit for running named instances'
    restore_template "$template_snapshot"
    rollback_release_unit "$pending_target" "$base/.upgrade-unit" "$base/current" "/etc/systemd/system/$service.service" "$service" "$prior_active"
    restart_named_services || die 'prior named wall instance did not regain frame progress'
    atomic_link "$pending_target" "$base/previous"
    rm -f "$base/.upgrade-pending" "$base/.upgrade-active" "$base/.upgrade-unit" "$base/.upgrade-template"
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
  template_snapshot="$base/previous-template.service"
  [[ -f $template_snapshot ]] || template_snapshot="$target/deploy/eufy-wall@.service"
  (( ${#named_units[@]} == 0 )) || [[ -f $template_snapshot ]] || die 'prior release has no unit for running named instances'
  restore_template "$template_snapshot"
  rollback_release_unit "$target" "$unit_snapshot" "$base/current" "/etc/systemd/system/$service.service" "$service" "$prior_active"
  restart_named_services || die 'prior named wall instance did not regain frame progress'
  say "rolled back to $(cat "$target/VERSION")"
  exit 0
fi

verify_release client "$arch"
[[ -x $payload/eufy-wall && -f $payload/config.example.yaml && -f $payload/deploy/eufy-wall.service && -f $payload/deploy/eufy-wall@.service ]] || die 'client archive is incomplete'
if ((verify_only)); then say "release $version is valid for this host"; exit 0; fi
[[ $EUID -eq 0 ]] || die 'run install as root (or use --verify-only)'
need systemctl; need useradd; need install; need mv; need readlink
active_named_services
[[ -f /etc/os-release ]] || die 'missing /etc/os-release'
source /etc/os-release
[[ ${ID:-} == debian || ${ID:-} == ubuntu || " ${ID_LIKE:-} " == *' debian '* ]] || die 'Debian or Raspberry Pi OS is required'
ensure_debian_packages gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-libav libdrm-tests
require_gstreamer_version
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
if ((${#named_units[@]})) && [[ -n $old_current && ! -f $template && ! -f $old_current/deploy/eufy-wall@.service ]]; then
  die 'running named instances need a prior template unit for safe rollback'
fi
if ! needs_activation "$current_release" "$release" "$base/.upgrade-pending" && cmp -s "$release/deploy/eufy-wall.service" "/etc/systemd/system/$service.service" && cmp -s "$release/deploy/eufy-wall@.service" /etc/systemd/system/eufy-wall@.service && [[ -L /usr/local/bin/eufy-wall && $(readlink /usr/local/bin/eufy-wall) == "$base/current/eufy-wall" ]]; then
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
  if [[ -f $template ]]; then cp -p "$template" "$base/.upgrade-template"
  else rm -f "$base/.upgrade-template"; fi
  write_marker "$base/.upgrade-active" "$upgrade_active"
  if [[ -n $old_current ]]; then write_marker "$base/.upgrade-pending" "$old_current"
  else write_marker "$base/.upgrade-pending" none; fi
fi
install_unit "$release/deploy/eufy-wall.service" "/etc/systemd/system/$service.service"
install_unit "$release/deploy/eufy-wall@.service" "$template"
atomic_link "$release" "$base/current"
install -d -m 755 /usr/local/bin
ln -sfn "$base/current/eufy-wall" /usr/local/bin/eufy-wall
systemctl daemon-reload

if ((upgrade_active || old_active || ${#named_units[@]})); then
  activation_ok=1
  if ((upgrade_active || old_active)); then
    systemctl restart "$service" && wait_active "$service" && /usr/local/bin/eufy-wall health || activation_ok=0
  fi
  if ((activation_ok)); then restart_named_services || activation_ok=0; fi
  if ((!activation_ok)); then
    say 'new client failed to stay active; restoring previous binary'
    if [[ -n $old_current ]]; then
      template_snapshot="$base/.upgrade-template"
      [[ -f $template_snapshot ]] || template_snapshot="$old_current/deploy/eufy-wall@.service"
      (( ${#named_units[@]} == 0 )) || [[ -f $template_snapshot ]] || die 'prior release has no unit for running named instances'
      restore_template "$template_snapshot"
      rollback_release_unit "$old_current" "$base/.upgrade-unit" "$base/current" "/etc/systemd/system/$service.service" "$service" "$upgrade_active"
      restart_named_services || die 'previous named wall instance did not regain frame progress'
    else
      systemctl stop "$service" || true
      die 'new client failed; no prior binary was installed'
    fi
    rm -f "$base/.upgrade-pending" "$base/.upgrade-active"
    rm -f "$base/.upgrade-unit" "$base/.upgrade-template"
    if [[ -n $old_current && $upgrade_active == 1 ]] && ! /usr/local/bin/eufy-wall health; then
      die 'upgrade failed; previous binary restored but frame progress did not recover'
    fi
    die 'upgrade failed; inspect journalctl -u eufy-wall'
  fi
fi
if [[ -n $old_current ]]; then atomic_link "$old_current" "$base/previous"; fi
if [[ -f $base/.upgrade-unit ]]; then copy_unit_exact "$base/.upgrade-unit" "$base/previous-unit.service"; fi
if [[ -f $base/.upgrade-template ]]; then copy_unit_exact "$base/.upgrade-template" "$base/previous-template.service"
else rm -f "$base/previous-template.service"; fi
rm -f "$base/.upgrade-pending" "$base/.upgrade-active"
rm -f "$base/.upgrade-unit" "$base/.upgrade-template"
say "installed client $version for $arch; $( ((upgrade_active || old_active || ${#named_units[@]})) && echo upgraded-running-service || echo run-eufy-wall-setup-then-enable-service )"
