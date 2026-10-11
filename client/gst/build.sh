#!/usr/bin/env sh
# Compile for the destination's native architecture and installed GStreamer ABI.
set -eu
src=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
dest=${1:?destination plugin directory}
mkdir -p "$dest"
cc=${CC:-cc}
"$cc" -std=c99 -O2 -Wall -Wextra -Werror -o "$dest/pacing-test" "$src/pacing_test.c"
"$dest/pacing-test"
rm "$dest/pacing-test"
# pkg-config emits compiler options, intentionally word split.
"$cc" -std=c99 -O2 -Wall -Wextra -Werror -shared -fPIC \
  $(pkg-config --cflags gstreamer-base-1.0) \
  "$src/gsteufypacing.c" -o "$dest/libgsteufypacing.so.new" \
  $(pkg-config --libs gstreamer-base-1.0)
mv "$dest/libgsteufypacing.so.new" "$dest/libgsteufypacing.so"
GST_PLUGIN_PATH="$dest${GST_PLUGIN_PATH:+:$GST_PLUGIN_PATH}" gst-inspect-1.0 --exists eufypacing
