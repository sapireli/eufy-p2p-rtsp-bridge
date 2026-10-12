#!/usr/bin/env bash
# Install the verified go2rtc TCP framing repair for the pinned upstream version.
set -euo pipefail
TARGET=${1:?binary path required}
ARCH=${2:?architecture required}
REPO=$(cd "$(dirname "$0")/.." && pwd)
VERSION=${GO2RTC_VERSION:-1.9.14}
[[ $VERSION == 1.9.14 ]] || { echo "verified TCP write repair supports go2rtc 1.9.14; audit newer versions before updating" >&2; exit 1; }
mkdir -p "$(dirname "$TARGET")"
python3 "$REPO/deploy/build-go2rtc.py" --target "$ARCH" --install "$TARGET"
