#!/usr/bin/env bash
# Diff our vendored ha-eufy-sdk-bridge modules against upstream HEAD so updates are a reviewed merge.
# Usage: server/scripts/sync-upstream.sh [--apply]
#
# Note: macOS ships bash 3.2 (no associative arrays), so this uses parallel
# indexed arrays instead of `declare -A` to stay portable with /usr/bin/env bash.
set -euo pipefail
MODE=${1:-}
HERE=$(cd "$(dirname "$0")/.." && pwd)
VEND="$HERE/src/vendor/ha-bridge"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
git clone -q --depth 1 https://github.com/mega-yfue/ha-eufy-sdk-bridge "$TMP/up"
OURS=(streams.mjs go2rtc-config.mjs auth.mjs watchdog.mjs)
THEIRS=(streams.mjs go2rtc-config.mjs src/auth.mjs src/watchdog.mjs)
changed=0
for i in "${!OURS[@]}"; do
  ours="${OURS[$i]}"
  theirs="$TMP/up/${THEIRS[$i]}"
  if ! diff -u "$VEND/$ours" "$theirs" >"$TMP/$ours.diff"; then
    changed=1
    echo "== $ours differs from upstream ${THEIRS[$i]} =="
    cat "$TMP/$ours.diff"
    [[ "$MODE" == "--apply" ]] && cp "$theirs" "$VEND/$ours" && echo "applied $ours"
  fi
done
echo "upstream HEAD: $(git -C "$TMP/up" rev-parse HEAD)  (recorded: $(grep -o 'Commit: .*' "$VEND/VENDOR.md"))"
if [[ $changed -eq 0 ]]; then echo "vendored files are up to date"; exit 0; fi
[[ "$MODE" == "--check" ]] && exit 1
exit 0
