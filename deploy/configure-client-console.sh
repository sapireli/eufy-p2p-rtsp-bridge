#!/usr/bin/env bash
# Opt-in kiosk console setup. Run on a dedicated Linux HDMI display after installing the client.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run as root" >&2; exit 1; }
[[ -c /dev/tty1 ]] || { echo "HDMI virtual console /dev/tty1 is missing" >&2; exit 1; }
command -v setterm >/dev/null
REPO=$(cd "$(dirname "$0")/.." && pwd)
install -d -m 755 /usr/local/libexec /etc/sysctl.d /etc/systemd/system/eufy-wall.service.d
install -m 755 "$REPO/deploy/eufy-wall-clear-console.sh" /usr/local/libexec/eufy-wall-clear-console
# Preserve this kernel's default/minimum/boot console levels; silence only current console printing.
read -r CONSOLE_CURRENT CONSOLE_DEFAULT CONSOLE_MINIMUM CONSOLE_BOOT_DEFAULT </proc/sys/kernel/printk
printf '# Dedicated camera display; kernel messages remain in the journal.\nkernel.printk = 0 %s %s %s\n' \
  "$CONSOLE_DEFAULT" "$CONSOLE_MINIMUM" "$CONSOLE_BOOT_DEFAULT" >/etc/sysctl.d/99-eufy-wall-console.conf
cat >/etc/systemd/system/eufy-wall.service.d/console.conf <<'EOF'
[Service]
ExecStartPre=+/usr/local/libexec/eufy-wall-clear-console
EOF
systemctl mask --now getty@tty1.service
sysctl -p /etc/sysctl.d/99-eufy-wall-console.conf
systemctl daemon-reload
/usr/local/libexec/eufy-wall-clear-console
echo "HDMI console cleared; kernel console output disabled now and at boot. Video service left running."
