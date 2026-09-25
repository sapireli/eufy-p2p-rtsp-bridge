# Bridge configuration and setup

The Node bridge owns its setup command and YAML loader. The installed `eufy-bridge` command and the service use the same package. A handwritten YAML file is fully supported; no browser or HTTP config endpoint is needed.

## First install

After installing the server release, run `sudo eufy-bridge setup`. It lists local IPv4 interfaces, asks for a dedicated Eufy account and LAN CIDR, and stores credentials in `/etc/eufy-wall-bridge.env` with mode `0600`. Setup first starts the bridge on `127.0.0.1` for login and 2FA or captcha; challenge answers are sent in JSON POST bodies. It then applies the chosen client listener, checks health, optionally probes selected camera streams, and enables the service only after those checks pass. A failed step restores the previous YAML and secrets. Use a terminal connected to the server; an SSH terminal works.

For noninteractive setup, create an answer file with mode `0600`. Keep it private and remove it after use. Do not type passwords into command arguments; they can remain in shell history.

```yaml
email: wall@example.com
password: your-password
country: US
lan_cidr: 192.168.1.0/24
lan_force: true
host: 192.168.1.10
client_host: 192.168.1.10
port: 3000
probe_streams: [T8410XXXXXXXXXXX]
cameras:
  T8410XXXXXXXXXXX: { mode: always }
```

```sh
umask 077
${EDITOR:-vi} ./answers.yaml
sudo eufy-bridge setup --answers ./answers.yaml
```

`host` is the local IPv4 address where the bridge listens after login, or `0.0.0.0` for all interfaces. `client_host` is the hostname or IPv4 address printed in URLs for display clients; it must be reachable by those clients. Setup checks that a specific bind address belongs to the server and that the selected LAN CIDR contains a local interface. `probe_streams` is optional: `true` probes all enabled cameras, `false` skips probes, and a serial list probes only those cameras. An interactive setup asks which cameras to probe; a noninteractive setup skips probes unless the answer file selects them. Each selected probe checks the RTSP video description and a bounded live bridge stream, then releases any temporary hold on a sleeping camera. A camera that cannot be woken or streamed makes setup fail and roll back, so choose probes deliberately for battery cameras.

For hand-edited config, use `eufy-bridge config example` to print the versioned template and `eufy-bridge config explain cameras` for short inline help. The runtime file is `/etc/eufy-wall-bridge.yaml` in a release install, overridable with `BRIDGE_CONFIG`. The environment file is `/etc/eufy-wall-bridge.env`, overridable with `BRIDGE_ENV`. Shell or systemd environment values take precedence over YAML credentials.

## Manual YAML path

Save credentials separately:

```sh
sudo install -m 0600 /dev/null /etc/eufy-wall-bridge.env
sudoedit /etc/eufy-wall-bridge.env
```

The file contains `EUFY_EMAIL="wall@example.com"`, `EUFY_PASSWORD="..."`, and `EUFY_COUNTRY="US"`. Then author the YAML:

```yaml
schema_version: 2
host: 0.0.0.0
port: 3000
lan:
  cidr: 192.168.1.0/24
  force: true
cameras:
  T8410XXXXXXXXXXX: { name: Garage, mode: always, codec: h264 }
  T8214XXXXXXXXXXX: { name: Front door, mode: on_motion, hold_seconds: 60 }
go2rtc:
  transcode: never
```

```sh
eufy-bridge config validate ./bridge.yaml
sudo eufy-bridge config apply ./bridge.yaml
cat ./bridge.yaml | ssh server 'sudo eufy-bridge config apply -'
eufy-bridge status --json
eufy-bridge doctor
eufy-bridge inventory export cameras.json --host 192.168.1.10
```

`config validate` is offline: it checks YAML structure and the bridge's config rules, using available environment credentials. `config apply` reads a file or stdin once, validates it, keeps a dated backup, atomically replaces the active file, restarts the service, and waits up to 30 seconds for `/healthz` to report authenticated health. If that check fails, it restores the prior file, restarts the service again, retains the failed candidate, records the reason in `<config>.apply-status.json`, and exits nonzero. Reapplying identical bytes skips a restart. `status` shows the last rollback; `doctor` checks Node, config, go2rtc, and live bridge health. `--json` is supported on the noninteractive commands.

To upgrade an unversioned or `schema_version: 1` file:

