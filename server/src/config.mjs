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
    pollMs: raw.poll_ms != null ? Number(raw.poll_ms) : undefined,
    lan: {
      cidr: lanRaw.cidr ?? null,
      force: Boolean(lanRaw.force ?? false),
      stationAddresses: { ...(lanRaw.station_addresses ?? {}) },
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
      // After this many consecutive open failures, recreate the stream client (fresh P2P session) — a
      // reused session can stay wedged when the device still holds the dropped one. See stream-manager.
      recreateClientAfter: Number(raw.stall?.recreate_client_after ?? 3),
      // The socket-sweep connect is reliable (it retries dropped probes continuously), so a reopen means
      // a real session drop, not a flaky connect — recover fast rather than backing off to a full minute.
      backoffMs: raw.stall?.backoff_ms ?? [1000, 2000, 4000, 8000],
    },
  };
  if (!cfg.email || !cfg.password) throw new Error("eufy email/password are required (config.yaml eufy.* or EUFY_EMAIL/EUFY_PASSWORD)");
  if (cfg.lan.cidr != null && !isValidCidr(cfg.lan.cidr)) throw new Error(`lan.cidr must be an IPv4 CIDR like 192.168.1.0/24 — octets 0–255, prefix 0–32 (got ${cfg.lan.cidr})`);
  if (cfg.lan.force && !cfg.lan.cidr) throw new Error("lan.force=true requires lan.cidr");
  if (!DUAL_VIEWS.has(cfg.defaults.dualView)) throw new Error(`defaults.dual_view must be one of ${[...DUAL_VIEWS].join(", ")}`);
  const DEBUG = /^(1|true|yes)$/i.test(env.BRIDGE_DEBUG ?? "");
  return { cfg, DEBUG };
}
