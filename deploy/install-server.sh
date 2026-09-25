#!/usr/bin/env bash
# Install a verified, versioned bridge release on Debian/Ubuntu amd64 or arm64.
# See docs/runbook-server.md for download, attestation and offline instructions.
set -euo pipefail
source "$(dirname "$0")/install-common.sh"
parse_install_args "$@"
arch=$(linux_arch)
[[ $arch == amd64 || $arch == arm64 ]] || die 'bridge releases support amd64 and arm64'
base=/opt/eufy-wall-bridge
service=eufy-wall-bridge

bridge_health() {
  /usr/local/bin/eufy-bridge status --json 2>/dev/null | "$base/current/bin/node" -e '
    const chunks = [];
    process.stdin.on("data", (chunk) => chunks.push(chunk));
    process.stdin.on("end", () => {
      try { process.exit(JSON.parse(Buffer.concat(chunks).toString()).live?.ok === true ? 0 : 1); }
      catch { process.exit(1); }
    });
  ' >/dev/null
}

wait_bridge_health() {
  local attempt
  for ((attempt=0; attempt<5; attempt++)); do
    bridge_health && return 0
    sleep 1
  done
  return 1
}

if ((rollback)); then
  [[ $EUID -eq 0 ]] || die 'run rollback as root'
  need systemctl
  prior_active=0
  systemctl is-active --quiet "$service" && prior_active=1 || true
  if [[ -f $base/.upgrade-pending ]]; then
    pending_target=$(cat "$base/.upgrade-pending")
    prior_active=$(cat "$base/.upgrade-active" 2>/dev/null || echo 1)
    [[ $pending_target == legacy || -d $pending_target ]] || die "pending upgrade has no prior service: $pending_target"
    if [[ $pending_target == legacy ]]; then pending_target=; fi
    rollback_release_unit "$pending_target" "$base/.upgrade-unit" "$base/current" "/etc/systemd/system/$service.service" "$service" "$prior_active"
    if [[ $prior_active == 1 ]]; then wait_bridge_health || die 'prior bridge process is active, but its HTTP health endpoint is unavailable'; fi
    if [[ -n $pending_target ]]; then atomic_link "$pending_target" "$base/previous"; fi
    rm -f "$base/.upgrade-pending" "$base/.upgrade-active" "$base/.upgrade-unit"
    say 'interrupted bridge upgrade rolled back to its prior running service'
    exit 0
  fi
  if [[ -L $base/previous ]]; then
    target=$(readlink "$base/previous")
    [[ -d $target ]] || die "previous release is missing: $target"
    unit_snapshot="$base/previous-unit.service"
    if [[ ! -f $unit_snapshot ]]; then
      say 'prior installed unit snapshot is missing; using packaged unit defaults'
      unit_snapshot="$target/deploy/eufy-wall-bridge.service"
    fi
    rollback_release_unit "$target" "$unit_snapshot" "$base/current" "/etc/systemd/system/$service.service" "$service" "$prior_active"
    if [[ $prior_active == 1 ]]; then wait_bridge_health || die 'prior bridge process is active, but its HTTP health endpoint is unavailable'; fi
    say "rolled back to $(cat "$target/VERSION")"
  elif [[ -f $base/legacy.service ]]; then
    rollback_release_unit '' "$base/legacy.service" "$base/current" "/etc/systemd/system/$service.service" "$service" "$prior_active"
    if [[ $prior_active == 1 ]]; then wait_bridge_health || die 'legacy bridge process is active, but its HTTP health endpoint is unavailable'; fi
    say 'rolled back to pre-release service'
  else
    die 'no previous release is available'
  fi
  exit 0
fi

verify_release server "$arch"
[[ -x $payload/bin/node && -x $payload/bin/go2rtc && -f $payload/server/server.mjs && -f $payload/server/cli.mjs ]] || die 'server archive is incomplete'
if ((verify_only)); then say "release $version is valid for this host"; exit 0; fi
[[ $EUID -eq 0 ]] || die 'run install as root (or use --verify-only)'
need systemctl; need useradd; need install; need mv; need readlink
[[ -f /etc/os-release ]] || die 'missing /etc/os-release'
source /etc/os-release
[[ ${ID:-} == debian || ${ID:-} == ubuntu || " ${ID_LIKE:-} " == *' debian '* ]] || die 'Debian or Ubuntu is required'
ensure_debian_packages ca-certificates ffmpeg

