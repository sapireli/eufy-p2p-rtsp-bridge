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
if systemctl is-active --quiet eufy-wall-bridge; then WAS_ACTIVE=1; fi

# Stop before replacing any executable/source or migrating settings. A failed upgrade
# stays stopped so an incompatible mixture cannot reopen camera sessions.
if systemctl cat eufy-wall-bridge >/dev/null 2>&1; then
  systemctl stop eufy-wall-bridge
fi
BACKUP_DIR="/var/backups/eufy-wall-bridge/$(date -u +%Y%m%dT%H%M%SZ)-$$"
trap 'echo "Upgrade failed; bridge remains stopped. Private rollback files: $BACKUP_DIR" >&2' ERR
install -d -m 700 "$BACKUP_DIR"
for item in /etc/eufy-wall-bridge.yaml /etc/eufy-wall-bridge.env /var/lib/eufy-wall-bridge/settings.json /var/lib/eufy-wall-bridge/settings-initialized; do
  if [[ -f "$item" ]]; then cp -a "$item" "$BACKUP_DIR/"; fi
done
if [[ -d "$PREFIX/server" ]]; then
  tar -czf "$BACKUP_DIR/server-source.tar.gz" --exclude=node_modules --exclude=data --exclude=spike-out -C "$PREFIX" server
fi
if [[ -f "$PREFIX/bin/go2rtc" ]]; then cp -a "$PREFIX/bin/go2rtc" "$BACKUP_DIR/go2rtc"; fi

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
# Preserve private legacy inputs until migration has read them (sources_file may
# name an untracked file relative to the old service working directory).
rsync -a --exclude node_modules --exclude data --exclude spike-out "$REPO/server/" "$PREFIX/server/"
(cd "$PREFIX/server" && npm ci --omit=dev --no-audit --no-fund)

# 4. config skeletons (never overwrite)
[[ -f /etc/eufy-wall-bridge.yaml ]] || { cp "$REPO/server/bootstrap.example.yaml" /etc/eufy-wall-bridge.yaml; echo "created bridge startup configuration; manage camera settings from a client"; }
[[ -f /etc/eufy-wall-bridge.env ]] || { cp "$REPO/deploy/eufy-wall-bridge.env.example" /etc/eufy-wall-bridge.env; chmod 600 /etc/eufy-wall-bridge.env; echo "edit /etc/eufy-wall-bridge.env"; }

# Explicit offline migration is the only supported legacy import path. The tool
# preserves an existing store, imports on first use, and retires YAML operating fields.
(cd "$PREFIX/server" && node ./migrate-legacy-config.mjs \
  --config /etc/eufy-wall-bridge.yaml \
  --data-dir /var/lib/eufy-wall-bridge \
  --env-file /etc/eufy-wall-bridge.env)
# Remove retired operating environment assignments only after successful migration.
# Account credentials, startup options, comments and permissions remain intact.
python3 - /etc/eufy-wall-bridge.env <<'PYENV'
import os, re, stat, sys, tempfile
path = sys.argv[1]
before = os.lstat(path)
if not stat.S_ISREG(before.st_mode):
    raise SystemExit("Service environment must be a regular file")
with open(path, "rb") as source:
    original = source.read()
lines = original.splitlines(keepends=True)
retired = {b"BRIDGE_GO2RTC_TRANSCODE", b"BRIDGE_GO2RTC_MAX_HEIGHT", b"BRIDGE_GO2RTC_VAAPI_DEVICE", b"BRIDGE_GO2RTC_VAAPI_DECODE"}
assignment = re.compile(rb"^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)")
kept = []
quote = None
dropping = False
for line in lines:
    if quote is None:
        match = assignment.match(line)
        dropping = bool(match and match.group(1) in retired)
        value = match.group(2) if match else b""
        if value[:1] in (b'"', b"'"):
            quote = value[0]
            remainder = value[1:]
        else:
            remainder = b""
    else:
        remainder = line
    if not dropping:
        kept.append(line)
    if quote is not None:
        escaped = False
        for character in remainder:
            if escaped:
                escaped = False
            elif character == 92 and quote == 34:
                escaped = True
            elif character == quote:
                quote = None
                break
updated = b"".join(kept)
if updated != original:
    fd, temporary = tempfile.mkstemp(prefix=".eufy-wall-env-", dir=os.path.dirname(path))
    try:
        with os.fdopen(fd, "wb") as output:
            output.write(updated)
            output.flush()
            os.fchmod(output.fileno(), stat.S_IMODE(before.st_mode))
            os.fchown(output.fileno(), before.st_uid, before.st_gid)
            os.fsync(output.fileno())
        current = os.lstat(path)
        if (current.st_ino, current.st_mtime_ns, current.st_size) != (before.st_ino, before.st_mtime_ns, before.st_size):
            raise RuntimeError("Service environment changed during upgrade")
        os.replace(temporary, path)
        directory = os.open(os.path.dirname(path), os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
PYENV
chown -R eufy-wall:eufy-wall "$PREFIX" /var/lib/eufy-wall-bridge

echo "Private pre-upgrade backup: $BACKUP_DIR"

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
  echo "updated, migrated and restarted eufy-wall-bridge"
else
  echo "installed. next: edit /etc/eufy-wall-bridge.env; systemctl start eufy-wall-bridge; journalctl -fu eufy-wall-bridge"
  echo "then pair a client using /var/lib/eufy-wall-bridge/settings-admin-token to configure cameras and operating settings"
fi

trap - ERR
