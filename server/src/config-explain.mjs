import { FIELDS, TOP_LEVEL } from "./config.mjs";

// Keep this list aligned with the strict v2 schema. It is short enough to ship with the CLI and
// gives a hand-written YAML author defaults and constraints without requiring the web docs.
export const FIELD_HELP = Object.freeze({
  schema_version: "Use 2 for strict field checks; omit for legacy YAML. Future versions are rejected.",
  eufy: "Dedicated Eufy account credentials. Prefer the private environment file over YAML.",
  "eufy.email": "Account email; EUFY_EMAIL takes precedence.",
  "eufy.password": "Account password; EUFY_PASSWORD takes precedence. Keep it out of shared YAML.",
  "eufy.country": "Account country, default US; EUFY_COUNTRY takes precedence.",
  host: "HTTP bind address, default 0.0.0.0; choose a reachable LAN address for clients.",
  port: "HTTP port, default 3000; integer from 1 to 65535.",
  self_host: "Address go2rtc uses to pull this bridge; defaults to 127.0.0.1 for a wildcard bind.",
  data_dir: "Session and generated go2rtc config directory, default ./data; BRIDGE_DATA_DIR takes precedence.",
  go2rtc_bin: "go2rtc executable path, default go2rtc; GO2RTC_BIN takes precedence.",
  poll_ms: "Positive cloud state poll interval in milliseconds; omit for the SDK default.",
  lan: "Camera peer network policy and station hints.",
  "lan.cidr": "IPv4 camera peer CIDR, such as 192.168.1.0/24; required when lan.force is true.",
  "lan.force": "Boolean, default false; refuse public peer fallback when true.",
  "lan.station_addresses": "Optional mapping of station serials to LAN IP address hints.",
  "lan.station_addresses.<serial>": "LAN IP address hint for this station serial.",
  "lan.upgrade": "Relay-to-LAN retry policy; only useful when lan.force is false.",
  "lan.upgrade.enabled": "Boolean, default true; try to upgrade a relay connection to LAN.",
  "lan.upgrade.interval_ms": "Positive base interval between station upgrade attempts, default 120000 ms.",
  "lan.upgrade.window_ms": "Positive wait for LAN media before falling back, default 6000 ms.",
  "lan.upgrade.stable_ms": "Positive relay stability before first upgrade attempt, default 15000 ms.",
  "lan.upgrade.initial_window_ms": "Positive LAN-first startup budget, default 14000 ms.",
  "lan.upgrade.max_backoff_ms": "Positive cap on upgrade retry backoff, default 900000 ms.",
  defaults: "Defaults for camera holds, motion events, quality, and dual view.",
  "defaults.quality": "Optional live-view quality tier; unset means leave the camera setting alone.",
  "defaults.dual_view": "Optional dual-lens view: split, pip-tl, pip-tr, pip-bl, pip-br, or single; unset leaves the camera alone.",
  "defaults.hold_seconds": "Default motion hold lifetime, 1–3600 seconds; default 60.",
  "defaults.motion_events": "List of event names taking a motion hold; default motion, personDetected, doorbellPress.",
  cameras: "Camera entries keyed by serial; discovered cameras can be overridden here.",
  "cameras.<serial>": "Overrides for one discovered camera identified by its serial.",
  "cameras.<serial>.name": "Optional display name; also influences the go2rtc stream key.",
  "cameras.<serial>.enabled": "Boolean; false excludes a discovered camera, default true.",
  "cameras.<serial>.mode": "Stream policy: always, on_motion, or on_demand; wired defaults to always and battery to on_motion.",
  "cameras.<serial>.power_override": "SDK power budget claim: auto, battery, or always-on; always-on is required for continuous battery streaming.",
  "cameras.<serial>.hold_seconds": "Per-camera motion hold lifetime, 1–3600 seconds.",
  "cameras.<serial>.codec": "Declared camera codec, h264 or h265; the live feed can correct it.",
  "cameras.<serial>.quality": "Optional live-view tier for this camera; unset leaves the camera setting alone.",
  "cameras.<serial>.dual_view": "Optional dual-lens view: split, pip-tl, pip-tr, pip-bl, pip-br, or single.",
  stall: "Feed watchdog and retry timings.",
  "stall.stall_ms": "Positive no-byte interval before reopening a feed, default 30000 ms.",
  "stall.gap_ms": "Positive no-byte interval before disconnecting consumers, default 45000 ms.",
  "stall.exit_after_ms": "Positive continuous failure interval before process exit, default 300000 ms.",
  "stall.recreate_client_after": "Positive consecutive failure count before recreating the station client, default 8.",
  "stall.backoff_ms": "Nonempty list of positive retry delays in milliseconds; default [1000, 2000, 4000, 8000].",
  go2rtc: "RTSP egress policy for go2rtc.",
  "go2rtc.transcode": "never (default), auto, or always; enabled transcoding requires hardware support.",
});

export function explainConfigField(path) {
  if (!path) return FIELD_HELP;
  let canonical = path;
  if (/^cameras\.[^.]+(?:\.[^.]+)?$/.test(path) && !path.startsWith("cameras.<serial>"))
    canonical = path.replace(/^cameras\.[^.]+/, "cameras.<serial>");
  if (/^lan\.station_addresses\.[^.]+$/.test(path)) canonical = "lan.station_addresses.<serial>";
  const explanation = FIELD_HELP[canonical];
  if (!explanation) throw new Error(`unknown config field ${JSON.stringify(path)}; run eufy-bridge config explain for supported paths`);
  return { path, explanation };
}

// Schema drift should fail in tests when a new strict v2 field has no inline help.
export function missingExplainFields() {
  const required = [...TOP_LEVEL];
  for (const [group, fields] of Object.entries(FIELDS)) {
    const prefix = group === "camera" ? "cameras.<serial>" : group;
    required.push(prefix, ...fields.map((field) => `${prefix}.${field}`));
  }
  required.push("lan.station_addresses.<serial>");
  return required.filter((field) => !FIELD_HELP[field]);
}
