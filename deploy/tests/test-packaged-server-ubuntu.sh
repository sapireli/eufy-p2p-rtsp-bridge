#!/usr/bin/env bash
# Fresh Ubuntu trial of the real packaged bridge CLI and installer. No Eufy login or camera is used.
set -euo pipefail
[[ ${CI:-} == true && $(uname -s) == Linux && $(uname -m) == x86_64 ]] || {
  echo 'requires an isolated Ubuntu amd64 CI runner' >&2; exit 1;
}
. /etc/os-release
[[ ${ID:-} == ubuntu ]] || { echo 'requires Ubuntu' >&2; exit 1; }
repo=$(cd "$(dirname "$0")/../.." && pwd)
for path in /opt/eufy-wall-bridge /var/lib/eufy-wall-bridge \
  /etc/eufy-wall-bridge.yaml /etc/eufy-wall-bridge.env /etc/eufy-wall-bridge.example.yaml \
  /etc/systemd/system/eufy-wall-bridge.service /usr/local/bin/eufy-bridge; do
  [[ ! -e $path && ! -L $path ]] || { echo "CI runner is not clean: $path" >&2; exit 1; }
done
id -u eufy-wall >/dev/null 2>&1 && { echo 'CI runner already has eufy-wall user' >&2; exit 1; }

scratch=$(mktemp -d)
health_pid=
cleanup() {
  [[ -z $health_pid ]] || { kill "$health_pid" 2>/dev/null || true; wait "$health_pid" 2>/dev/null || true; }
  sudo systemctl disable --now eufy-wall-bridge.service >/dev/null 2>&1 || true
  sudo rm -f /etc/systemd/system/eufy-wall-bridge.service /usr/local/bin/eufy-bridge \
    /etc/eufy-wall-bridge.yaml /etc/eufy-wall-bridge.env /etc/eufy-wall-bridge.example.yaml
  sudo rm -rf /opt/eufy-wall-bridge /var/lib/eufy-wall-bridge
  sudo userdel eufy-wall >/dev/null 2>&1 || true
  sudo systemctl daemon-reload >/dev/null 2>&1 || true
  rm -rf "$scratch"
}
trap cleanup EXIT

version=v0.0.0-ci
bash "$repo/deploy/package-release.sh" server "$version" amd64 "$scratch"
archive="$scratch/eufy-wall-bridge-$version-linux-amd64.tar.gz"
(cd "$scratch" && sha256sum "${archive##*/}" > SHA256SUMS)
digest=$(sha256sum "$archive"); digest=${digest%% *}
installer=(bash "$repo/deploy/install-server.sh" --artifact "$archive" \
  --checksums "$scratch/SHA256SUMS" --trusted-sha256 "$digest")
"${installer[@]}" --verify-only
[[ ! -e /opt/eufy-wall-bridge && ! -e /usr/local/bin/eufy-bridge ]]
sudo "${installer[@]}"
[[ $(cat /opt/eufy-wall-bridge/current/VERSION) == "$version" ]]
[[ -x /opt/eufy-wall-bridge/current/bin/node && -x /opt/eufy-wall-bridge/current/bin/go2rtc ]]
[[ -d /opt/eufy-wall-bridge/current/server/node_modules/@mega-yfue/eufy-sdk ]]
[[ $(/opt/eufy-wall-bridge/current/bin/node --version) == v24.5.0 ]]
[[ $(stat -c '%a %U %G' /etc/eufy-wall-bridge.env) == '600 root root' ]]
[[ ! -e /etc/eufy-wall-bridge.yaml ]]
! sudo systemctl is-enabled --quiet eufy-wall-bridge.service
! sudo systemctl is-active --quiet eufy-wall-bridge.service

# These run as the unprivileged CI user while the installed env file remains root:0600.
eufy-bridge config example > "$scratch/example.yaml"
python3 - "$scratch/example.yaml" /opt/eufy-wall-bridge/current/server/config.example.yaml <<'PY'
import pathlib, sys
assert pathlib.Path(sys.argv[1]).read_text().strip() == pathlib.Path(sys.argv[2]).read_text().strip()
PY
cat > "$scratch/manual.yaml" <<'EOF'
schema_version: 2
host: 127.0.0.1
port: 3000
cameras:
  CAM: { name: Manual, mode: on_motion }
EOF
export EUFY_EMAIL=ci@example.invalid EUFY_PASSWORD=ci-only-not-real
if eufy-bridge config validate "$scratch/manual.yaml" --json > "$scratch/unprivileged.out" 2> "$scratch/unprivileged.err"; then
  echo 'unprivileged validation unexpectedly read root-owned credentials' >&2; exit 1
fi
python3 - "$scratch/unprivileged.err" <<'PY'
import json, sys
assert json.load(open(sys.argv[1]))['diagnostics'][0]['code'] == 'CONFIG_SECRETS_UNREADABLE'
PY
cat > "$scratch/local.env" <<'EOF'
EUFY_EMAIL="ci@example.invalid"
EUFY_PASSWORD="ci-only-not-real"
EOF
chmod 600 "$scratch/local.env"
sudo install -m 600 -o root -g root "$scratch/local.env" /etc/eufy-wall-bridge.env
sudo eufy-bridge config validate "$scratch/manual.yaml" --json > "$scratch/file-validation.json"
sudo eufy-bridge config validate - --json < "$scratch/manual.yaml" > "$scratch/stdin-validation.json"
python3 - "$scratch/file-validation.json" "$scratch/stdin-validation.json" <<'PY'
import json, sys
for path in sys.argv[1:]:
    result = json.load(open(path))
    assert result['ok'] is True and result['schemaVersion'] == 2 and result['cameras'] == 1
