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

function cameraEntry(sn, raw) {
  const out = {};
  if (raw.name != null) out.name = String(raw.name);
  if (raw.enabled != null) out.enabled = Boolean(raw.enabled);
  if (raw.quality != null) out.quality = String(raw.quality);
  if (raw.dual_view != null) {
    if (!DUAL_VIEWS.has(raw.dual_view)) throw new Error(`cameras.${sn}.dual_view must be one of ${[...DUAL_VIEWS].join(", ")}`);
    out.dualView = raw.dual_view;
  }
  return out;
}

export function loadConfig({ env = process.env, configPath = env.BRIDGE_CONFIG || "./config.yaml" } = {}) {
  const raw = existsSync(configPath) ? (parse(readFileSync(configPath, "utf8")) ?? {}) : {};
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
    // How go2rtc should egress each camera. "copy" passes the device bitstream through untouched (lowest
    // CPU) but pins the RTSP SDP to whatever the first keyframe said — so when a flapping camera reopens at
    // a different resolution, already-connected players freeze on "input format change". "auto" keeps copy
    // for H.264 (stable, and what a Pi can decode) and transcodes H.265 to H.264, which both stabilises the
    // output format across reconnects and makes those cameras playable on clients with no HEVC decoder.
    go2rtcTranscode: String(raw.go2rtc?.transcode ?? "auto"),
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
      quality: raw.defaults?.quality ?? null,
      dualView: raw.defaults?.dual_view ?? "split",
    },
    cameras: Object.fromEntries(Object.entries(raw.cameras ?? {}).map(([sn, c]) => [sn, cameraEntry(sn, c ?? {})])),
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
      // The socket-sweep connect is reliable (it retries dropped probes continuously), so a reopen means
      // a real session drop, not a flaky connect — recover fast rather than backing off to a full minute.
      backoffMs: raw.stall?.backoff_ms ?? [1000, 2000, 4000, 8000],
      // How long the SDK waits for a stream to produce its first frame/keyframe before failing the open.
      // The eufy app rides out multi-second dead spots on the SAME session (packet capture: an 11s gap with
      // nothing but ACKs, then video resumed — no reconnect, no restart command). The SDK's 20s default was
      // tearing our feed down mid-recovery, and each rebuild can bring the camera back at a different
      // resolution, which breaks an already-negotiated RTSP session. Be patient like the app instead.
      warmTimeoutMs: Number(raw.stall?.warm_timeout_ms ?? 45_000),
    },
  };
  if (!cfg.email || !cfg.password) throw new Error("eufy email/password are required (config.yaml eufy.* or EUFY_EMAIL/EUFY_PASSWORD)");
  if (cfg.lan.cidr != null && !isValidCidr(cfg.lan.cidr)) throw new Error(`lan.cidr must be an IPv4 CIDR like 192.168.1.0/24 — octets 0–255, prefix 0–32 (got ${cfg.lan.cidr})`);
  if (cfg.lan.force && !cfg.lan.cidr) throw new Error("lan.force=true requires lan.cidr");
  if (!DUAL_VIEWS.has(cfg.defaults.dualView)) throw new Error(`defaults.dual_view must be one of ${[...DUAL_VIEWS].join(", ")}`);
  const DEBUG = /^(1|true|yes)$/i.test(env.BRIDGE_DEBUG ?? "");
  return { cfg, DEBUG };
}
