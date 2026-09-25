// Config = YAML file + env overrides. Env wins for secrets (EUFY_EMAIL/EUFY_PASSWORD/EUFY_COUNTRY) so the
// yaml can be committed without credentials. Everything the vendored ha-bridge modules read off `cfg`
// (email, password, country, session, port, selfHost, go2rtcConfig) keeps upstream's names.
import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { parse } from "yaml";

const CIDR_RE = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/;

/** True for a well-formed IPv4 CIDR: four 0–255 octets and a 0–32 prefix length (the regex only checks shape). */
export function isValidCidr(v) {
  if (typeof v !== "string" || !CIDR_RE.test(v)) return false;
  const [net, bits] = v.split("/");
  return net.split(".").every((o) => Number(o) <= 255) && Number(bits) <= 32;
}
const DUAL_VIEWS = new Set(["split", "pip-tl", "pip-tr", "pip-bl", "pip-br", "single"]);
/**
 * When a camera streams.
 *  always    — continuously (a wired camera; Phase 1 behaviour, and the default for one)
 *  on_motion — idle until an event takes a hold on it (the default for a battery camera, which a
 *              continuous stream would keep awake and flatten)
 *  on_demand — only while something is actually watching it
 */
const CAMERA_MODES = new Set(["always", "on_motion", "on_demand"]);
const POWER_OVERRIDES = new Set(["auto", "always-on", "battery"]);
const TOP_LEVEL = new Set(["schema_version", "eufy", "host", "port", "self_host", "data_dir", "go2rtc_bin", "poll_ms", "lan", "defaults", "cameras", "stall", "go2rtc"]);
const FIELDS = {
  eufy: ["email", "password", "country"],
  lan: ["cidr", "force", "station_addresses", "upgrade"],
  "lan.upgrade": ["enabled", "interval_ms", "window_ms", "stable_ms", "initial_window_ms", "max_backoff_ms"],
  defaults: ["quality", "dual_view", "hold_seconds", "motion_events"],
  stall: ["stall_ms", "gap_ms", "exit_after_ms", "recreate_client_after", "backoff_ms"],
  go2rtc: ["transcode"],
  camera: ["name", "enabled", "mode", "power_override", "hold_seconds", "codec", "quality", "dual_view"],
};