PY
printf 'schema_version: 2\nporrt: 3000\n' > "$scratch/invalid.yaml"
if sudo eufy-bridge config validate "$scratch/invalid.yaml" --json > "$scratch/invalid.out" 2> "$scratch/invalid.err"; then
  echo 'invalid manual YAML passed validation' >&2; exit 1
fi
python3 - "$scratch/invalid.err" <<'PY'
import json, sys
diagnostic = json.load(open(sys.argv[1]))['diagnostics'][0]
assert diagnostic['code'] == 'CONFIG_UNSUPPORTED_KEY' and diagnostic['path'] == 'porrt'
PY
if BRIDGE_CONFIG="$scratch/stdin-apply.yaml" BRIDGE_ENV="$scratch/local.env" eufy-bridge config apply - --json < "$scratch/invalid.yaml" > "$scratch/invalid-apply.out" 2> "$scratch/invalid-apply.err"; then
  echo 'invalid stdin YAML unexpectedly applied' >&2; exit 1
fi
[[ ! -e "$scratch/stdin-apply.yaml" && ! -e /etc/eufy-wall-bridge.yaml ]]

# Exercise the installed CLI's actual atomic apply/health rollback with a local health endpoint.
# systemctl is stubbed only for this transaction; the packaged service never contacts Eufy.
cat > "$scratch/health.py" <<'PY'
import http.server, json, pathlib, sys
active, port_file = map(pathlib.Path, sys.argv[1:])
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        state = 'pending' if 'name: unhealthy' in active.read_text() else 'ok'
        body = json.dumps({'ok': True, 'auth': {'state': state}}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *args): pass
server = http.server.HTTPServer(('127.0.0.1', 0), Handler)
port_file.write_text(str(server.server_port))
server.serve_forever()
PY
active="$scratch/active.yaml"
python3 "$scratch/health.py" "$active" "$scratch/health-port" & health_pid=$!
for ((i=0; i<50; i++)); do [[ -s $scratch/health-port ]] && break; sleep 0.1; done
[[ -s $scratch/health-port ]] || { echo 'synthetic health server did not start' >&2; exit 1; }
port=$(cat "$scratch/health-port")
cat > "$active" <<EOF
schema_version: 2
host: 127.0.0.1
port: $port
cameras:
  CAM: { name: healthy, mode: on_motion }
EOF
chmod 640 "$active"
cp "$active" "$scratch/previous.yaml"
cat > "$scratch/candidate.yaml" <<EOF
schema_version: 2
host: 127.0.0.1
port: $port
cameras:
  CAM: { name: unhealthy, mode: on_motion }
EOF
mkdir "$scratch/bin"
cat > "$scratch/bin/systemctl" <<'EOF'
#!/bin/sh
[ "$1" = restart ] && [ "$2" = eufy-wall-bridge.service ] || exit 2
printf 'restart\n' >> "$RESTART_LOG"
EOF
chmod 755 "$scratch/bin/systemctl"
unset EUFY_EMAIL EUFY_PASSWORD
export BRIDGE_CONFIG="$active" BRIDGE_ENV="$scratch/local.env" RESTART_LOG="$scratch/restarts"
if PATH="$scratch/bin:$PATH" eufy-bridge config apply "$scratch/candidate.yaml" --json > "$scratch/apply.out" 2> "$scratch/apply.err"; then
  echo 'unhealthy manual YAML unexpectedly applied' >&2; exit 1
fi
cmp "$active" "$scratch/previous.yaml"
[[ $(stat -c '%a' "$active") == 640 ]]
[[ $(wc -l < "$scratch/restarts") == 2 ]]
python3 - "$active.apply-status.json" "$scratch/candidate.yaml" <<'PY'
import json, pathlib, sys
status = json.loads(pathlib.Path(sys.argv[1]).read_text())
rollback = status['lastRollback']
assert 'health check failed' in rollback['reason']
assert pathlib.Path(rollback['failed']).read_text() == pathlib.Path(sys.argv[2]).read_text()
assert pathlib.Path(rollback['backup']).exists()
PY
PATH="$scratch/bin:$PATH" eufy-bridge status --json > "$scratch/status.json"
python3 - "$scratch/status.json" <<'PY'
import json, sys
status = json.load(open(sys.argv[1]))
assert 'health check failed' in status['config']['lastRollback']['reason']
PY
[[ ! -e "$active.apply-pending.json" && ! -e "$active.apply-lock" ]]
[[ ! -e /etc/eufy-wall-bridge.yaml ]]
! sudo systemctl is-active --quiet eufy-wall-bridge.service
echo 'packaged Ubuntu server install, manual YAML validation, and failed-apply rollback passed (no Eufy camera)'
