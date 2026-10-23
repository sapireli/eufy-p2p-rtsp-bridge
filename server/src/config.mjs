// Bootstrap file + JSON operating settings. Environment overrides remain for login/startup only.
// Legacy operating YAML is accepted only by the explicit offline migration tool. Everything the vendored ha-bridge modules read off `cfg`
// (email, password, country, session, port, selfHost, go2rtcConfig) keeps upstream's names.
import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { parse } from "yaml";
import { SettingsStore } from "./settings-store.mjs";

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
const TRANSCODE_MODES = new Set(["never", "auto", "always"]);

/** Codecs a camera can be declared as; anything else is a typo we should not silently accept. */
export const CAMERA_CODECS = new Set(["h264", "h265"]);

function cameraEntry(sn, raw) {
  const out = {};
  if (raw.name != null) out.name = String(raw.name);
  if (raw.enabled != null) out.enabled = Boolean(raw.enabled);
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
  if (raw.transcode != null) {
    if (!TRANSCODE_MODES.has(raw.transcode))
      throw new Error(`cameras.${sn}.transcode must be one of ${[...TRANSCODE_MODES].join(", ")}`);
    out.transcode = raw.transcode;
  }
  if (raw.vaapi_decode != null) {
    if (typeof raw.vaapi_decode !== "boolean") throw new Error(`cameras.${sn}.vaapi_decode must be a boolean`);
    out.vaapiDecode = raw.vaapi_decode;
  }
  if (raw.quality != null) out.quality = String(raw.quality);
  if (raw.dual_view != null) {
    if (!DUAL_VIEWS.has(raw.dual_view)) throw new Error(`cameras.${sn}.dual_view must be one of ${[...DUAL_VIEWS].join(", ")}`);
    out.dualView = raw.dual_view;
  }
  return out;
}

export const BOOTSTRAP_KEYS = new Set(["eufy", "host", "port", "rtsp_port", "go2rtc_api_port", "self_host", "data_dir", "go2rtc_bin"]);
export const LEGACY_OPERATING_ENV = ["BRIDGE_GO2RTC_TRANSCODE", "BRIDGE_GO2RTC_MAX_HEIGHT", "BRIDGE_GO2RTC_VAAPI_DEVICE", "BRIDGE_GO2RTC_VAAPI_DECODE"];

export function validateBootstrap(raw) {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw new Error("Bootstrap configuration must be a mapping");
  for (const key of Object.keys(raw)) {
    if (!BOOTSTRAP_KEYS.has(key)) throw new Error(`Legacy or unsupported bootstrap field ${key}: operating settings must be managed through a client. Run migrate-legacy-config.mjs before starting this version.`);
  }
  if (raw.eufy !== undefined) {
    if (!raw.eufy || typeof raw.eufy !== "object" || Array.isArray(raw.eufy)) throw new Error("Bootstrap eufy must be a mapping");
    for (const [key,value] of Object.entries(raw.eufy)) {
      if (!["email", "password", "country"].includes(key) || typeof value !== "string" || !value) throw new Error("Bootstrap eufy only accepts nonempty email, password and country strings");
    }
  }
  for (const key of ["host", "self_host", "data_dir", "go2rtc_bin"]) if (raw[key] !== undefined && (typeof raw[key] !== "string" || !raw[key])) throw new Error(`Bootstrap ${key} must be a nonempty string`);
  for (const key of ["port", "rtsp_port", "go2rtc_api_port"]) if (raw[key] !== undefined && (!Number.isInteger(raw[key]) || raw[key] < 1 || raw[key] > 65535)) throw new Error(`Bootstrap ${key} must be an integer from 1 to 65535`);
  return raw;
}

/** Normal startup reads bootstrap options and the JSON store only. Legacy import is an offline command. */
export function loadConfig({ env = process.env, configPath = env.BRIDGE_CONFIG || "./config.yaml" } = {}) {
  const bootstrap = validateBootstrap(existsSync(configPath) ? (parse(readFileSync(configPath, "utf8")) ?? {}) : {});
  const legacyEnv = LEGACY_OPERATING_ENV.filter(key => env[key] !== undefined);
  if (legacyEnv.length) throw new Error(`Legacy operating environment ${legacyEnv.join(", ")} is no longer supported. Run migrate-legacy-config.mjs, then remove these overrides from the service environment.`);
  const dataDir = resolve(env.BRIDGE_DATA_DIR || bootstrap.data_dir || "./data");
  const settingsStore = new SettingsStore({ dataDir });
  const {settings} = settingsStore.load();
  return { ...buildConfig({ env, raw: {...bootstrap, ...settings, data_dir: dataDir} }), settingsStore };
}

