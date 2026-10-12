#!/usr/bin/env bash
# Select stock by default; the trial write fix requires explicit opt-in.
set -euo pipefail
TARGET=${1:?binary path required}
ARCH=${2:?architecture required}
REPO=$(cd "$(dirname "$0")/.." && pwd)
VERSION=${GO2RTC_VERSION:-1.9.14}
WRITE_FIX=${GO2RTC_WRITE_FIX:-0}
case "$WRITE_FIX" in 0|1) ;; *) echo "GO2RTC_WRITE_FIX must be 0 or 1" >&2; exit 1 ;; esac
mkdir -p "$(dirname "$TARGET")"
if [[ $WRITE_FIX == 1 ]]; then
  [[ $VERSION == 1.9.14 ]] || { echo "trial write fix supports only go2rtc 1.9.14" >&2; exit 1; }
  python3 "$REPO/deploy/build-go2rtc.py" --target "$ARCH" --install "$TARGET"
else
  TEMP_BINARY=$(mktemp "${TARGET}.tmp.XXXXXX")
  trap 'rm -f "$TEMP_BINARY"' EXIT
  curl -fsSL -o "$TEMP_BINARY" "https://github.com/AlexxIT/go2rtc/releases/download/v${VERSION}/go2rtc_linux_${ARCH}"
  chmod +x "$TEMP_BINARY"
  mv -f "$TEMP_BINARY" "$TARGET"
fi
