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

if ((rollback)); then
  [[ $EUID -eq 0 ]] || die 'run rollback as root'
  need systemctl
  if [[ -L $base/previous ]]; then
    target=$(readlink "$base/previous")
    [[ -d $target ]] || die "previous release is missing: $target"
    atomic_link "$target" "$base/current"
    systemctl restart "$service"
    wait_active "$service" || die 'previous release did not become active; inspect journalctl'
    say "rolled back to $(cat "$target/VERSION")"
  elif [[ -f $base/legacy.service ]]; then
    install -m 644 "$base/legacy.service" "/etc/systemd/system/$service.service"
    systemctl daemon-reload
    systemctl restart "$service"
    wait_active "$service" || die 'legacy service did not become active; inspect journalctl'
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
if [[ ! -e /etc/eufy-wall-bridge.yaml ]]; then
  install -m 640 -o root -g eufy-wall "$release/server/config.example.yaml" /etc/eufy-wall-bridge.yaml
  say 'created /etc/eufy-wall-bridge.yaml; run eufy-bridge setup or edit it before starting'
fi
if [[ ! -e /etc/eufy-wall-bridge.env ]]; then
  install -m 600 -o root -g root "$release/deploy/eufy-wall-bridge.env.example" /etc/eufy-wall-bridge.env
  say 'created /etc/eufy-wall-bridge.env (0600); set credentials before starting'
fi

current_release= old_current= old_active=0
[[ -L $base/current ]] && current_release=$(readlink "$base/current")
if [[ -f $base/.upgrade-pending ]]; then
  old_current=$(cat "$base/.upgrade-pending")
  [[ $old_current == legacy || $old_current == none || -d $old_current ]] || die 'pending upgrade names a missing previous release'
  [[ $old_current == legacy || $old_current == none ]] && old_current=
else
  old_current=$current_release
fi
systemctl is-active --quiet "$service" && old_active=1 || true
if ! needs_activation "$current_release" "$release" "$base/.upgrade-pending" && cmp -s "$release/deploy/eufy-wall-bridge.service" "/etc/systemd/system/$service.service" && [[ -x /usr/local/bin/eufy-bridge ]]; then
  say "$version already installed; leaving service running"; exit 0
fi
if [[ -f /etc/systemd/system/$service.service && -z $current_release && ! -e $base/legacy.service ]]; then
  cp -p "/etc/systemd/system/$service.service" "$base/legacy.service"
fi
if [[ ! -f $base/.upgrade-pending ]]; then
  if [[ -n $old_current ]]; then printf '%s\n' "$old_current" > "$base/.upgrade-pending"
  elif [[ -f $base/legacy.service ]]; then printf 'legacy\n' > "$base/.upgrade-pending"
  else printf 'none\n' > "$base/.upgrade-pending"; fi
fi
install -m 644 "$release/deploy/eufy-wall-bridge.service" "/etc/systemd/system/$service.service"
install -d -m 755 /usr/local/bin
cat > /usr/local/bin/eufy-bridge <<'EOF'
#!/bin/sh
exec /opt/eufy-wall-bridge/current/bin/node /opt/eufy-wall-bridge/current/server/cli.mjs "$@"
EOF
chmod 755 /usr/local/bin/eufy-bridge
atomic_link "$release" "$base/current"
systemctl daemon-reload
systemctl enable "$service"

if ((old_active)) || [[ $(cat "$base/.upgrade-pending") != none ]]; then
  if ! systemctl restart "$service" || ! wait_active "$service"; then
    say 'new bridge failed to start; restoring previous version'
    if [[ -n $old_current ]]; then
      atomic_link "$old_current" "$base/current"
    elif [[ -f $base/legacy.service ]]; then
      install -m 644 "$base/legacy.service" "/etc/systemd/system/$service.service"
      systemctl daemon-reload
    fi
    systemctl restart "$service" || true
    rm -f "$base/.upgrade-pending"
    die 'upgrade failed; previous service restored; inspect journalctl -u eufy-wall-bridge'
  fi
fi
if [[ -n $old_current ]]; then atomic_link "$old_current" "$base/previous"; fi
rm -f "$base/.upgrade-pending"
say "installed bridge $version for $arch; $( ((old_active)) && echo upgraded-running-service || echo run-eufy-bridge-setup-then-start-service )"
