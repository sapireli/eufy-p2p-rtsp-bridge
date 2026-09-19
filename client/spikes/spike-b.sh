#!/usr/bin/env bash
# Throwaway experiments for the Pi display backend. Run ON the Pi as a user in the video+render groups,
# from a text console (no X/Wayland). Usage: spike-b.sh <rtsp-url-1> [<rtsp-url-2> ...]
set -u
URLS=("$@"); N=${#URLS[@]}
[[ $N -ge 1 ]] || { echo "usage: $0 rtsp://... [rtsp://...]"; exit 1; }
echo "== environment"; uname -r; cat /proc/device-tree/model 2>/dev/null; echo; vcgencmd get_mem gpu 2>/dev/null
echo "== decoder + sinks"; for e in v4l2h264dec kmssink compositor glvideomixer; do printf '%-14s %s\n' "$e" "$(gst-inspect-1.0 --exists $e && echo yes || echo NO)"; done
echo "== overlay planes (need ids for sink=planes)"; modetest -M vc4 -p 2>/dev/null | awk '/^Planes/,/^$/' | head -40

W=1920; H=1080; COLS=3; ROWS=2; CW=$((W/COLS)); CH=$((H/ROWS))
src() { echo "rtspsrc location=$1 latency=200 protocols=tcp ! rtph264depay ! h264parse ! v4l2h264dec"; }

echo; echo "== A) one kmssink per tile on separate planes (edit PLANES to match modetest)"; PLANES=(${PLANES:-})
if [[ ${#PLANES[@]} -ge $N ]]; then
  P=""; for i in $(seq 0 $((N-1))); do x=$(( (i%COLS)*CW )); y=$(( (i/COLS)*CH )); P+="$(src "${URLS[$i]}") ! kmssink plane-id=${PLANES[$i]} render-rectangle=<$x,$y,$CW,$CH> force-aspect-ratio=true sync=false "; done
  echo "gst-launch-1.0 -e $P"; timeout 60 gst-launch-1.0 -e $P & sleep 45; top -bn1 | head -12; wait
else echo "skip: export PLANES='31 32 33 ...' first"; fi

echo; echo "== B) compositor → one kmssink"
P=""; MIX="compositor name=mix background=black "
for i in $(seq 0 $((N-1))); do x=$(( (i%COLS)*CW )); y=$(( (i/COLS)*CH )); P+="$(src "${URLS[$i]}") ! videoconvert ! mix.sink_$i "; MIX+="sink_$i::xpos=$x sink_$i::ypos=$y sink_$i::width=$CW sink_$i::height=$CH sink_$i::sizing-policy=keep-aspect-ratio "; done
echo "gst-launch-1.0 -e $P $MIX ! video/x-raw,width=$W,height=$H ! kmssink sync=false"
timeout 60 gst-launch-1.0 -e $P $MIX ! video/x-raw,width=$W,height=$H ! kmssink sync=false & sleep 45; top -bn1 | head -12; wait

echo; echo "== C) glvideomixer → kmssink (GPU composite)"
P=""; MIX="glvideomixer name=mix "
for i in $(seq 0 $((N-1))); do x=$(( (i%COLS)*CW )); y=$(( (i/COLS)*CH )); P+="$(src "${URLS[$i]}") ! glupload ! mix.sink_$i "; MIX+="sink_$i::xpos=$x sink_$i::ypos=$y sink_$i::width=$CW sink_$i::height=$CH "; done
echo "gst-launch-1.0 -e $P $MIX ! gldownload ! kmssink sync=false"
timeout 60 gst-launch-1.0 -e $P $MIX ! gldownload ! kmssink sync=false & sleep 45; top -bn1 | head -12; wait
echo "== done. Record: which variants rendered, CPU%, dropped frames (GST_DEBUG=2 warnings), kernel."
