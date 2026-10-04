#!/bin/zsh
set -euo pipefail

iface=$(/sbin/route -n get default | /usr/bin/awk '/interface:/{print $2; exit}')
ip=$(/usr/sbin/ipconfig getifaddr "$iface")
case "$ip" in
  ''|127.*) echo "No LAN IPv4 address on $iface" >&2; exit 1 ;;
esac

exec /usr/bin/dns-sd -P EufyWallLocal _eufy-wall._tcp local 3000 eufy-local.local "$ip" rtsp=8565
