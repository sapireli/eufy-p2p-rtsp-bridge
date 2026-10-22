#!/usr/bin/env bash
# Install eufy-wall-bridge on Debian/Ubuntu (amd64 or arm64) without Docker.
#   sudo deploy/install-server.sh            (run from the repo root)
# Installs Node 24 (NodeSource), verified go2rtc with TCP framing repair, the server under /opt/eufy-wall-bridge, a
# system user, config/env skeletons in /etc and a systemd unit. Idempotent: re-run to upgrade.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run as root (sudo)"; exit 1; }
REPO=$(cd "$(dirname "$0")/.." && pwd)
PREFIX=/opt/eufy-wall-bridge
GO2RTC_VERSION=${GO2RTC_VERSION:-1.9.14}
WAS_ACTIVE=0
systemctl is-active --quiet eufy-wall-bridge && WAS_ACTIVE=1

# 1. Node 24 + rsync (step 3) + ffmpeg (go2rtc's generated `ffmpeg:` sources spawn the ffmpeg binary)
apt-get update && apt-get install -y ca-certificates curl gnupg rsync ffmpeg avahi-daemon python3 patch
if ! command -v node >/dev/null || [[ $(node -v | sed 's/v\([0-9]*\).*/\1/') -lt 24 ]]; then
  curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
  apt-get install -y nodejs
fi
node -v

# 2. go2rtc binary
case "$(uname -m)" in
  x86_64) ARCH=amd64 ;; aarch64) ARCH=arm64 ;; armv7l) ARCH=arm ;; armv6l) ARCH=armv6 ;;
  *) echo "unsupported arch $(uname -m)"; exit 1 ;;
esac
mkdir -p "$PREFIX/bin"
bash "$REPO/deploy/install-go2rtc.sh" "$PREFIX/bin/go2rtc" "$ARCH"
"$PREFIX/bin/go2rtc" --version || true

# 3. app files + deps
id -u eufy-wall >/dev/null 2>&1 || useradd --system --home /var/lib/eufy-wall-bridge --shell /usr/sbin/nologin eufy-wall
mkdir -p "$PREFIX/server" /var/lib/eufy-wall-bridge
rsync -a --delete --exclude node_modules --exclude data --exclude spike-out "$REPO/server/" "$PREFIX/server/"
(cd "$PREFIX/server" && npm ci --omit=dev --no-audit --no-fund)
chown -R eufy-wall:eufy-wall "$PREFIX" /var/lib/eufy-wall-bridge

# 4. config skeletons (never overwrite)
[[ -f /etc/eufy-wall-bridge.yaml ]] || { cp "$REPO/server/bootstrap.example.yaml" /etc/eufy-wall-bridge.yaml; echo "created bridge startup configuration; manage camera settings from a client"; }
[[ -f /etc/eufy-wall-bridge.env ]] || { cp "$REPO/deploy/eufy-wall-bridge.env.example" /etc/eufy-wall-bridge.env; chmod 600 /etc/eufy-wall-bridge.env; echo "edit /etc/eufy-wall-bridge.env"; }

# 5. systemd
cp "$REPO/deploy/eufy-wall-bridge.service" /etc/systemd/system/
# Avahi publishes the bridge's fixed HTTP and RTSP ports on the local network.
install -d -m 755 /etc/avahi/services
install -m 644 "$REPO/deploy/eufy-wall-avahi.service" /etc/avahi/services/eufy-wall.service
systemctl daemon-reload
systemctl enable --now avahi-daemon
systemctl enable eufy-wall-bridge
# DietPi's service menu uses its own include list; a systemd unit alone does not appear there.
DIETPI_SERVICES=/boot/dietpi/.dietpi-services_include_exclude
if [[ -f $DIETPI_SERVICES ]] && ! grep -Fqx '+ eufy-wall-bridge' "$DIETPI_SERVICES"; then
  printf '\n+ eufy-wall-bridge\n' >> "$DIETPI_SERVICES"
fi
if [[ $WAS_ACTIVE -eq 1 ]]; then
  systemctl restart eufy-wall-bridge
  echo "updated and restarted eufy-wall-bridge"
else
  echo "installed. next: edit /etc/eufy-wall-bridge.env; systemctl start eufy-wall-bridge; journalctl -fu eufy-wall-bridge"
  echo "then pair a client using /var/lib/eufy-wall-bridge/settings-admin-token to configure cameras and operating settings"
fi
