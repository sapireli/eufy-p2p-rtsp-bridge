#!/usr/bin/env bash
# Install eufy-wall-bridge on Debian/Ubuntu (amd64 or arm64) without Docker.
#   sudo deploy/install-server.sh            (run from the repo root)
# Installs Node 24 (NodeSource), a go2rtc release binary, the server under /opt/eufy-wall-bridge, a
# system user, config/env skeletons in /etc and a systemd unit. Idempotent: re-run to upgrade.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run as root (sudo)"; exit 1; }
REPO=$(cd "$(dirname "$0")/.." && pwd)
PREFIX=/opt/eufy-wall-bridge
GO2RTC_VERSION=${GO2RTC_VERSION:-1.9.14}

# 1. Node 24 + rsync (step 3) + ffmpeg (go2rtc's generated `ffmpeg:` sources spawn the ffmpeg binary)
apt-get update && apt-get install -y ca-certificates curl gnupg rsync ffmpeg
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
curl -fsSL -o "$PREFIX/bin/go2rtc" "https://github.com/AlexxIT/go2rtc/releases/download/v${GO2RTC_VERSION}/go2rtc_linux_${ARCH}"
chmod +x "$PREFIX/bin/go2rtc"
"$PREFIX/bin/go2rtc" --version || true

# 3. app files + deps
id -u eufy-wall >/dev/null 2>&1 || useradd --system --home /var/lib/eufy-wall-bridge --shell /usr/sbin/nologin eufy-wall
mkdir -p "$PREFIX/server" /var/lib/eufy-wall-bridge
rsync -a --delete --exclude node_modules --exclude data --exclude spike-out "$REPO/server/" "$PREFIX/server/"
(cd "$PREFIX/server" && npm ci --omit=dev --no-audit --no-fund)
chown -R eufy-wall:eufy-wall "$PREFIX" /var/lib/eufy-wall-bridge

# 4. config skeletons (never overwrite)
[[ -f /etc/eufy-wall-bridge.yaml ]] || { cp "$REPO/server/config.example.yaml" /etc/eufy-wall-bridge.yaml; echo "edit /etc/eufy-wall-bridge.yaml"; }
[[ -f /etc/eufy-wall-bridge.env ]] || { cp "$REPO/deploy/eufy-wall-bridge.env.example" /etc/eufy-wall-bridge.env; chmod 600 /etc/eufy-wall-bridge.env; echo "edit /etc/eufy-wall-bridge.env"; }

# 5. systemd
cp "$REPO/deploy/eufy-wall-bridge.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable eufy-wall-bridge
echo "installed. next: edit /etc/eufy-wall-bridge.{env,yaml}; systemctl start eufy-wall-bridge; journalctl -fu eufy-wall-bridge"
