#!/usr/bin/env bash
# Install eufy-wall on a Raspberry Pi (OS Lite, Bookworm/Trixie) or Debian x86 box. Run as root after
# building the binary (make pi1|pi3|pi64|amd64) or with a downloaded release binary. Expects the repo
# layout around it (deploy/ next to client/ — `scp -r deploy client pi:/tmp/eufy-wall/`):
#   sudo /tmp/eufy-wall/deploy/install-client.sh /tmp/eufy-wall/client/bin/eufy-wall-armv7
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run as root"; exit 1; }
BIN=${1:?path to eufy-wall binary}
REPO=$(cd "$(dirname "$0")/.." && pwd)

apt-get update
apt-get install -y gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad libdrm-tests
# x86 VAAPI / software fallback (harmless on the Pi if unavailable)
apt-get install -y gstreamer1.0-vaapi gstreamer1.0-libav 2>/dev/null || true

install -m 755 "$BIN" /usr/local/bin/eufy-wall
id -u wall >/dev/null 2>&1 || useradd --system --shell /usr/sbin/nologin --groups video,render wall
if [[ ! -f /etc/eufy-wall.yaml ]]; then
  if [[ -f "$REPO/client/config.example.yaml" ]]; then
    cp "$REPO/client/config.example.yaml" /etc/eufy-wall.yaml; echo "edit /etc/eufy-wall.yaml"
  else
    echo "warning: $REPO/client/config.example.yaml not found — write /etc/eufy-wall.yaml by hand (see docs/runbook-client.md)" >&2
  fi
fi
cp "$REPO/deploy/eufy-wall.service" /etc/systemd/system/
systemctl daemon-reload && systemctl enable eufy-wall

if [[ -f /boot/firmware/config.txt ]]; then
  CFG=/boot/firmware/config.txt
  grep -q '^dtoverlay=vc4-kms-v3d' "$CFG" || echo 'dtoverlay=vc4-kms-v3d' >> "$CFG"
  grep -q '^gpu_mem=' "$CFG" || echo 'gpu_mem=128' >> "$CFG"
  grep -q '^hdmi_blanking=' "$CFG" || echo 'hdmi_blanking=0' >> "$CFG"
  echo "Pi config.txt updated (vc4-kms-v3d, gpu_mem=128, hdmi_blanking=0) — reboot required"
fi
echo "installed. next: edit /etc/eufy-wall.yaml (rtsp_base, tiles, planes); eufy-wall -config /etc/eufy-wall.yaml -dry-run; systemctl start eufy-wall; journalctl -fu eufy-wall"