```sh
eufy-bridge config migrate ./old-bridge.yaml --output ./bridge-v2.yaml
eufy-bridge config validate ./bridge-v2.yaml
sudo eufy-bridge config apply ./bridge-v2.yaml
```

The migration command accepts `-` for stdin, writes the candidate with mode `0600`, and reports a path-level diff against the active file. It does not replace the active file or restart the bridge. Review the candidate before the explicit apply. Without `--output`, the candidate YAML is included in the command output; a file with an inline `eufy.password` requires `--output` to avoid printing that password to the terminal. If the permissive legacy file has keys that v2 does not support, migration lists each omitted path under `unsupportedPaths`, writes the supported candidate, and exits with code 2. Resolve those paths before applying; the command never silently claims a lossy conversion is complete. YAML comments and formatting are regenerated, so keep the original for reference.

The apply command also writes a short transaction journal while a candidate is being checked. A release service should run `eufy-bridge config recover` as a root `ExecStartPre` step so a power loss during apply restores the last backed-up file before the bridge starts. It leaves an in-progress apply alone while its owner process is alive.

The exported inventory is a JSON file with `schema_version: 1`, `bridge_url`, and camera entries keyed by serial. It contains no Eufy credentials or session tokens. Specify `--host` when the server has more than one client-facing address; otherwise the first non-loopback IPv4 address is used. It includes RTSP URLs, so review the file before sharing it outside your LAN.

## YAML reference

Omitting `schema_version` keeps the legacy format. New files should set `schema_version: 2`; the loader rejects unknown fields in v2 and rejects unknown future major versions. The existing credential fields `eufy.email`, `eufy.password`, `eufy.country` still load, but the environment file is preferred so YAML can be kept in Git. `eufy-bridge config example` contains a complete annotated template.

| Field | Default | Meaning |
| --- | --- | --- |
| `host`, `port` | `0.0.0.0`, `3000` | HTTP listener. Port is 1–65535. |
| `self_host` | `127.0.0.1` | Address go2rtc uses to pull the bridge stream. |
| `data_dir`, `go2rtc_bin` | `./data`, `go2rtc` | Session/generated config directory and go2rtc executable. The release service overrides both paths. |
| `poll_ms` | SDK default | Cloud state poll interval in milliseconds. |
| `lan.cidr`, `lan.force` | unset, `false` | IPv4 peer network; force requires a CIDR and refuses peers outside it. |
| `lan.station_addresses` | `{}` | Optional station serial to LAN address hints. |
| `lan.upgrade.*` | enabled, timed defaults | Relay-to-LAN attempt timing; see template for each interval. |
| `defaults.quality`, `defaults.dual_view` | unset | Only set when you deliberately want the bridge to write these camera settings. |
| `defaults.hold_seconds` | `60` | Motion hold lifetime, 1–3600 seconds; later motion extends it. |
| `defaults.motion_events` | motion, personDetected, doorbellPress | Events that can take a motion hold. |
| `cameras.<serial>.enabled` | `true` | Exclude a discovered camera when false. |
| `cameras.<serial>.mode` | wired: always; battery: on_motion | `always`, `on_motion`, or `on_demand`. Continuous battery service requires `power_override: always-on`. |
| `cameras.<serial>.power_override` | `auto` | SDK budget claim: `auto`, `battery`, or `always-on`. This does not change device power. |
| `cameras.<serial>.codec` | learned live | `h264` or `h265`; helps the client choose a decoder before live metadata arrives. |
| `cameras.<serial>.name`, `.quality`, `.dual_view`, `.hold_seconds` | device/default | Per-camera overrides; `hold_seconds` is 1–3600 seconds. Dual view accepts `split`, four `pip-*` corners, or `single`. |
| `go2rtc.transcode` | `never` | `never`, `auto`, or `always`; hardware-only transcoding when enabled. |
| `stall.stall_ms`, `.gap_ms`, `.exit_after_ms`, `.recreate_client_after`, `.backoff_ms` | 30000, 45000, 300000, 8, `[1000,2000,4000,8000]` | Feed recovery timers and retry schedule. |

If validation fails, the error names the YAML key or parse line. Existing configurations without a schema version continue to load; review them with `config validate` before applying a changed version. Challenge answers should use the CLI or a JSON POST body to `/auth/tfa` or `/auth/captcha` on loopback, for example `curl -X POST -H 'content-type: application/json' --data '{"code":"123456"}' http://127.0.0.1:3000/auth/tfa`. The legacy query form still works for older callers but exposes answers in URL logs.
