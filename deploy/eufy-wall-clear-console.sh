#!/bin/sh
# Dedicated HDMI appliance: keep kernel diagnostics in the journal, off the video backdrop.
set -eu
printf '0\n' >/proc/sys/kernel/printk
TERM=linux setterm --background black --foreground white --clear all --cursor off --blank 0 --powerdown 0 </dev/tty1 >/dev/tty1