function record(value, path) {
  if (value == null) return {};
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${path} must be a mapping`);
  return value;
}

function positiveNumber(value, path) {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) throw new Error(`${path} must be a positive number`);
}
function stringField(value, path) {
  if (typeof value !== "string" || !value.trim()) throw new Error(`${path} must be a nonempty string`);
}

function checkKeys(value, allowed, path = "") {
  for (const key of Object.keys(record(value, path || "config"))) {
    if (!allowed.has(key)) throw new Error(`unsupported config key ${path ? `${path}.` : ""}${key}`);
  }
}

/** Parse and check structural fields before runtime defaults or environment overrides are applied. */
export function parseConfigText(text) {
  const raw = record(parse(text, { uniqueKeys: true }), "config");
  if (raw.schema_version != null && raw.schema_version !== 1 && raw.schema_version !== 2)
    throw new Error(`schema_version ${raw.schema_version} is unsupported; upgrade eufy-bridge for newer config formats`);
  // Legacy files remain permissive; versioned files reject misspelled fields instead of silently dropping them.
  if (raw.schema_version === 2) {
    checkKeys(raw, TOP_LEVEL);
    for (const key of ["eufy", "lan", "defaults", "stall", "go2rtc"]) checkKeys(raw[key], new Set(FIELDS[key]), key);
    checkKeys(raw.lan?.upgrade, new Set(FIELDS["lan.upgrade"]), "lan.upgrade");
    for (const [sn, camera] of Object.entries(record(raw.cameras, "cameras"))) checkKeys(camera, new Set(FIELDS.camera), `cameras.${sn}`);
  }
  for (const key of ["eufy", "lan", "defaults", "stall", "go2rtc", "cameras"]) record(raw[key], key);
  for (const key of ["host", "self_host", "data_dir", "go2rtc_bin"]) if (raw[key] != null) stringField(raw[key], key);
  for (const key of ["email", "password", "country"]) if (raw.eufy?.[key] != null) stringField(raw.eufy[key], `eufy.${key}`);
  if (raw.lan?.force != null && typeof raw.lan.force !== "boolean") throw new Error("lan.force must be true or false");
  if (raw.lan?.upgrade?.enabled != null && typeof raw.lan.upgrade.enabled !== "boolean") throw new Error("lan.upgrade.enabled must be true or false");
  for (const [sn, address] of Object.entries(record(raw.lan?.station_addresses, "lan.station_addresses"))) stringField(address, `lan.station_addresses.${sn}`);
  for (const [sn, camera] of Object.entries(record(raw.cameras, "cameras"))) {
    if (camera?.name != null) stringField(camera.name, `cameras.${sn}.name`);
    if (camera?.quality != null) stringField(camera.quality, `cameras.${sn}.quality`);
  }
  if (raw.port != null && (!Number.isInteger(raw.port) || raw.port < 1 || raw.port > 65535)) throw new Error("port must be an integer from 1 to 65535");
  if (raw.poll_ms != null) positiveNumber(raw.poll_ms, "poll_ms");
  if (raw.go2rtc?.transcode != null && !["never", "auto", "always"].includes(raw.go2rtc.transcode)) throw new Error("go2rtc.transcode must be never, auto, or always");
  if (raw.defaults?.hold_seconds != null) positiveNumber(raw.defaults.hold_seconds, "defaults.hold_seconds");
  if (raw.defaults?.motion_events != null && (!Array.isArray(raw.defaults.motion_events) || !raw.defaults.motion_events.every((x) => typeof x === "string"))) throw new Error("defaults.motion_events must be a list of event names");
  for (const key of ["stall_ms", "gap_ms", "exit_after_ms", "recreate_client_after"]) if (raw.stall?.[key] != null) positiveNumber(raw.stall[key], `stall.${key}`);
  if (raw.stall?.backoff_ms != null && (!Array.isArray(raw.stall.backoff_ms) || !raw.stall.backoff_ms.length || raw.stall.backoff_ms.some((x) => typeof x !== "number" || !Number.isFinite(x) || x <= 0))) throw new Error("stall.backoff_ms must be a nonempty list of positive milliseconds");
  for (const key of ["interval_ms", "window_ms", "stable_ms", "initial_window_ms", "max_backoff_ms"]) if (raw.lan?.upgrade?.[key] != null) positiveNumber(raw.lan.upgrade[key], `lan.upgrade.${key}`);
  return raw;
}

/** Codecs a camera can be declared as; anything else is a typo we should not silently accept. */
export const CAMERA_CODECS = new Set(["h264", "h265"]);

function cameraEntry(sn, raw) {
  if (raw == null) throw new Error(`cameras.${sn} must be a mapping`);
  raw = record(raw, `cameras.${sn}`);
  const out = {};
  if (raw.name != null) out.name = String(raw.name);
  if (raw.enabled != null) {
    if (typeof raw.enabled !== "boolean") throw new Error(`cameras.${sn}.enabled must be true or false`);
    out.enabled = raw.enabled;
  }
  if (raw.mode != null) {
    if (!CAMERA_MODES.has(raw.mode)) throw new Error(`cameras.${sn}.mode must be one of ${[...CAMERA_MODES].join(", ")}`);
    out.mode = raw.mode;
  }
  if (raw.power_override != null) {
    if (!POWER_OVERRIDES.has(raw.power_override))
      throw new Error(`cameras.${sn}.power_override must be one of ${[...POWER_OVERRIDES].join(", ")}`);
    out.powerOverride = raw.power_override;
  }
  if (raw.hold_seconds != null) {
    const n = Number(raw.hold_seconds);
    if (!Number.isFinite(n) || n <= 0) throw new Error(`cameras.${sn}.hold_seconds must be a positive number of seconds`);
    out.holdSeconds = n;
  }
  if (raw.codec != null) {
    // A declared codec lets automatic transcoding choose its egress before the first frame arrives.
    // The live feed still corrects the declaration if the device reports a different codec.
    const codec = String(raw.codec).toLowerCase();
    if (!CAMERA_CODECS.has(codec)) throw new Error(`cameras.${sn}.codec must be one of ${[...CAMERA_CODECS].join(", ")}`);
    out.codec = codec;
  }
  if (raw.quality != null) out.quality = String(raw.quality);
  if (raw.dual_view != null) {
    if (!DUAL_VIEWS.has(raw.dual_view)) throw new Error(`cameras.${sn}.dual_view must be one of ${[...DUAL_VIEWS].join(", ")}`);
    out.dualView = raw.dual_view;
  }
  return out;
}

export function loadConfig({ env = process.env, configPath = env.BRIDGE_CONFIG || "./config.yaml", rawText } = {}) {
  const raw = rawText !== undefined ? parseConfigText(rawText) : (existsSync(configPath) ? parseConfigText(readFileSync(configPath, "utf8")) : {});
  const dataDir = resolve(env.BRIDGE_DATA_DIR || raw.data_dir || "./data");
  const lanRaw = raw.lan ?? {};
  const cfg = {
    email: env.EUFY_EMAIL || raw.eufy?.email,
    password: env.EUFY_PASSWORD || raw.eufy?.password,
    country: env.EUFY_COUNTRY || raw.eufy?.country || "US",
    host: env.BRIDGE_HOST || raw.host || "0.0.0.0",
    port: Number(env.BRIDGE_PORT || raw.port || 3000),
    selfHost: env.BRIDGE_SELF_HOST || raw.self_host || "127.0.0.1", // what go2rtc dials to reach us
    dataDir,
    session: resolve(dataDir, ".eufy-session.json"),
    go2rtcConfig: resolve(dataDir, "go2rtc.yaml"),
    go2rtcBin: env.GO2RTC_BIN || raw.go2rtc_bin || "go2rtc",
    // How go2rtc should egress each camera. "never" (default) passes the device bitstream through with
    // -c:v copy: the wall decodes H.265 in hardware, so there is nothing to gain from re-encoding on the
    // bridge and a great deal to lose — even hardware encoders could not hold real time for the 2160p
    // HEVC cameras (VideoToolbox measured 0.9x), and go2rtc kills a producer that falls behind, taking
    // the stream down mid-view. "auto" transcodes H.265 to H.264 for players without an HEVC decoder;
    // "always" transcodes everything. Both are opt-in and hardware-only (see egressFor).
    go2rtcTranscode: String(raw.go2rtc?.transcode ?? "never"),
    pollMs: raw.poll_ms != null ? Number(raw.poll_ms) : undefined,
    lan: {
      cidr: lanRaw.cidr ?? null,
      force: Boolean(lanRaw.force ?? false),
      stationAddresses: { ...(lanRaw.station_addresses ?? {}) },
      // Opportunistic LAN upgrade: start on whatever connects first (usually relay for a HomeBase), then
      // periodically try to migrate a station to a DIRECT-LAN peer and lock it there. Only meaningful when
      // force=false (force=true is already LAN-only). See lan-upgrade.mjs.
      upgrade: {
        enabled: Boolean(lanRaw.upgrade?.enabled ?? true),
        intervalMs: Number(lanRaw.upgrade?.interval_ms ?? 120_000), // base gap between attempts per station
        windowMs: Number(lanRaw.upgrade?.window_ms ?? 6_000),      // wait this long for LAN bytes before giving up
        stableMs: Number(lanRaw.upgrade?.stable_ms ?? 15_000),      // relay must stream this long before first try
        initialWindowMs: Number(lanRaw.upgrade?.initial_window_ms ?? 14_000), // LAN-first budget at boot before relay fallback
        maxBackoffMs: Number(lanRaw.upgrade?.max_backoff_ms ?? 900_000), // cap the exponential retry backoff
      },
    },
    defaults: {
      // Both default to UNSET, and unset means "do not write this to the camera".
      //
      // Dual view and streaming quality belong to the eufy app, and only the owner account can change
      // them there. Writing them from the bridge is a SET_PAYLOAD to a camera that may be mid-stream, for
      // a setting the operator has usually already chosen — so it is opt-in, for the case where someone
      // genuinely wants the bridge to force it.
      quality: raw.defaults?.quality ?? null,
      dualView: raw.defaults?.dual_view ?? null,
      // How long motion keeps a camera streaming. A bound, not a starting point: a battery camera held
      // open indefinitely is the failure this whole mode exists to avoid. Further motion extends it.
      holdSeconds: Number(raw.defaults?.hold_seconds ?? 60),
      // Events that take a hold. Named by the SDK's semantic vocabulary; a device only fires the ones
      // dev.describe() says it emits, so listing an event a camera cannot send is inert, not an error.
      motionEvents: raw.defaults?.motion_events ?? ["motion", "personDetected", "doorbellPress"],
    },
    cameras: Object.fromEntries(Object.entries(raw.cameras ?? {}).map(([sn, c]) => [sn, cameraEntry(sn, c)])),
    stall: {
      // 30 s, not 12 s: a HomeBase-attached camera can briefly go silent while the station favours a
      // sibling channel, and the SDK's own LiveStream re-assert recovers it within a few seconds. Tearing
      // the feed down too eagerly fights that recovery and makes co-located cameras flap. A truly dead
      // session is caught faster by the SDK's heartbeat (feed error/close), not this timer.
      stallMs: Number(raw.stall?.stall_ms ?? 30_000),
      gapMs: Number(raw.stall?.gap_ms ?? 45_000),
      exitAfterMs: Number(raw.stall?.exit_after_ms ?? 300_000),
      // Tear the whole STATION session down only after this many consecutive failures (0 = never). A
      // per-camera media open failing self-closes just that media session in the SDK, so a retry re-lookups
      // its port without disturbing the shared control session or sibling cameras — tearing the station down
      // on every camera flap would keep killing the connection all cameras share. Reserve teardown for a
      // control session that looks truly dead (many failures in a row).
      recreateClientAfter: Number(raw.stall?.recreate_client_after ?? 8),
      // A failed open retries promptly; longer waits leave a feed unavailable even after its path recovers.
      backoffMs: raw.stall?.backoff_ms ?? [1000, 2000, 4000, 8000],
    },
  };
  if (!cfg.email || !cfg.password) throw new Error("eufy email/password are required (config.yaml eufy.* or EUFY_EMAIL/EUFY_PASSWORD)");
  if (cfg.lan.cidr != null && !isValidCidr(cfg.lan.cidr)) throw new Error(`lan.cidr must be an IPv4 CIDR like 192.168.1.0/24 — octets 0–255, prefix 0–32 (got ${cfg.lan.cidr})`);
  if (cfg.lan.force && !cfg.lan.cidr) throw new Error("lan.force=true requires lan.cidr");
  if (cfg.defaults.dualView != null && !DUAL_VIEWS.has(cfg.defaults.dualView))
    throw new Error(`defaults.dual_view must be one of ${[...DUAL_VIEWS].join(", ")}`);
  const DEBUG = /^(1|true|yes)$/i.test(env.BRIDGE_DEBUG ?? "");
  return { cfg, DEBUG };
}
