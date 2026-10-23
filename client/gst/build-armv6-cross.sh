#!/bin/sh
# Build off-device using an extracted target ARM hard-float development sysroot.
# No target installation or service changes. The normal native installer remains supported.
set -eu
: "${ARM_SYSROOT:?Set ARM_SYSROOT to the extracted target development sysroot}"
compiler=${ARM_CC:-arm-linux-gnueabihf-gcc}
output=${1:-libgsteufypacing-armv6.so}
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$compiler" -std=gnu11 -O2 -fPIC -shared -nostartfiles -shared-libgcc -Wall -Wextra \
  -marm -mcpu=arm1176jzf-s -mfpu=vfp -mfloat-abi=hard --sysroot="$ARM_SYSROOT" \
  -I"$ARM_SYSROOT/usr/include/arm-linux-gnueabihf" \
  -I"$ARM_SYSROOT/usr/include/gstreamer-1.0" \
  -I"$ARM_SYSROOT/usr/include/glib-2.0" \
  -I"$ARM_SYSROOT/usr/lib/arm-linux-gnueabihf/glib-2.0/include" \
  -L"$ARM_SYSROOT/usr/lib/arm-linux-gnueabihf" \
  -L"$ARM_SYSROOT/lib/arm-linux-gnueabihf" \
  -Wl,--allow-shlib-undefined -Wl,-soname,libgsteufypacing.so \
  -o "$output" "$source_dir/gsteufypacing.c" \
  -lgstapp-1.0 -lgstvideo-1.0 -lgstbase-1.0 -lgstreamer-1.0 -lgobject-2.0 -lglib-2.0 -pthread
