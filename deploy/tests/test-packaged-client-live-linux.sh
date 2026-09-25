#!/usr/bin/env bash
# Real packaged binary, manual YAML, and decoded RTSP frames on an isolated systemd CI runner.
# Xvfb exercises the window sink; physical DRM connectors and visible pixels remain untested.
set -euo pipefail
[[ ${CI:-} == true && $(uname -s) == Linux && $(uname -m) == x86_64 ]] || { echo 'requires an isolated amd64 Linux CI runner' >&2; exit 1; }
. /etc/os-release
[[ ${ID:-} == ubuntu ]] || { echo 'requires an Ubuntu CI runner' >&2; exit 1; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
scratch=$(mktemp -d)
chmod 755 "$scratch"
service=eufy-wall.service
status=/run/eufy-wall/status.json
cleanup() {
  if [[ -n ${publisher_pid:-} ]]; then kill "$publisher_pid" 2>/dev/null || true; fi
  if [[ -n ${bridge_pid:-} ]]; then kill "$bridge_pid" 2>/dev/null || true; fi
  if [[ -n ${rtsp_pid:-} ]]; then kill "$rtsp_pid" 2>/dev/null || true; fi
  if [[ -n ${xvfb_pid:-} ]]; then kill "$xvfb_pid" 2>/dev/null || true; fi
  sudo journalctl -u "$service" -n 45 --no-pager > "$scratch/service.log" 2>/dev/null || true
  sudo systemctl disable --now "$service" >/dev/null 2>&1 || true
  sudo rm -rf /etc/systemd/system/eufy-wall.service.d /opt/eufy-wall
  sudo rm -f /etc/systemd/system/eufy-wall.service /etc/systemd/system/eufy-wall@.service \
    /usr/local/bin/eufy-wall /etc/eufy-wall.yaml /etc/eufy-wall.example.yaml
  sudo systemctl daemon-reload || true
  if [[ ${trial_ok:-0} != 1 ]]; then
    echo 'live client trial logs:' >&2
    tail -n 45 "$scratch"/*.log >&2 || true
  fi
  rm -rf "$scratch"
}
for path in /opt/eufy-wall /etc/systemd/system/eufy-wall.service \
  /etc/systemd/system/eufy-wall@.service /etc/systemd/system/eufy-wall.service.d \
  /usr/local/bin/eufy-wall /etc/eufy-wall.yaml /etc/eufy-wall.example.yaml; do
  [[ ! -e $path && ! -L $path ]] || { echo "CI runner is not clean: $path" >&2; exit 1; }
done
trap cleanup EXIT

# SHA-256 is pinned from MediaMTX v1.21.1's published checksums.sha256.
archive=mediamtx_v1.21.1_linux_amd64.tar.gz
curl -fsSL --retry 2 --max-time 90 -o "$scratch/$archive" \
  "https://github.com/bluenviron/mediamtx/releases/download/v1.21.1/$archive"
printf '%s  %s\n' '653abc672a3e693f8d3b2717752492fdcfb8072291ec108d03d3dd857411b0ee' "$scratch/$archive" | sha256sum -c -
tar -xzf "$scratch/$archive" -C "$scratch" mediamtx
cat > "$scratch/mediamtx.yml" <<'EOF'
logLevel: warn
rtspAddress: 127.0.0.1:38554
rtspTransports: [tcp]
rtmp: false
hls: false
webrtc: false
srt: false
moq: false
paths:
  test:
    source: publisher
EOF
"$scratch/mediamtx" "$scratch/mediamtx.yml" > "$scratch/rtsp.log" 2>&1 & rtsp_pid=$!
python3 "$repo/deploy/tests/synthetic_bridge.py" 39001 > "$scratch/bridge.log" 2>&1 & bridge_pid=$!
Xvfb :99 -screen 0 640x360x24 -ac -nolisten tcp > "$scratch/xvfb.log" 2>&1 & xvfb_pid=$!
for _ in {1..50}; do
  if curl -fsS --max-time 1 http://127.0.0.1:39001/healthz >/dev/null 2>&1 &&
     (echo > /dev/tcp/127.0.0.1/38554) 2>/dev/null && [[ -S /tmp/.X11-unix/X99 ]]; then break; fi
  kill -0 "$rtsp_pid" "$bridge_pid" "$xvfb_pid"
  sleep 0.2
done
[[ -S /tmp/.X11-unix/X99 ]] && (echo > /dev/tcp/127.0.0.1/38554) 2>/dev/null
ffmpeg -hide_banner -loglevel error -re -f lavfi -i testsrc2=size=320x180:rate=15 \
  -an -c:v libx264 -preset ultrafast -tune zerolatency -pix_fmt yuv420p \
  -g 15 -f rtsp -rtsp_transport tcp rtsp://127.0.0.1:38554/test \
  > "$scratch/publisher.log" 2>&1 & publisher_pid=$!
sleep 1
kill -0 "$publisher_pid"

bash "$repo/deploy/package-release.sh" client v0.0.0-live amd64 "$scratch" > "$scratch/package.log"
package="$scratch/eufy-wall-v0.0.0-live-linux-amd64.tar.gz"
(cd "$scratch" && sha256sum "${package##*/}") > "$scratch/SHA256SUMS"
digest=$(sha256sum "$package"); digest=${digest%% *}
sudo bash "$repo/deploy/install-client.sh" --artifact "$package" \
  --checksums "$scratch/SHA256SUMS" --trusted-sha256 "$digest" --no-apt
[[ ! -e /etc/eufy-wall.yaml ]]
if sudo systemctl is-active --quiet "$service"; then
  echo 'fresh install unexpectedly started service' >&2; exit 1
fi
sudo mkdir -p /etc/systemd/system/eufy-wall.service.d
printf '[Service]\nEnvironment=DISPLAY=:99\n' | sudo tee /etc/systemd/system/eufy-wall.service.d/ci-display.conf >/dev/null
sudo systemctl daemon-reload

cat > "$scratch/answers.yaml" <<'EOF'
bridge_url: http://127.0.0.1:39001
rtsp_base: rtsp://127.0.0.1:38554
cameras: [SYNTHETIC1]
template: one
decoder: software
sink: window
probe_streams: true
EOF
eufy-wall setup --answers "$scratch/answers.yaml" --output "$scratch/generated.yaml"
eufy-wall config validate "$scratch/generated.yaml"
eufy-wall probe "$scratch/generated.yaml" SYNTHETIC1

cat > "$scratch/manual.yaml" <<'EOF'
schema_version: 2
bridge_url: http://127.0.0.1:39001
rtsp_base: rtsp://127.0.0.1:38554
screen: {width: 320, height: 180}
layout: custom
canvas: {cols: 32, rows: 32}
decoder: software
sink: window
tiles:
  - id: synthetic
    url: rtsp://127.0.0.1:38554/test
    codec: h264
    rect: {x: 0, y: 0, w: 32, h: 32}
EOF
eufy-wall config validate "$scratch/manual.yaml"
eufy-wall layout preview "$scratch/manual.yaml" --png "$scratch/layout.png"
[[ -s $scratch/layout.png ]]
sudo eufy-wall config apply "$scratch/manual.yaml"
sudo systemctl is-active --quiet "$service"
sudo cmp "$scratch/manual.yaml" /etc/eufy-wall.yaml

frames() {
  sudo python3 -c 'import json,sys; s=json.load(open(sys.argv[1])); t=s["tiles"]["synthetic"]; assert s["sink"]=="window" and t["expected_live"] and t["source_kind"]=="live"; print(s["output_frames"], t["decoded_frames"])' "$status"
}
read -r output_before decoded_before <<< "$(frames)"
sleep 2
read -r output_after decoded_after <<< "$(frames)"
(( output_after > output_before && decoded_after > decoded_before ))

sed 's@38554/test$@38554/missing@' "$scratch/manual.yaml" > "$scratch/broken.yaml"
if sudo eufy-wall config apply "$scratch/broken.yaml" > "$scratch/broken-apply.log" 2>&1; then
  echo 'accepted a fixed RTSP tile without frames' >&2; exit 1
fi
sudo cmp "$scratch/manual.yaml" /etc/eufy-wall.yaml
sudo systemctl is-active --quiet "$service"
read -r output_before decoded_before <<< "$(frames)"
sleep 2
read -r output_after decoded_after <<< "$(frames)"
(( output_after > output_before && decoded_after > decoded_before ))
trial_ok=1
echo 'packaged Linux client: generated setup, manual YAML, live frames, and failed-apply rollback passed under Xvfb'