release="$base/releases/$version-$arch"
install -d -m 755 "$base/releases" /var/lib/eufy-wall-bridge
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

id -u eufy-wall >/dev/null 2>&1 || useradd --system --home /var/lib/eufy-wall-bridge --shell /usr/sbin/nologin eufy-wall
chown eufy-wall:eufy-wall /var/lib/eufy-wall-bridge
if [[ ! -e /etc/eufy-wall-bridge.example.yaml ]]; then
  install -m 644 -o root -g root "$release/server/config.example.yaml" /etc/eufy-wall-bridge.example.yaml
  say 'installed /etc/eufy-wall-bridge.example.yaml; run eufy-bridge setup or apply YAML before starting'
fi
if [[ ! -e /etc/eufy-wall-bridge.env ]]; then
  install -m 600 -o root -g root "$release/deploy/eufy-wall-bridge.env.example" /etc/eufy-wall-bridge.env
  say 'created /etc/eufy-wall-bridge.env (0600); set credentials before starting'
fi

current_release= old_current= old_active=0
[[ -L $base/current ]] && current_release=$(readlink "$base/current")
systemctl is-active --quiet "$service" && old_active=1 || true
if [[ -f $base/.upgrade-pending ]]; then
  old_current=$(cat "$base/.upgrade-pending")
  upgrade_active=$(cat "$base/.upgrade-active" 2>/dev/null || echo 1)
  [[ $old_current == legacy || $old_current == none || -d $old_current ]] || die 'pending upgrade names a missing previous release'
  [[ $old_current == legacy || $old_current == none ]] && old_current=
else
  old_current=$current_release
  upgrade_active=$old_active
fi
if ! needs_activation "$current_release" "$release" "$base/.upgrade-pending" && cmp -s "$release/deploy/eufy-wall-bridge.service" "/etc/systemd/system/$service.service" && [[ -x /usr/local/bin/eufy-bridge ]] && grep -Fqx 'export BRIDGE_CONFIG=${BRIDGE_CONFIG:-/etc/eufy-wall-bridge.yaml}' /usr/local/bin/eufy-bridge; then
  say "$version already installed; leaving service running"; exit 0
fi
if [[ -f /etc/systemd/system/$service.service && -z $current_release && ! -e $base/legacy.service ]]; then
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
  elif [[ -f $base/legacy.service ]]; then write_marker "$base/.upgrade-pending" legacy
  else write_marker "$base/.upgrade-pending" none; fi
fi
install_unit "$release/deploy/eufy-wall-bridge.service" "/etc/systemd/system/$service.service"
install -d -m 755 /usr/local/bin
cat > /usr/local/bin/eufy-bridge <<'EOF'
#!/bin/sh
export BRIDGE_CONFIG=${BRIDGE_CONFIG:-/etc/eufy-wall-bridge.yaml}
exec /opt/eufy-wall-bridge/current/bin/node /opt/eufy-wall-bridge/current/server/cli.mjs "$@"
EOF
chmod 755 /usr/local/bin/eufy-bridge
atomic_link "$release" "$base/current"
systemctl daemon-reload

if ((upgrade_active || old_active)); then
  if ! systemctl restart "$service" || ! wait_active "$service" || ! wait_bridge_health; then
    say 'new bridge failed to start; restoring previous version'
    if [[ -n $old_current || -f $base/legacy.service ]]; then
      rollback_release_unit "$old_current" "$base/.upgrade-unit" "$base/current" "/etc/systemd/system/$service.service" "$service"
      wait_bridge_health || die 'previous bridge process was restored, but its HTTP health endpoint is unavailable'
    else
      systemctl stop "$service" || true
      die 'new bridge failed; no prior service was installed'
    fi
    rm -f "$base/.upgrade-pending" "$base/.upgrade-active"
    rm -f "$base/.upgrade-unit"
    die 'upgrade failed; previous service restored; inspect journalctl -u eufy-wall-bridge'
  fi
fi
if [[ -n $old_current ]]; then atomic_link "$old_current" "$base/previous"; fi
if [[ -f $base/.upgrade-unit ]]; then copy_unit_exact "$base/.upgrade-unit" "$base/previous-unit.service"; fi
rm -f "$base/.upgrade-pending" "$base/.upgrade-active"
rm -f "$base/.upgrade-unit"
say "installed bridge $version for $arch; $( ((upgrade_active || old_active)) && echo upgraded-running-service || echo run-eufy-bridge-setup-then-enable-service )"
