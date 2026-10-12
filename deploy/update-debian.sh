#!/usr/bin/env bash
# From this Mac checkout: sync the current bridge code, install dependencies, and restart the service.
# Config, credentials, and the Eufy session remain under /etc and /var/lib on the server.
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
HOST=${1:-root@192.168.23.158}
KEY=${EUFY_WALL_SSH_KEY:-$HOME/.ssh/eufy-wall-debian}
SSH=(ssh -o StrictHostKeyChecking=accept-new)
if [[ -f $KEY ]]; then SSH+=(-i "$KEY"); fi
REMOTE=/root/eufy-wall-source

[[ ${GO2RTC_VERSION:-1.9.14} == 1.9.14 ]] || { echo "verified TCP write repair supports go2rtc 1.9.14"; exit 1; }
# Build or verify locally before touching the remote installation. The small
# bridge then installs the verified cache without compiling Go.
TARGET_MACHINE=$("${SSH[@]}" "$HOST" uname -m)
case "$TARGET_MACHINE" in
  x86_64) GO2RTC_TARGET=amd64 ;; aarch64) GO2RTC_TARGET=arm64 ;;
  armv7l) GO2RTC_TARGET=arm ;; armv6l) GO2RTC_TARGET=armv6 ;;
  *) echo "unsupported target architecture $TARGET_MACHINE"; exit 1 ;;
esac
python3 "$REPO/deploy/build-go2rtc.py" --target "$GO2RTC_TARGET"

"${SSH[@]}" "$HOST" "mkdir -p '$REMOTE/server' '$REMOTE/deploy'"
RSYNC_RSH="ssh -o StrictHostKeyChecking=accept-new"
if [[ -f $KEY ]]; then RSYNC_RSH+=" -i $KEY"; fi
rsync -a --delete -e "$RSYNC_RSH" \
  --exclude node_modules --exclude data --exclude local.env --exclude config.yaml \
  --exclude run.log --exclude .DS_Store \
  "$REPO/server/" "$HOST:$REMOTE/server/"
rsync -a --delete -e "$RSYNC_RSH" "$REPO/deploy/" "$HOST:$REMOTE/deploy/"
"${SSH[@]}" "$HOST" "bash '$REMOTE/deploy/install-server.sh'"
if "${SSH[@]}" "$HOST" "systemctl is-active --quiet eufy-wall-bridge"; then
  "${SSH[@]}" "$HOST" 'for i in $(seq 1 45); do
    body=$(curl -fsS --max-time 3 http://127.0.0.1:3000/healthz 2>/dev/null) || { sleep 1; continue; }
    if [[ $body == *\"state\":\"ok\"* && $body == *\"go2rtc\":\"running\"* && $body != *\"cameras\":0* && $body != *\"streaming\":[]* ]]; then
      printf "%s\n" "$body"; exit 0
    fi
    sleep 1
  done
  exit 1'
else
  echo "Code installed; configure /etc/eufy-wall-bridge.{env,yaml} and start eufy-wall-bridge."
fi