/** Internal mapper for validated JSON settings; never reads YAML or other configuration files. */
export function buildConfig({ env = process.env, raw = {} } = {}) {
  const dataDir = resolve(env.BRIDGE_DATA_DIR || raw.data_dir || "./data");
  const lanRaw = raw.lan ?? {};
  if (raw.go2rtc?.sources_file !== undefined) throw new Error("go2rtc.sources_file requires the offline migration tool; operating settings use embedded streams");
  for (const [key, source] of Object.entries(raw.go2rtc?.streams ?? {})) {
    const urls = Array.isArray(source) ? source : [source];
    if (!urls.length || urls.some(url => typeof url !== "string" || !/^(wyze|rtsp|rtsps):\/\//.test(url) || /[\r\n]/.test(url))) throw new Error(`imported go2rtc stream ${key} requires a Wyze or RTSP source`);
  }
  const cfg = {
    email: env.EUFY_EMAIL || raw.eufy?.email,
    password: env.EUFY_PASSWORD || raw.eufy?.password,
    country: env.EUFY_COUNTRY || raw.eufy?.country || "US",
    host: env.BRIDGE_HOST || raw.host || "0.0.0.0",
    port: Number(env.BRIDGE_PORT || raw.port || 3000),
    rtspPort: Number(env.BRIDGE_RTSP_PORT || raw.rtsp_port || 8554),
    go2rtcApiPort: Number(env.BRIDGE_GO2RTC_API_PORT || raw.go2rtc_api_port || 1984),
    selfHost: env.BRIDGE_SELF_HOST || raw.self_host || "127.0.0.1", // what go2rtc dials to reach us
    dataDir,
    session: resolve(dataDir, ".eufy-session.json"),
    go2rtcConfig: resolve(dataDir, "go2rtc.yaml"),
    go2rtcImported: { streams: raw.go2rtc?.streams ?? {} },
    go2rtcBin: env.GO2RTC_BIN || raw.go2rtc_bin || "go2rtc",
    // How go2rtc should egress each camera. "never" (default) passes the device bitstream through with
    // -c:v copy: the wall decodes H.265 in hardware, so there is nothing to gain from re-encoding on the
    // bridge and a great deal to lose — even hardware encoders could not hold real time for the 2160p
    // HEVC cameras (VideoToolbox measured 0.9x), and go2rtc kills a producer that falls behind, taking
    // the stream down mid-view. "auto" transcodes H.265 to H.264 for players without an HEVC decoder;
    // "always" transcodes everything. Both are opt-in; Linux transcodes try hardware first, then software fallback.
    go2rtcTranscode: String(raw.go2rtc?.transcode || "never"),
    go2rtcMaxHeight: Number(raw.go2rtc?.max_height || 0),
    // Optional Linux render-node override; unset discovers /dev/dri/renderD* devices.
    // Actual codec/driver failures fall back per producer, preserving hardware encoding where possible.
    go2rtcVaapiDevice: String(raw.go2rtc?.vaapi_device || ""),
    // Hardware decoding is attempted first. false is an explicit troubleshooting override.
    go2rtcVaapiDecode: ["true", "1"].includes(String(raw.go2rtc?.vaapi_decode ?? true).toLowerCase()),
    freshness: {
      enabled: raw.freshness?.enabled !== false,
      mode: raw.freshness?.mode ?? "observe",
      maxAgeMs: Number(raw.freshness?.max_age_ms ?? 500),
    },
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
    cameras: Object.fromEntries(Object.entries(raw.cameras ?? {}).map(([sn, c]) => [sn, cameraEntry(sn, c ?? {})])),
    stall: {
      // Bound silence during playback separately from opening a feed. A new feed
      // needs time for connection/keyframe negotiation before its first media.
      stallMs: Number(raw.stall?.stall_ms ?? 5_000),
      startupStallMs: Number(raw.stall?.startup_stall_ms ?? 30_000),
      gapMs: Number(raw.stall?.gap_ms ?? 45_000),
      exitAfterMs: Number(raw.stall?.exit_after_ms ?? 300_000),
      // Retire only the failed camera's cached SDK media source before retrying. The default
      // resets on the first failure; sibling streams and shared station/control remain intact.
      // Explicit N makes retirement eligible at the Nth consecutive failure (0 disables it).
      // At most one successful reset occurs until genuinely new media ends the failure episode.
      recreateClientAfter: Number(raw.stall?.recreate_client_after ?? 1),
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
