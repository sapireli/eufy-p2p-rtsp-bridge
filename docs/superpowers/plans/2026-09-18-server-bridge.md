# eufy-wall-bridge (server) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single Node service that logs into eufy once, keeps every wired camera's P2P stream warm, and serves each as RTSP (via go2rtc) for the display clients — LAN-only, H.264, restart-safe, no Docker.

**Architecture:** `server.mjs` wires a `ctx` object (config + SDK client + state + module functions) exactly like upstream `ha-eufy-sdk-bridge`. Modules copied verbatim from upstream live in `server/src/vendor/ha-bridge/` and are never edited; our modules (`stream-manager`, `pins`, `lan-guard`, `cameras`, `http`, `go2rtc`) wrap them. All SDK calls go through `sdk-adapter.mjs`. go2rtc runs as a child process pulling `http://127.0.0.1:3000/stream/<sn>` (Annex-B) and re-serving RTSP on `:8554`.

**Tech Stack:** Node ≥ 24.5 (ESM, `node --test`), `@mega-yfue/eufy-sdk` 0.1.1 (pinned), `yaml`, go2rtc release binary, systemd on Debian.

**Spec:** `docs/superpowers/specs/2026-09-18-eufy-wall-design.md`

## Global Constraints

- Node `>=24.5.0` (eufy-sdk floor). Package `"type": "module"`, files are `.mjs`.
- `@mega-yfue/eufy-sdk` pinned **exactly** `0.1.1` in `package.json` (no `^`).
- **No Docker** anywhere. Deployment = `deploy/install-server.sh` + systemd unit.
- Files under `server/src/vendor/ha-bridge/` are copied verbatim from upstream commit recorded in `VENDOR.md`; never edit them — wrap them.
- Phase 1 scope: wired/powered cameras, always-on. Battery cameras are excluded with a log line unless `enabled: true` is set explicitly in config (then treated as always-on at the user's risk).
- Every log line is prefixed `[bridge]`.
- Upstream reference clones (read-only) live in the session scratchpad: `…/scratchpad/ha-eufy-sdk-bridge`, `…/scratchpad/eufy-sdk`, `…/scratchpad/eufy-frigate-bridge`. If the scratchpad is gone, `git clone --depth 1 https://github.com/mega-yfue/ha-eufy-sdk-bridge` and `…/mega-yfue/eufy-sdk` into a temp dir.

## File structure

```
server/
  package.json                 deps + scripts (test = node --test)
  config.example.yaml          documented example config
  server.mjs                   wiring only (ctx assembly, event hookup, listen, login, shutdown)
  src/
    config.mjs                 YAML + env → cfg object (+ validation)
    state.mjs                  mutable runtime state (flags, per-camera slots)
    sdk-adapter.mjs            THE ONLY file that imports @mega-yfue/eufy-sdk directly (besides vendor/)
    cameras.mjs                camera registry: merge config + SDK device list → enabled camera set + /api shape
    pins.mjs                   apply per-camera settings (dual view mode; streaming quality when supported)
    stream-manager.mjs         warm always-on feeds, fan-out to HTTP consumers, stall watchdog, backoff, exit-after-stall
    lan-guard.mjs              force-LAN: verify P2P peer address per session, close + block if WAN
    go2rtc.mjs                 write go2rtc.yaml (via vendored generator) + spawn/supervise the binary
    http.mjs                   /stream/<sn>, /snapshot/<sn>, /healthz, /api/cameras, /auth/*
    vendor/ha-bridge/
      VENDOR.md                upstream repo, commit SHA, license notice, list of files
      streams.mjs              (verbatim) session-per-camera EufyMega instances
      go2rtc-config.mjs        (verbatim) go2rtc.yaml generator
      auth.mjs                 (verbatim) login/2FA/captcha state machine
      watchdog.mjs             (verbatim) poll/push liveness watchdog
  test/
    config.test.mjs
    cameras.test.mjs
    pins.test.mjs
    stream-manager.test.mjs
    lan-guard.test.mjs
    http.test.mjs
    sdk-contract.test.mjs
  scripts/
    sync-upstream.sh           diff vendored files against upstream HEAD
  spikes/
    spike-a.mjs                throwaway: login, list, dump 10 s per camera, probe view mode + peer address
deploy/
  install-server.sh
  eufy-wall-bridge.service
  eufy-wall-bridge.env.example
docs/
  runbook-server.md
```

Module contract (every module exports `createX(ctx)` and returns plain functions; `ctx` carries `cfg`, `eufy`, `state`, and every other module's functions, read lazily at call time):

```js
// ctx shape after assembly (server.mjs)
ctx = {
  cfg, eufy, state,
  // vendored
  authStatus(), applyLogin(result), maybeRecoverSession(), onSessionExpired(),   // auth.mjs
  bumpActivity(), stallThresholdMs(), watchdogTick(),                            // watchdog.mjs
  // ours
  completeBoot(), broadcast(evt),                                                // server.mjs (broadcast = log only in Phase 1)
  listCameras(), getCamera(sn), refreshCameras(),                                // cameras.mjs
  applyPins(sn), applyAllPins(),                                                 // pins.mjs
  ensureWarm(sn), attachConsumer(sn, res), streamStatus(sn), streamTick(),       // stream-manager.mjs
  attachLanGuard(client, label), isBlocked(sn),                                  // lan-guard.mjs
  startGo2rtc(), writeGo2rtc(),                                                  // go2rtc.mjs
  PUSH_STALL_MS, SCHEMA_VERSION,
}
```

---

### Task 1: Package skeleton, config loader, example config

**Files:**
- Create: `server/package.json`, `server/src/config.mjs`, `server/config.example.yaml`, `server/test/config.test.mjs`

**Interfaces:**
- Produces: `loadConfig({ env, configPath }) → { cfg, DEBUG }` where `cfg` has:
  `email, password, country, host, port, selfHost, session, go2rtcConfig, go2rtcBin, dataDir, lan: { cidr, force, stationAddresses }, defaults: { quality, dualView }, cameras: { [sn]: { name?, enabled?, quality?, dualView? } }, stall: { stallMs, gapMs, exitAfterMs, backoffMs[] }, pollMs`

- [ ] **Step 1: Create `server/package.json`**

```json
{
  "name": "eufy-wall-bridge",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "description": "eufy P2P → RTSP bridge for the camera wall (wired, always-on)",
  "main": "server.mjs",
  "engines": { "node": ">=24.5.0" },
  "scripts": {
    "start": "node server.mjs",
    "test": "node --test test/*.test.mjs",
    "spike:a": "node spikes/spike-a.mjs"
  },
  "dependencies": {
    "@mega-yfue/eufy-sdk": "0.1.1",
    "yaml": "2.8.1"
  }
}
```

- [ ] **Step 2: Install deps**

Run: `cd server && npm install`
Expected: `node_modules/@mega-yfue/eufy-sdk/package.json` exists with `"version": "0.1.1"`. (Requires Node ≥ 24.5 locally: `node -v`.)

- [ ] **Step 3: Write the failing config test**

`server/test/config.test.mjs`:
```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { writeFileSync, mkdtempSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { loadConfig } from "../src/config.mjs";

function tmpYaml(text) {
  const dir = mkdtempSync(join(tmpdir(), "ewb-"));
  const p = join(dir, "config.yaml");
  writeFileSync(p, text);
  return p;
}

test("loads yaml, applies defaults, env overrides secrets", () => {
  const p = tmpYaml(`
eufy: { email: a@b.c, password: yamlpw, country: US }
lan: { cidr: 10.0.0.0/8, force: true, station_addresses: { T8010X: 10.0.0.5 } }
defaults: { quality: "Full HD (1080P)" }
cameras:
  T8410X: { name: Garage }
  T8214X: { dual_view: split }
  T8113X: { enabled: false }
`);
  const { cfg } = loadConfig({ env: { EUFY_PASSWORD: "envpw" }, configPath: p });
  assert.equal(cfg.email, "a@b.c");
  assert.equal(cfg.password, "envpw");
  assert.equal(cfg.country, "US");
  assert.equal(cfg.port, 3000);
  assert.equal(cfg.host, "0.0.0.0");
  assert.equal(cfg.selfHost, "127.0.0.1");
  assert.equal(cfg.lan.cidr, "10.0.0.0/8");
  assert.equal(cfg.lan.force, true);
  assert.deepEqual(cfg.lan.stationAddresses, { T8010X: "10.0.0.5" });
  assert.equal(cfg.defaults.quality, "Full HD (1080P)");
  assert.equal(cfg.defaults.dualView, "split");
  assert.deepEqual(cfg.cameras.T8410X, { name: "Garage" });
  assert.deepEqual(cfg.cameras.T8214X, { dualView: "split" });
  assert.deepEqual(cfg.cameras.T8113X, { enabled: false });
  assert.deepEqual(cfg.stall, { stallMs: 12000, gapMs: 45000, exitAfterMs: 300000, backoffMs: [2000, 4000, 8000, 15000, 30000, 60000] });
  assert.equal(cfg.go2rtcBin, "go2rtc");
});

test("rejects missing credentials and bad cidr", () => {
  const p = tmpYaml(`eufy: { email: a@b.c }\nlan: { cidr: nope }`);
  assert.throws(() => loadConfig({ env: {}, configPath: p }), /password/);
  const p2 = tmpYaml(`eufy: { email: a@b.c, password: x }\nlan: { cidr: nope }`);
  assert.throws(() => loadConfig({ env: {}, configPath: p2 }), /lan.cidr/);
});

test("missing config file → env-only config", () => {
  const { cfg } = loadConfig({ env: { EUFY_EMAIL: "e", EUFY_PASSWORD: "p", EUFY_COUNTRY: "DE" }, configPath: "/nonexistent.yaml" });
  assert.equal(cfg.country, "DE");
  assert.deepEqual(cfg.cameras, {});
  assert.equal(cfg.lan.force, false);
});
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd server && npm test`
Expected: FAIL — `Cannot find module '../src/config.mjs'`.

- [ ] **Step 5: Implement `server/src/config.mjs`**

```js
// Config = YAML file + env overrides. Env wins for secrets (EUFY_EMAIL/EUFY_PASSWORD/EUFY_COUNTRY) so the
// yaml can be committed without credentials. Everything the vendored ha-bridge modules read off `cfg`
// (email, password, country, session, port, selfHost, go2rtcConfig) keeps upstream's names.
import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { parse } from "yaml";

const CIDR_RE = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/;
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
      stallMs: Number(raw.stall?.stall_ms ?? 12_000),
      gapMs: Number(raw.stall?.gap_ms ?? 45_000),
      exitAfterMs: Number(raw.stall?.exit_after_ms ?? 300_000),
      backoffMs: raw.stall?.backoff_ms ?? [2000, 4000, 8000, 15000, 30000, 60000],
    },
  };
  if (!cfg.email || !cfg.password) throw new Error("eufy email/password are required (config.yaml eufy.* or EUFY_EMAIL/EUFY_PASSWORD)");
  if (cfg.lan.cidr != null && !CIDR_RE.test(cfg.lan.cidr)) throw new Error(`lan.cidr must look like 192.168.1.0/24 (got ${cfg.lan.cidr})`);
  if (cfg.lan.force && !cfg.lan.cidr) throw new Error("lan.force=true requires lan.cidr");
  if (!DUAL_VIEWS.has(cfg.defaults.dualView)) throw new Error(`defaults.dual_view must be one of ${[...DUAL_VIEWS].join(", ")}`);
  const DEBUG = /^(1|true|yes)$/i.test(env.BRIDGE_DEBUG ?? "");
  return { cfg, DEBUG };
}
```

- [ ] **Step 6: Run tests**

Run: `cd server && npm test`
Expected: 3 passing.

- [ ] **Step 7: Write `server/config.example.yaml`**

```yaml
# eufy-wall-bridge configuration. Copy to config.yaml (or set BRIDGE_CONFIG). Secrets may instead come
# from the environment: EUFY_EMAIL, EUFY_PASSWORD, EUFY_COUNTRY (env wins over this file).
eufy:
  email: wall@example.com        # use a DEDICATED eufy account (eufy allows one login per account;
  password: change-me            # the phone app on the same account evicts the bridge)
  country: US

host: 0.0.0.0                    # HTTP bind (go2rtc + display clients + curl)
port: 3000
# self_host: 127.0.0.1           # address go2rtc uses to pull /stream from this process
# data_dir: ./data               # session token + generated go2rtc.yaml
# go2rtc_bin: go2rtc             # path to the go2rtc binary
# poll_ms: 600000                # cloud state poll interval (SDK default 10 min)

lan:
  cidr: 192.168.1.0/24           # your LAN
  force: true                    # refuse P2P sessions whose peer is outside cidr (LAN-only streaming)
  station_addresses: {}          # optional LAN IP hints keyed by HomeBase/station serial, e.g. T8010XXXX: 192.168.1.50

defaults:
  quality: "Full HD (1080P)"     # desired live-view tier (H.264). Values: "HD (720P)" | "Full HD (1080P)" | "Max"
  dual_view: split               # dual-lens cameras: split | pip-tl | pip-tr | pip-bl | pip-br | single

cameras:                         # keyed by camera serial; unlisted WIRED cameras are included, BATTERY excluded
  T8410XXXXXXXXXXX: { name: Garage }
  T8214XXXXXXXXXXX: { name: Front door, dual_view: split }
  T8113XXXXXXXXXXX: { enabled: false }

# stall:                         # media watchdog (defaults shown)
#   stall_ms: 12000              # no bytes for this long → restart the feed
#   gap_ms: 45000                # consumers are disconnected after this long without bytes
#   exit_after_ms: 300000        # continuous failure this long → exit(1) so systemd restarts us
#   backoff_ms: [2000, 4000, 8000, 15000, 30000, 60000]
```

- [ ] **Step 8: Commit**

```bash
git add server/package.json server/package-lock.json server/src/config.mjs server/config.example.yaml server/test/config.test.mjs
git commit -m "feat(server): package skeleton and config loader"
```

---

### Task 2: Vendor upstream modules + sync script + SDK contract test

**Files:**
- Create: `server/src/vendor/ha-bridge/{VENDOR.md,streams.mjs,go2rtc-config.mjs,auth.mjs,watchdog.mjs}`, `server/scripts/sync-upstream.sh`, `server/test/sdk-contract.test.mjs`

**Interfaces:**
- Produces (vendored, unchanged upstream signatures):
  - `streamClientFor(sn, cfg) → Promise<EufyMega>` and `closeStreamClients()` — `cfg` needs `email, password, country, session`.
  - `writeGo2rtcConfig(cfg, devices) → Promise<string[]>` — `cfg` needs `selfHost, port, go2rtcConfig`; `devices` entries need `sn` and `stream` (`/stream/<sn>`). Writes `api :1984, rtsp :8554, webrtc :8555`.
  - `createAuth(ctx) → { authStatus, applyLogin, maybeRecoverSession, onSessionExpired }` — needs `ctx.eufy`, `ctx.state.flags`, `ctx.completeBoot`, `ctx.broadcast`.
  - `createWatchdog(ctx) → { bumpActivity, stallThresholdMs, watchdogTick }` — needs `ctx.eufy`, `ctx.PUSH_STALL_MS`, `ctx.state.flags`, `ctx.applyLogin`.

- [ ] **Step 1: Copy the files verbatim and record provenance**

```bash
UP=/private/tmp/claude-502/-Users-eliransapir-code-eufy-p2p-rtsp-bridge/1cabb21e-fe8f-4972-bff4-5890dc67c942/scratchpad/ha-eufy-sdk-bridge
# if missing: git clone --depth 1 https://github.com/mega-yfue/ha-eufy-sdk-bridge "$UP"
mkdir -p server/src/vendor/ha-bridge
cp "$UP/streams.mjs" "$UP/go2rtc-config.mjs" server/src/vendor/ha-bridge/
cp "$UP/src/auth.mjs" "$UP/src/watchdog.mjs" server/src/vendor/ha-bridge/
SHA=$(git -C "$UP" rev-parse HEAD)
cat > server/src/vendor/ha-bridge/VENDOR.md <<EOF
# Vendored from mega-yfue/ha-eufy-sdk-bridge

- Upstream: https://github.com/mega-yfue/ha-eufy-sdk-bridge
- Commit: $SHA
- License: Apache-2.0 (see upstream LICENSE). Copyright the ha-eufy-sdk-bridge authors.
- Files (verbatim, DO NOT EDIT — wrap them from ../../*.mjs instead):
  - streams.mjs        ← upstream streams.mjs        (session-per-streaming-camera EufyMega instances)
  - go2rtc-config.mjs  ← upstream go2rtc-config.mjs  (go2rtc.yaml generator)
  - auth.mjs           ← upstream src/auth.mjs        (login / 2FA / captcha / re-auth state machine)
  - watchdog.mjs       ← upstream src/watchdog.mjs    (poll + push liveness watchdog)

Update with: \`server/scripts/sync-upstream.sh\` (prints a diff per file; review, copy, bump the SHA here).
EOF
```

- [ ] **Step 2: Write `server/scripts/sync-upstream.sh`**

```bash
#!/usr/bin/env bash
# Diff our vendored ha-eufy-sdk-bridge modules against upstream HEAD so updates are a reviewed merge.
# Usage: server/scripts/sync-upstream.sh [--apply]
set -euo pipefail
HERE=$(cd "$(dirname "$0")/.." && pwd)
VEND="$HERE/src/vendor/ha-bridge"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
git clone -q --depth 1 https://github.com/mega-yfue/ha-eufy-sdk-bridge "$TMP/up"
declare -A MAP=( [streams.mjs]=streams.mjs [go2rtc-config.mjs]=go2rtc-config.mjs [auth.mjs]=src/auth.mjs [watchdog.mjs]=src/watchdog.mjs )
changed=0
for ours in "${!MAP[@]}"; do
  theirs="$TMP/up/${MAP[$ours]}"
  if ! diff -u "$VEND/$ours" "$theirs" >"$TMP/$ours.diff"; then
    changed=1
    echo "== $ours differs from upstream ${MAP[$ours]} =="
    cat "$TMP/$ours.diff"
    [[ "${1:-}" == "--apply" ]] && cp "$theirs" "$VEND/$ours" && echo "applied $ours"
  fi
done
echo "upstream HEAD: $(git -C "$TMP/up" rev-parse HEAD)  (recorded: $(grep -o 'Commit: .*' "$VEND/VENDOR.md"))"
[[ $changed -eq 0 ]] && echo "vendored files are up to date"
exit 0
```
Run: `chmod +x server/scripts/sync-upstream.sh && server/scripts/sync-upstream.sh`
Expected: "vendored files are up to date" and the HEAD equals the recorded SHA.

- [ ] **Step 3: Write the SDK contract test**

`server/test/sdk-contract.test.mjs` — pins the SDK surface we and the vendored modules rely on, so a version bump that removes something fails here, not in production:
```js
import { test } from "node:test";
import assert from "node:assert/strict";
import * as sdk from "@mega-yfue/eufy-sdk";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const version = require("@mega-yfue/eufy-sdk/package.json").version;

test("pinned sdk version", () => assert.equal(version, "0.1.1"));

test("exports used by the bridge exist", () => {
  for (const name of ["EufyMega", "FileSessionStore", "LoginStatus", "ConsoleLogger", "extractParamSets", "codedGeometry"])
    assert.equal(typeof sdk[name], name === "LoginStatus" ? "object" : "function", name);
  assert.ok(sdk.LoginStatus.Ok && sdk.LoginStatus.Captcha && sdk.LoginStatus.TwoFactor);
});

test("EufyMega instance methods used by the bridge", () => {
  const eufy = new sdk.EufyMega({ email: "x@y.z", password: "p", countryCode: "US", autoRealtime: false });
  for (const m of ["login", "solveCaptcha", "submitVerifyCode", "getDevices", "getDevice", "setProperty", "getP2pSessions", "disconnect", "setPollInterval", "on"])
    assert.equal(typeof eufy[m], "function", m);
  assert.equal(typeof eufy.pollIntervalMs, "number");
  // Internal escape hatch used by pins.mjs for the dual-view raw command (no public capability yet).
  assert.equal(typeof eufy.commandSinkFor, "function", "commandSinkFor (private in TS, reachable in JS)");
});

test("annex-b helpers behave", () => {
  // SPS(7) + PPS(8) + IDR(5) H.264 access unit with start codes.
  const au = Buffer.from([0,0,0,1,0x67,0x42,0xc0,0x1e,0xda,0x02,0x80,0xf6,0x80,0x6d,0x0a,0x13,0x50, 0,0,0,1,0x68,0xce,0x38,0x80, 0,0,0,1,0x65,0x88,0x84,0x00]);
  const sets = sdk.extractParamSets(au);
  assert.ok(sets, "param sets found");
  assert.equal(sets.codec, "h264");
  assert.equal(sdk.extractParamSets(Buffer.from([0,0,0,1,0x41,0x9a,0x00])), undefined, "delta frame → no sets");
});
```
Run: `cd server && npm test`
Expected: all pass. If `codedGeometry`/`extractParamSets` are not exported from the package root, check `node -e "import('@mega-yfue/eufy-sdk').then(m=>console.log(Object.keys(m).filter(k=>/Param|Geometry/.test(k))))"` and adjust the import name in this test AND note it in `sdk-adapter.mjs` (Task 3).

- [ ] **Step 4: Commit**

```bash
git add server/src/vendor server/scripts/sync-upstream.sh server/test/sdk-contract.test.mjs
git commit -m "feat(server): vendor ha-eufy-sdk-bridge modules, sync script, sdk contract test"
```

---

### Task 3: State + SDK adapter

**Files:**
- Create: `server/src/state.mjs`, `server/src/sdk-adapter.mjs`

**Interfaces:**
- Produces `createState() → { flags: {ready, sessionLost, lastLogin, booting, recovering, lastActivity, pushConnected, pushSince, go2rtcProc}, timers: {watchdog, stream}, slots: Map<sn, Slot>, blocked: Map<sn, string>, streaming: Set<sn> }`
  - `Slot = { sn, feed, client, lastBytesAt, startedAt, firstFailureAt, failures, backoffIdx, restartTimer, consumers: Set<res>, codec, width, height, lastKeyChunk, stalls }`
- Produces `createSdk({ cfg, DEBUG }) → { eufy, sdk }` where `sdk` = `{ LoginStatus, extractParamSets, codedGeometry, openFeed(sn), sendSetPayload(client, sn, cmd, payload), sessionPeerHost(client, stationSn), closeSession(client, stationSn), deviceManifest(dev), isBattery(dev), streamClientFor(sn), closeStreamClients() }`

- [ ] **Step 1: Write `server/src/state.mjs`**

```js
// One mutable runtime-state object shared through ctx (mirrors upstream's state.mjs so the vendored
// auth/watchdog modules find the flags they expect). Our additions: per-camera stream slots + LAN blocks.
export function createState() {
  return {
    flags: {
      ready: false,
      sessionLost: false,
      lastLogin: undefined,
      booting: false,
      recovering: false,
      lastActivity: Date.now(),
      pushConnected: false,
      pushSince: Date.now(),
      go2rtcProc: undefined,
    },
    timers: { watchdog: null, stream: null },
    streaming: new Set(), // sns with a live feed right now (upstream name; read by vendored code paths)
    slots: new Map(), // sn -> Slot (stream-manager.mjs)
    blocked: new Map(), // sn -> reason (lan-guard.mjs), e.g. "wan-path 203.0.113.9"
  };
}

export function newSlot(sn) {
  return {
    sn,
    feed: undefined,
    client: undefined,
    lastBytesAt: 0,
    startedAt: 0,
    firstFailureAt: 0,
    failures: 0,
    backoffIdx: 0,
    restartTimer: null,
    consumers: new Set(),
    codec: undefined,
    width: undefined,
    height: undefined,
    lastKeyChunk: undefined,
    stalls: 0,
  };
}
```

- [ ] **Step 2: Write `server/src/sdk-adapter.mjs`**

```js
// The ONLY non-vendored file that imports @mega-yfue/eufy-sdk. Everything the bridge needs from the SDK
// is re-exposed here with stable names, so an SDK API change is a one-file edit (+ the contract test).
import { EufyMega, FileSessionStore, LoginStatus, ConsoleLogger, extractParamSets, codedGeometry } from "@mega-yfue/eufy-sdk";
import { streamClientFor as vendoredStreamClientFor, closeStreamClients } from "./vendor/ha-bridge/streams.mjs";

export function createSdk({ cfg, DEBUG }) {
  const eufy = new EufyMega({
    email: cfg.email,
    password: cfg.password,
    countryCode: cfg.country,
    store: new FileSessionStore(cfg.session),
    pollMs: cfg.pollMs,
    prewarmEvents: [], // no speculative P2P warm-ups on push events (Phase 1 streams are always warm anyway)
    localAddresses: Object.keys(cfg.lan.stationAddresses).length ? cfg.lan.stationAddresses : undefined,
    logger: DEBUG ? new ConsoleLogger("info") : undefined,
  });

  const sdk = {
    LoginStatus,
    extractParamSets,
    codedGeometry,

    /** Dedicated per-camera EufyMega (vendored workaround: one P2P session per streaming camera). */
    streamClientFor: (sn) => vendoredStreamClientFor(sn, cfg),
    closeStreamClients,

    /** Open the raw Annex-B Readable for a camera on the given client. */
    async openFeed(client, sn) {
      const cam = (await client.getDevice(sn)).camera?.();
      if (!cam?.openReadable) throw new Error(`${sn}: no live video (not a camera or openReadable unavailable)`);
      return cam.openReadable();
    },

    /** Manifest + power source for the /api shape. */
    async describe(sn) {
      const dev = await eufy.getDevice(sn);
      const m = dev.describe();
      return {
        sn: m.sn,
        name: m.name,
        model: m.model || m.modelName,
        modelName: m.modelName,
        isCamera: m.capabilities.includes("camera") || m.capabilities.includes("video"),
        battery: dev.has("battery"),
      };
    },

    /**
     * Raw SET_PAYLOAD (1350) sub-command on a device channel — the escape hatch for settings the SDK has
     * no capability for yet (dual-lens view mode 6243/2700). Uses the SDK's private commandSinkFor; the
     * contract test asserts it exists. Replace with a public capability once upstream ships one.
     */
    async sendSetPayload(client, sn, cmd, payload) {
      const sink = client.commandSinkFor(sn);
      await sink.dispatch({ kind: "set-payload", cmd, payload, mValue3: 0 });
    },

    /** The IP the P2P session for `stationSn` is talking to, or undefined if unknown/not connected. */
    sessionPeerHost(client, stationSn) {
      const s = client.getP2pSessions().get(stationSn);
      return s?.connectAddress?.host; // private field in TS, reachable at runtime; contract test guards
    },

    async closeSession(client, stationSn) {
      const s = client.getP2pSessions().get(stationSn);
      if (s) await s.close();
    },
  };
  return { eufy, sdk };
}
```

- [ ] **Step 3: Extend the contract test for the session field**

Append to `server/test/sdk-contract.test.mjs`:
```js
test("P2PSession exposes connectAddress and close (used by lan-guard)", async () => {
  const src = await import("node:fs").then((fs) =>
    fs.readFileSync(require.resolve("@mega-yfue/eufy-sdk").replace(/index\.js$/, "transport/p2p/p2p-session.js"), "utf8"),
  );
  assert.match(src, /this\.connectAddress = /, "connectAddress field assigned on connect");
  assert.match(src, /async close\(\)/, "close() method");
});
```
Run: `cd server && npm test` → passes. (If the dist path differs, find it with `find server/node_modules/@mega-yfue/eufy-sdk/dist -name 'p2p-session.js'` and fix the path.)

- [ ] **Step 4: Commit**

```bash
git add server/src/state.mjs server/src/sdk-adapter.mjs server/test/sdk-contract.test.mjs
git commit -m "feat(server): runtime state and sdk adapter"
```

---

### Task 4: Camera registry (`cameras.mjs`)

**Files:**
- Create: `server/src/cameras.mjs`, `server/test/cameras.test.mjs`

**Interfaces:**
- Consumes: `ctx.sdk.describe(sn)`, `ctx.eufy.getDevices()`, `ctx.cfg.cameras/defaults`, `ctx.state.{slots,blocked,streaming}`, `ctx.streamStatus(sn)` (Task 6; optional-chained).
- Produces: `refreshCameras() → Promise<Camera[]>`, `listCameras() → Camera[]` (cached), `getCamera(sn) → Camera|undefined`, `apiShape(cam) → object`.
  - `Camera = { sn, name, model, modelName, battery, enabled, quality, dualView, isDual }`
  - Dual-lens models: `T8214` (Doorbell E340), `T8425` (Floodlight E340), `T8213` / `T8203` (older dual doorbells), `T8170`/`T8171` (S340 SoloCam), `T8416` (Indoor S350). `viewModeCmd`: 6243 for T8214/T8425/T8170/T8171/T8416, 2700 for T8213/T8203.

- [ ] **Step 1: Write the failing test**

`server/test/cameras.test.mjs`:
```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { createCameras, DUAL_MODELS } from "../src/cameras.mjs";
import { createState } from "../src/state.mjs";

function ctxWith(devices, cfgCams = {}, defaults = { quality: "Full HD (1080P)", dualView: "split" }) {
  const byName = Object.fromEntries(devices.map((d) => [d.sn, d]));
  return {
    cfg: { cameras: cfgCams, defaults, port: 3000 },
    state: createState(),
    eufy: { getDevices: async () => devices.map((d) => ({ sn: d.sn })) },
    sdk: { describe: async (sn) => byName[sn] },
    streamStatus: () => ({ streaming: false, stalls: 0, codec: undefined, width: undefined, height: undefined }),
  };
}

const wired = { sn: "T8410A", name: "Garage", model: "T8410", modelName: "Indoor Cam", isCamera: true, battery: false };
const batt = { sn: "T8113B", name: "Yard", model: "T8113", modelName: "eufyCam 2C", isCamera: true, battery: true };
const door = { sn: "T8214C", name: "Door", model: "T8214", modelName: "Doorbell E340", isCamera: true, battery: false };
const hub = { sn: "T8010D", name: "HomeBase", model: "T8010", modelName: "HomeBase 2", isCamera: false, battery: false };

test("wired cameras enabled by default, battery excluded, non-cameras dropped", async () => {
  const c = createCameras(ctxWith([wired, batt, door, hub]));
  const cams = await c.refreshCameras();
  assert.deepEqual(cams.map((x) => [x.sn, x.enabled]), [["T8410A", true], ["T8113B", false], ["T8214C", true]]);
  assert.equal(c.getCamera("T8010D"), undefined);
});

test("config overrides name/enabled/quality/dual view; dual models flagged with command id", async () => {
  const c = createCameras(ctxWith([wired, batt, door], {
    T8113B: { enabled: true, quality: "HD (720P)" },
    T8214C: { name: "Front", dualView: "pip-br" },
  }));
  await c.refreshCameras();
  assert.equal(c.getCamera("T8113B").enabled, true);
  assert.equal(c.getCamera("T8113B").quality, "HD (720P)");
  assert.equal(c.getCamera("T8410A").quality, "Full HD (1080P)");
  assert.equal(c.getCamera("T8214C").name, "Front");
  assert.equal(c.getCamera("T8214C").isDual, true);
  assert.equal(c.getCamera("T8214C").viewModeCmd, 6243);
  assert.equal(c.getCamera("T8214C").dualView, "pip-br");
  assert.equal(c.getCamera("T8410A").isDual, false);
});

test("apiShape merges stream status and rtsp url", async () => {
  const ctx = ctxWith([wired]);
  ctx.streamStatus = () => ({ streaming: true, stalls: 2, codec: "h264", width: 1920, height: 1080 });
  ctx.state.blocked.set("T8410A", "wan-path 203.0.113.9");
  const c = createCameras(ctx);
  await c.refreshCameras();
  const s = c.apiShape(c.getCamera("T8410A"), "192.168.1.10");
  assert.deepEqual(s, {
    sn: "T8410A", name: "Garage", model: "T8410", modelName: "Indoor Cam", enabled: true, powered: true,
    dual: false, dualView: null, quality: "Full HD (1080P)", codec: "h264", width: 1920, height: 1080,
    streaming: true, stalls: 2, blocked: "wan-path 203.0.113.9", rtsp: "rtsp://192.168.1.10:8554/T8410A",
    stream: "/stream/T8410A",
  });
});

test("DUAL_MODELS map", () => {
  assert.equal(DUAL_MODELS.T8425, 6243);
  assert.equal(DUAL_MODELS.T8213, 2700);
});
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && node --test test/cameras.test.mjs`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `server/src/cameras.mjs`**

```js
// Camera registry: SDK device list ∩ config → the set of cameras this bridge serves, plus the /api shape.
// Phase 1 rule: wired cameras are in unless `enabled: false`; battery cameras are out unless `enabled: true`.

/** Dual-lens models → the SET_PAYLOAD sub-command that sets their composed view (from bropat's client). */
export const DUAL_MODELS = {
  T8214: 6243, // Battery Doorbell E340
  T8425: 6243, // Floodlight Cam E340
  T8170: 6243, // SoloCam S340
  T8171: 6243, // SoloCam S340 variant
  T8416: 6243, // Indoor Cam S350
  T8213: 2700, // Battery Doorbell 2K Dual
  T8203: 2700, // Wired Doorbell Dual
};

/** Config names → wire `video_type` values (bropat DualCamWatchViewMode states). */
export const DUAL_VIEW_VALUES = { "pip-tl": 2, "pip-tr": 3, "pip-bl": 4, "pip-br": 5, split: 12, single: 0 };

export function createCameras(ctx) {
  let cache = [];

  async function refreshCameras() {
    const devices = await ctx.eufy.getDevices();
    const out = [];
    for (const d of devices) {
      let m;
      try {
        m = await ctx.sdk.describe(d.sn);
      } catch (e) {
        console.error(`[bridge] ${d.sn}: describe failed: ${e?.message ?? e}`);
        continue;
      }
      if (!m.isCamera) continue;
      const c = ctx.cfg.cameras[m.sn] ?? {};
      const enabled = c.enabled ?? !m.battery;
      const modelKey = String(m.model ?? "").slice(0, 5).toUpperCase();
      const isDual = modelKey in DUAL_MODELS;
      if (!enabled && !(m.sn in ctx.cfg.cameras)) console.log(`[bridge] ${m.sn} (${m.name}) is battery-powered — skipped in phase 1 (set cameras.${m.sn}.enabled: true to force)`);
      out.push({
        sn: m.sn,
        name: c.name ?? m.name,
        model: m.model,
        modelName: m.modelName,
        battery: m.battery,
        enabled,
        quality: c.quality ?? ctx.cfg.defaults.quality ?? null,
        isDual,
        viewModeCmd: isDual ? DUAL_MODELS[modelKey] : null,
        dualView: isDual ? (c.dualView ?? ctx.cfg.defaults.dualView) : null,
      });
    }
    cache = out;
    return out;
  }

  const listCameras = () => cache;
  const getCamera = (sn) => cache.find((c) => c.sn === sn);

  /** What GET /api/cameras returns per camera. `host` is the address clients should dial for RTSP. */
  function apiShape(cam, host) {
    const st = ctx.streamStatus?.(cam.sn) ?? {};
    return {
      sn: cam.sn,
      name: cam.name,
      model: cam.model,
      modelName: cam.modelName,
      enabled: cam.enabled,
      powered: !cam.battery,
      dual: cam.isDual,
      dualView: cam.dualView,
      quality: cam.quality,
      codec: st.codec,
      width: st.width,
      height: st.height,
      streaming: Boolean(st.streaming),
      stalls: st.stalls ?? 0,
      blocked: ctx.state.blocked.get(cam.sn) ?? null,
      rtsp: `rtsp://${host}:8554/${cam.sn}`,
      stream: `/stream/${cam.sn}`,
    };
  }

  return { refreshCameras, listCameras, getCamera, apiShape };
}
```

- [ ] **Step 4: Run tests**

Run: `cd server && npm test` → all pass.

- [ ] **Step 5: Commit**

```bash
git add server/src/cameras.mjs server/test/cameras.test.mjs
git commit -m "feat(server): camera registry with wired/battery defaults and dual-lens detection"
```

---

### Task 5: Pins (`pins.mjs`) — dual view mode + streaming quality

**Files:**
- Create: `server/src/pins.mjs`, `server/test/pins.test.mjs`

**Interfaces:**
- Consumes: `ctx.getCamera(sn)`, `ctx.sdk.streamClientFor(sn)`, `ctx.sdk.sendSetPayload(client, sn, cmd, payload)`, `ctx.eufy.setProperty(sn, name, value)`, `DUAL_VIEW_VALUES`.
- Produces: `applyPins(sn) → Promise<{ dualView: "set"|"skip"|"error", quality: "set"|"unsupported"|"skip"|"error" }>`, `applyAllPins()`.
- Wire facts: dual view = SET_PAYLOAD sub-cmd `viewModeCmd` with `{ restore: 1, video_type: <value> }`. Streaming quality: the SDK's `streamingQuality` property has **no verified setter** in 0.1.1 (`setProperty` throws) — we try it, and on failure log once that the live-view quality must be set in the eufy app (Camera → Settings → Video → Streaming quality) and rely on the codec sniff/warning in stream-manager.

- [ ] **Step 1: Write the failing test**

`server/test/pins.test.mjs`:
```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { createPins } from "../src/pins.mjs";

function ctxWith({ cam, setPropertyImpl }) {
  const sent = [];
  const props = [];
  return {
    sent, props,
    getCamera: () => cam,
    listCameras: () => [cam],
    sdk: {
      streamClientFor: async () => ({ id: "client" }),
      sendSetPayload: async (client, sn, cmd, payload) => sent.push({ sn, cmd, payload }),
    },
    eufy: { setProperty: async (sn, name, value) => { props.push({ sn, name, value }); return setPropertyImpl?.(); } },
  };
}

test("dual-lens camera gets view mode command; quality set when supported", async () => {
  const ctx = ctxWith({ cam: { sn: "T8214C", isDual: true, viewModeCmd: 6243, dualView: "split", quality: "Full HD (1080P)", enabled: true } });
  const r = await createPins(ctx).applyPins("T8214C");
  assert.deepEqual(ctx.sent, [{ sn: "T8214C", cmd: 6243, payload: { restore: 1, video_type: 12 } }]);
  assert.deepEqual(ctx.props, [{ sn: "T8214C", name: "streamingQuality", value: "Full HD (1080P)" }]);
  assert.deepEqual(r, { dualView: "set", quality: "set" });
});

test("quality setter unsupported → reported, not thrown; single-lens sends no view command", async () => {
  const ctx = ctxWith({
    cam: { sn: "T8410A", isDual: false, viewModeCmd: null, dualView: null, quality: "HD (720P)", enabled: true },
    setPropertyImpl: () => { throw new Error("wire unverified"); },
  });
  const r = await createPins(ctx).applyPins("T8410A");
  assert.deepEqual(ctx.sent, []);
  assert.deepEqual(r, { dualView: "skip", quality: "unsupported" });
});

test("no quality configured → skip; disabled camera → nothing", async () => {
  const ctx = ctxWith({ cam: { sn: "T8410A", isDual: false, quality: null, enabled: false } });
  const r = await createPins(ctx).applyPins("T8410A");
  assert.deepEqual(r, { dualView: "skip", quality: "skip" });
  assert.deepEqual(ctx.props, []);
});
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && node --test test/pins.test.mjs` → FAIL (module not found).

- [ ] **Step 3: Implement `server/src/pins.mjs`**

```js
// Per-camera settings the wall depends on, re-applied at boot and after every recovery:
//  - dual-lens view mode (6243/2700 SET_PAYLOAD {restore:1, video_type}) — one composed stream per device
//  - live-view quality tier — selects H.264 (720p/1080p) vs H.265 (2K) on most models. The SDK 0.1.1 has
//    no verified setter for it; when setProperty throws we say so once and leave it to the eufy app.
import { DUAL_VIEW_VALUES } from "./cameras.mjs";

export function createPins(ctx) {
  const warnedQuality = new Set();

  async function applyPins(sn) {
    const cam = ctx.getCamera(sn);
    const result = { dualView: "skip", quality: "skip" };
    if (!cam || !cam.enabled) return result;

    if (cam.isDual && cam.viewModeCmd && cam.dualView) {
      try {
        const client = await ctx.sdk.streamClientFor(sn); // same session the stream will use
        await ctx.sdk.sendSetPayload(client, sn, cam.viewModeCmd, { restore: 1, video_type: DUAL_VIEW_VALUES[cam.dualView] });
        console.log(`[bridge] ${sn}: dual view pinned to ${cam.dualView} (cmd ${cam.viewModeCmd})`);
        result.dualView = "set";
      } catch (e) {
        console.error(`[bridge] ${sn}: dual view pin failed: ${e?.message ?? e}`);
        result.dualView = "error";
      }
    }

    if (cam.quality) {
      try {
        await ctx.eufy.setProperty(sn, "streamingQuality", cam.quality);
        console.log(`[bridge] ${sn}: streaming quality pinned to ${cam.quality}`);
        result.quality = "set";
      } catch (e) {
        const msg = String(e?.message ?? e);
        if (/unverified|not supported|CapabilityNotSupported/i.test(msg) || e?.name === "CapabilityNotSupportedError") {
          if (!warnedQuality.has(sn))
            console.warn(`[bridge] ${sn}: SDK cannot set streaming quality yet (${msg}) — set "${cam.quality}" in the eufy app: camera → Settings → Video → Streaming quality. The codec check below will tell you if the stream is not H.264.`);
          warnedQuality.add(sn);
          result.quality = "unsupported";
        } else {
          console.error(`[bridge] ${sn}: streaming quality pin failed: ${msg}`);
          result.quality = "error";
        }
      }
    }
    return result;
  }

  async function applyAllPins() {
    const out = {};
    for (const cam of ctx.listCameras()) if (cam.enabled) out[cam.sn] = await applyPins(cam.sn);
    return out;
  }

  return { applyPins, applyAllPins };
}
```

- [ ] **Step 4: Run tests** — `cd server && npm test` → pass.

- [ ] **Step 5: Commit**

```bash
git add server/src/pins.mjs server/test/pins.test.mjs
git commit -m "feat(server): pin dual-lens view mode and streaming quality per camera"
```

---

### Task 6: Stream manager — warm feeds, fan-out, stall watchdog

**Files:**
- Create: `server/src/stream-manager.mjs`, `server/test/stream-manager.test.mjs`

**Interfaces:**
- Consumes: `ctx.sdk.{streamClientFor, openFeed, extractParamSets, codedGeometry}`, `ctx.cfg.stall`, `ctx.state.{slots, streaming}`, `newSlot`, `ctx.attachLanGuard(client, label)` (Task 7, optional-chained), `ctx.isBlocked(sn)`, `ctx.getCamera(sn)`, `ctx.applyPins(sn)`.
- Produces:
  - `ensureWarm(sn) → Promise<void>` — open (or keep) the always-on feed; idempotent.
  - `attachConsumer(sn, res) → () => void` — pipe live Annex-B into an HTTP response (`res.write`); returns detach fn. Primes with `slot.lastKeyChunk`. Drops chunks while `res.writableNeedDrain` and resumes at the next keyframe chunk.
  - `streamStatus(sn) → { streaming, stalls, codec, width, height, lastBytesAgoMs, consumers, failures }`
  - `streamTick(now = Date.now()) → void` — call every 2 s: stall detection, gap disconnect, exit-after-stall.
  - `stopAll() → Promise<void>`
  - `exit` is injectable (`ctx.exit ?? process.exit`) for tests.

- [ ] **Step 1: Write the failing tests**

`server/test/stream-manager.test.mjs`:
```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { PassThrough } from "node:stream";
import { createStreamManager } from "../src/stream-manager.mjs";
import { createState } from "../src/state.mjs";

// SPS+PPS+IDR (keyframe) and a delta-frame chunk, both Annex-B.
const KEY = Buffer.from([0,0,0,1,0x67,0x42,0xc0,0x1e,0xda,0x02,0x80,0xf6,0x80,0x6d,0x0a,0x13,0x50, 0,0,0,1,0x68,0xce,0x38,0x80, 0,0,0,1,0x65,0x88,0x84,0x00]);
const DELTA = Buffer.from([0,0,0,1,0x41,0x9a,0x00,0x11]);

function fakeRes() {
  const chunks = [];
  return { chunks, writableNeedDrain: false, destroyed: false, write(b) { chunks.push(b); return true; }, end() { this.destroyed = true; } };
}

function ctxWith({ feeds, exit }) {
  const state = createState();
  let opens = 0;
  return {
    state,
    cfg: { stall: { stallMs: 12000, gapMs: 45000, exitAfterMs: 300000, backoffMs: [10, 20, 40] } },
    exit: exit ?? (() => {}),
    getCamera: () => ({ enabled: true }),
    isBlocked: () => false,
    applyPins: async () => {},
    sdk: {
      streamClientFor: async () => ({}),
      openFeed: async () => { opens++; return feeds[Math.min(opens, feeds.length) - 1](); },
      extractParamSets: (b) => (b[4] === 0x67 ? { codec: "h264", sps: [b.subarray(4, 17)], pps: [] } : undefined),
      codedGeometry: () => ({ width: 640, height: 480 }),
    },
    opens: () => opens,
  };
}

test("warm feed sniffs codec/geometry, primes late consumer with last keyframe, streams deltas", async () => {
  const feed = new PassThrough();
  const ctx = ctxWith({ feeds: [() => feed] });
  const sm = createStreamManager(ctx);
  await sm.ensureWarm("A");
  feed.write(KEY);
  feed.write(DELTA);
  await new Promise((r) => setImmediate(r));
  const st = sm.streamStatus("A");
  assert.equal(st.streaming, true);
  assert.equal(st.codec, "h264");
  assert.equal(st.width, 640);
  const res = fakeRes();
  sm.attachConsumer("A", res);
  assert.equal(res.chunks[0], KEY, "primed with last keyframe");
  feed.write(DELTA);
  await new Promise((r) => setImmediate(r));
  assert.equal(res.chunks.length, 2);
  assert.equal(ctx.state.streaming.has("A"), true);
});

test("consumer under backpressure drops until next keyframe", async () => {
  const feed = new PassThrough();
  const ctx = ctxWith({ feeds: [() => feed] });
  const sm = createStreamManager(ctx);
  await sm.ensureWarm("A");
  feed.write(KEY);
  await new Promise((r) => setImmediate(r));
  const res = fakeRes();
  sm.attachConsumer("A", res);
  res.writableNeedDrain = true;
  feed.write(DELTA);
  await new Promise((r) => setImmediate(r));
  assert.equal(res.chunks.length, 1, "delta dropped while draining");
  res.writableNeedDrain = false;
  feed.write(DELTA);
  await new Promise((r) => setImmediate(r));
  assert.equal(res.chunks.length, 1, "still waiting for a keyframe");
  feed.write(KEY);
  await new Promise((r) => setImmediate(r));
  assert.equal(res.chunks.length, 2, "resumed at keyframe");
});

test("stall → feed destroyed and reopened with backoff; gap → consumers ended; exit after continuous failure", async () => {
  const f1 = new PassThrough(), f2 = new PassThrough();
  let exited = 0;
  const ctx = ctxWith({ feeds: [() => f1, () => f2], exit: () => exited++ });
  const sm = createStreamManager(ctx);
  await sm.ensureWarm("A");
  const t0 = 1_000_000;
  f1.write(KEY);
  await new Promise((r) => setImmediate(r));
  ctx.state.slots.get("A").lastBytesAt = t0;
  const res = fakeRes();
  sm.attachConsumer("A", res);
  sm.streamTick(t0 + 13_000); // > stallMs
  assert.equal(sm.streamStatus("A").stalls, 1);
  assert.equal(f1.destroyed, true);
  await new Promise((r) => setTimeout(r, 30)); // backoff 10ms → reopen
  assert.equal(ctx.opens(), 2);
  assert.equal(ctx.state.slots.get("A").feed, f2);
  sm.streamTick(t0 + 46_000); // > gapMs with no bytes → consumers dropped
  assert.equal(res.destroyed, true);
  ctx.state.slots.get("A").firstFailureAt = t0;
  sm.streamTick(t0 + 301_000);
  assert.equal(exited, 1, "exit(1) requested after exitAfterMs of failure");
});

test("blocked camera is not warmed", async () => {
  const ctx = ctxWith({ feeds: [() => new PassThrough()] });
  ctx.isBlocked = () => true;
  const sm = createStreamManager(ctx);
  await sm.ensureWarm("A");
  assert.equal(ctx.opens(), 0);
});
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && node --test test/stream-manager.test.mjs` → FAIL (module not found).

- [ ] **Step 3: Implement `server/src/stream-manager.mjs`**

```js
// Always-on media core. One Slot per enabled camera holds the SDK's Annex-B Readable ("feed"), fans its
// chunks out to HTTP consumers (go2rtc), and is watched for stalls. Watchdog thresholds are ported from
// eufy-frigate-bridge (12 s stall / 45 s gap / 300 s exit), which were calibrated over a 19 h run.
import { newSlot } from "./state.mjs";

export function createStreamManager(ctx) {
  const { state, cfg } = ctx;
  const exit = (code) => (ctx.exit ?? process.exit)(code);
  const slotFor = (sn) => state.slots.get(sn) ?? state.slots.set(sn, newSlot(sn)).get(sn);

  function onChunk(slot, chunk) {
    const now = Date.now();
    slot.lastBytesAt = now;
    slot.firstFailureAt = 0;
    slot.failures = 0;
    slot.backoffIdx = 0;
    if (!state.streaming.has(slot.sn)) {
      state.streaming.add(slot.sn);
      console.log(`[bridge] ${slot.sn}: streaming`);
    }
    const sets = ctx.sdk.extractParamSets(chunk); // non-undefined ⇒ this chunk carries SPS/PPS (keyframe AU)
    if (sets) {
      slot.lastKeyChunk = chunk;
      if (slot.codec !== sets.codec) {
        slot.codec = sets.codec;
        const g = ctx.sdk.codedGeometry(sets);
        slot.width = g?.width;
        slot.height = g?.height;
        console.log(`[bridge] ${slot.sn}: codec ${sets.codec} ${slot.width ?? "?"}x${slot.height ?? "?"}`);
        if (sets.codec !== "h264")
          console.warn(`[bridge] ${slot.sn}: stream is ${sets.codec.toUpperCase()} — Raspberry Pi clients cannot decode it. Lower the camera's streaming quality to 1080p/720p in the eufy app.`);
      }
    }
    for (const c of slot.consumers) {
      if (c.destroyed) { slot.consumers.delete(c); continue; }
      if (c.writableNeedDrain) { c._ewbDropping = true; continue; } // backpressure: drop until next keyframe
      if (c._ewbDropping && !sets) continue;
      c._ewbDropping = false;
      c.write(chunk);
    }
  }

  function scheduleReopen(slot, why) {
    if (slot.restartTimer) return;
    const delay = cfg.stall.backoffMs[Math.min(slot.backoffIdx, cfg.stall.backoffMs.length - 1)];
    slot.backoffIdx++;
    console.log(`[bridge] ${slot.sn}: ${why} — reopening in ${delay} ms`);
    slot.restartTimer = setTimeout(() => {
      slot.restartTimer = null;
      void ensureWarm(slot.sn);
    }, delay);
  }

  function closeFeed(slot) {
    const f = slot.feed;
    slot.feed = undefined;
    if (state.streaming.delete(slot.sn)) console.log(`[bridge] ${slot.sn}: stopped`);
    if (f) { f.removeAllListeners(); f.destroy(); }
  }

  /** Open the camera's feed if it is not open. Idempotent; failures schedule a backoff retry. */
  async function ensureWarm(sn) {
    const cam = ctx.getCamera?.(sn);
    if (cam && !cam.enabled) return;
    if (ctx.isBlocked?.(sn)) return;
    const slot = slotFor(sn);
    if (slot.feed || slot.opening) return;
    slot.opening = true;
    try {
      const client = await ctx.sdk.streamClientFor(sn);
      slot.client = client;
      ctx.attachLanGuard?.(client, sn);
      const feed = await ctx.sdk.openFeed(client, sn);
      slot.feed = feed;
      slot.startedAt = Date.now();
      feed.on("data", (chunk) => onChunk(slot, chunk));
      const onEnd = (why) => () => {
        if (slot.feed !== feed) return;
        closeFeed(slot);
        noteFailure(slot);
        scheduleReopen(slot, why);
      };
      feed.on("error", (e) => { console.error(`[bridge] ${sn}: feed error: ${e?.message ?? e}`); onEnd("feed error")(); });
      feed.on("end", onEnd("feed ended"));
      feed.on("close", onEnd("feed closed"));
    } catch (e) {
      console.error(`[bridge] ${sn}: open failed: ${e?.message ?? e}`);
      noteFailure(slot);
      scheduleReopen(slot, "open failed");
    } finally {
      slot.opening = false;
    }
  }

  function noteFailure(slot) {
    slot.failures++;
    if (!slot.firstFailureAt) slot.firstFailureAt = Date.now();
  }

  /** Pipe the live feed into an HTTP response. Returns a detach function. */
  function attachConsumer(sn, res) {
    const slot = slotFor(sn);
    slot.consumers.add(res);
    if (slot.lastKeyChunk) res.write(slot.lastKeyChunk);
    void ensureWarm(sn);
    return () => slot.consumers.delete(res);
  }

  function streamStatus(sn) {
    const s = state.slots.get(sn);
    if (!s) return { streaming: false, stalls: 0, consumers: 0, failures: 0 };
    return {
      streaming: state.streaming.has(sn),
      stalls: s.stalls,
      codec: s.codec,
      width: s.width,
      height: s.height,
      lastBytesAgoMs: s.lastBytesAt ? Date.now() - s.lastBytesAt : null,
      consumers: s.consumers.size,
      failures: s.failures,
    };
  }

  /** Every 2 s: stall → restart; gap → drop consumers so go2rtc reconnects cleanly; long failure → exit. */
  function streamTick(now = Date.now()) {
    for (const slot of state.slots.values()) {
      const silent = slot.lastBytesAt ? now - slot.lastBytesAt : now - (slot.startedAt || now);
      if (slot.feed && silent >= cfg.stall.stallMs) {
        slot.stalls++;
        console.warn(`[bridge] ${slot.sn}: no bytes for ${Math.round(silent / 1000)} s — restarting feed (stall #${slot.stalls})`);
        closeFeed(slot);
        noteFailure(slot);
        scheduleReopen(slot, "stalled");
      }
      if (silent >= cfg.stall.gapMs && slot.consumers.size) {
        console.warn(`[bridge] ${slot.sn}: ${Math.round(silent / 1000)} s gap — disconnecting ${slot.consumers.size} consumer(s)`);
        for (const c of slot.consumers) c.end();
        slot.consumers.clear();
      }
      if (slot.firstFailureAt && now - slot.firstFailureAt >= cfg.stall.exitAfterMs) {
        console.error(`[bridge] ${slot.sn}: failing continuously for ${Math.round((now - slot.firstFailureAt) / 1000)} s — exiting for a clean restart`);
        exit(1);
        return;
      }
    }
  }

  async function stopAll() {
    for (const slot of state.slots.values()) {
      if (slot.restartTimer) clearTimeout(slot.restartTimer);
      slot.restartTimer = null;
      for (const c of slot.consumers) c.end();
      slot.consumers.clear();
      closeFeed(slot);
    }
  }

  return { ensureWarm, attachConsumer, streamStatus, streamTick, stopAll };
}
```

- [ ] **Step 4: Run tests** — `cd server && npm test` → all pass. If the backoff test is flaky on timing, raise the `setTimeout` wait to 60 ms.

- [ ] **Step 5: Commit**

```bash
git add server/src/stream-manager.mjs server/test/stream-manager.test.mjs
git commit -m "feat(server): always-on stream manager with fan-out, backpressure and stall watchdog"
```

---

### Task 7: LAN guard (`lan-guard.mjs`)

**Files:**
- Create: `server/src/lan-guard.mjs`, `server/test/lan-guard.test.mjs`

**Interfaces:**
- Consumes: `ctx.cfg.lan`, `ctx.sdk.{sessionPeerHost, closeSession}`, `ctx.state.blocked`, `ctx.listCameras()` (to map station → cameras; a camera's station serial is not in our Camera shape, so block by station AND by the camera sn passed as `label`).
- Produces: `attachLanGuard(client, label) → void` (subscribe once per client), `isBlocked(sn) → boolean`, `inCidr(ip, cidr) → boolean` (exported for tests), `checkSession(client, stationSn, label) → Promise<"ok"|"blocked"|"unknown">`.

- [ ] **Step 1: Write the failing tests**

`server/test/lan-guard.test.mjs`:
```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { createLanGuard, inCidr } from "../src/lan-guard.mjs";
import { createState } from "../src/state.mjs";

test("inCidr", () => {
  assert.equal(inCidr("192.168.1.77", "192.168.1.0/24"), true);
  assert.equal(inCidr("192.168.2.1", "192.168.1.0/24"), false);
  assert.equal(inCidr("10.9.8.7", "10.0.0.0/8"), true);
  assert.equal(inCidr("203.0.113.9", "10.0.0.0/8"), false);
  assert.equal(inCidr("garbage", "10.0.0.0/8"), false);
});

function ctxWith({ peer, force = true, cidr = "192.168.1.0/24" }) {
  const closed = [];
  return {
    closed,
    cfg: { lan: { cidr, force, stationAddresses: {} } },
    state: createState(),
    sdk: { sessionPeerHost: () => peer, closeSession: async (c, st) => closed.push(st) },
  };
}

test("WAN peer with force → session closed and camera blocked; LAN peer → ok", async () => {
  const ctx = ctxWith({ peer: "203.0.113.9" });
  const g = createLanGuard(ctx);
  const client = new EventEmitter();
  g.attachLanGuard(client, "CAM1");
  client.emit("p2pConnect", "STATION1");
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(ctx.closed, ["STATION1"]);
  assert.equal(g.isBlocked("CAM1"), true);
  assert.match(ctx.state.blocked.get("CAM1"), /wan-path 203\.0\.113\.9/);

  const ctx2 = ctxWith({ peer: "192.168.1.50" });
  const g2 = createLanGuard(ctx2);
  const c2 = new EventEmitter();
  g2.attachLanGuard(c2, "CAM1");
  c2.emit("p2pConnect", "STATION1");
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(ctx2.closed, []);
  assert.equal(g2.isBlocked("CAM1"), false);
});

test("force off → WAN peer only warns; unknown address → unknown, not blocked", async () => {
  const ctx = ctxWith({ peer: "203.0.113.9", force: false });
  const g = createLanGuard(ctx);
  assert.equal(await g.checkSession({}, "S", "CAM1"), "ok");
  const ctx2 = ctxWith({ peer: undefined });
  assert.equal(await createLanGuard(ctx2).checkSession({}, "S", "CAM1"), "unknown");
  assert.equal(ctx2.state.blocked.size, 0);
});

test("a later LAN connect clears the block", async () => {
  let peer = "203.0.113.9";
  const ctx = ctxWith({ peer });
  ctx.sdk.sessionPeerHost = () => peer;
  const g = createLanGuard(ctx);
  await g.checkSession({}, "S", "CAM1");
  assert.equal(g.isBlocked("CAM1"), true);
  peer = "192.168.1.9";
  await g.checkSession({}, "S", "CAM1");
  assert.equal(g.isBlocked("CAM1"), false);
});
```

- [ ] **Step 2: Run to verify failure** — `cd server && node --test test/lan-guard.test.mjs` → FAIL.

- [ ] **Step 3: Implement `server/src/lan-guard.mjs`**

```js
// Force-LAN: the SDK always races a LAN lookup against the PPCS cloud lookup and keeps whichever peer
// answers first. We can't stop the race (no local-only option in 0.1.1 — upstream PR pending), so we
// inspect the winner on every p2pConnect and refuse a WAN/relay peer: close the session, mark the camera
// blocked (visible in /healthz + /api/cameras), and let the stream manager's backoff try again — the
// station usually answers locally on the next attempt.

export function inCidr(ip, cidr) {
  const [net, bitsStr] = cidr.split("/");
  const bits = Number(bitsStr);
  const toInt = (s) => {
    const p = s.split(".").map(Number);
    if (p.length !== 4 || p.some((n) => !Number.isInteger(n) || n < 0 || n > 255)) return null;
    return ((p[0] << 24) | (p[1] << 16) | (p[2] << 8) | p[3]) >>> 0;
  };
  const a = toInt(ip), n = toInt(net);
  if (a == null || n == null || !(bits >= 0 && bits <= 32)) return false;
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
  return (a & mask) === (n & mask);
}

export function createLanGuard(ctx) {
  const { cfg, state } = ctx;
  const attached = new WeakSet();

  /** Decide for one connected session. Returns "ok" | "blocked" | "unknown". */
  async function checkSession(client, stationSn, label) {
    if (!cfg.lan.cidr) return "ok";
    const host = ctx.sdk.sessionPeerHost(client, stationSn);
    if (!host) {
      console.warn(`[bridge] ${label}: P2P connected to ${stationSn} but peer address unknown — cannot verify LAN path`);
      return "unknown";
    }
    if (inCidr(host, cfg.lan.cidr)) {
      if (state.blocked.delete(label)) console.log(`[bridge] ${label}: LAN path restored via ${host} — unblocked`);
      return "ok";
    }
    if (!cfg.lan.force) {
      console.warn(`[bridge] ${label}: P2P peer ${host} is outside ${cfg.lan.cidr} (lan.force=false, allowing)`);
      return "ok";
    }
    const reason = `wan-path ${host}`;
    state.blocked.set(label, reason);
    console.error(`[bridge] ${label}: P2P peer ${host} is outside ${cfg.lan.cidr} — closing session (force-LAN)`);
    try {
      await ctx.sdk.closeSession(client, stationSn);
    } catch (e) {
      console.error(`[bridge] ${label}: close after WAN detect failed: ${e?.message ?? e}`);
    }
    return "blocked";
  }

  /** Subscribe once per EufyMega client (the control client and each per-camera stream client). */
  function attachLanGuard(client, label) {
    if (!cfg.lan.cidr || attached.has(client)) return;
    attached.add(client);
    client.on("p2pConnect", (stationSn) => void checkSession(client, stationSn, label));
  }

  const isBlocked = (sn) => state.blocked.has(sn);

  return { attachLanGuard, checkSession, isBlocked };
}
```

- [ ] **Step 4: Run tests** — `cd server && npm test` → pass.

- [ ] **Step 5: Commit**

```bash
git add server/src/lan-guard.mjs server/test/lan-guard.test.mjs
git commit -m "feat(server): force-LAN guard on P2P session peer address"
```

---

### Task 8: go2rtc supervisor + HTTP surface

**Files:**
- Create: `server/src/go2rtc.mjs`, `server/src/http.mjs`, `server/test/http.test.mjs`

**Interfaces:**
- Consumes: `writeGo2rtcConfig(cfg, devices)` (vendored), `ctx.listCameras()`, `ctx.apiShape(cam, host)`, `ctx.attachConsumer(sn,res)`, `ctx.streamStatus(sn)`, `ctx.authStatus()`, `ctx.applyLogin(result)`, `ctx.eufy.{login, solveCaptcha, submitVerifyCode, getDevice}`, `ctx.state`, `ctx.stallThresholdMs()`.
- Produces: `createGo2rtc(ctx) → { writeGo2rtc(), startGo2rtc(), stopGo2rtc() }` and `createHttpHandler(ctx) → (req,res) => void` with routes:
  - `GET /healthz` → `{ ok, schemaVersion, auth, streaming[], blocked{}, stalled, pushConnected, go2rtc: "running"|"stopped", cameras: n }` (200 always)
  - `GET /api/cameras` → `[apiShape…]` (503 before ready)
  - `GET /stream/<sn>` → `video/H264` chunked Annex-B (404 unknown/disabled, 423 blocked, 503 not ready)
  - `GET /snapshot/<sn>` → JPEG via `cam.snapshotLive()` (502 on failure)
  - `GET /auth/status` → `authStatus()`; `GET /auth/captcha` → PNG image (404 if none); `POST /auth/captcha?code=X`; `POST /auth/tfa?code=123456`; `POST /auth/retry` → re-run `eufy.login()`.

- [ ] **Step 1: Write `server/src/go2rtc.mjs`**

```js
// go2rtc lifecycle: write its yaml from the enabled camera list (vendored generator) and run the binary
// as a child, restarting it with a short delay if it dies. Not fatal when the binary is missing (dev).
import { spawn } from "node:child_process";
import { writeGo2rtcConfig } from "./vendor/ha-bridge/go2rtc-config.mjs";

export function createGo2rtc(ctx) {
  const { cfg, state } = ctx;
  let stopping = false;

  async function writeGo2rtc() {
    const devices = ctx.listCameras().filter((c) => c.enabled).map((c) => ({ sn: c.sn, stream: `/stream/${c.sn}` }));
    const sns = await writeGo2rtcConfig(cfg, devices);
    console.log(`[bridge] go2rtc config written (${sns.length} stream(s)) → ${cfg.go2rtcConfig}`);
    return sns;
  }

  function startGo2rtc() {
    if (state.flags.go2rtcProc || stopping) return;
    const proc = spawn(cfg.go2rtcBin, ["-config", cfg.go2rtcConfig], { stdio: ["ignore", "inherit", "inherit"] });
    state.flags.go2rtcProc = proc;
    proc.on("error", (e) => {
      console.error(`[bridge] go2rtc failed to start (${e.message}) — RTSP unavailable; HTTP /stream still works`);
      state.flags.go2rtcProc = undefined;
    });
    proc.on("exit", (code, sig) => {
      state.flags.go2rtcProc = undefined;
      if (stopping) return;
      console.error(`[bridge] go2rtc exited (${code ?? sig}) — restarting in 3 s`);
      setTimeout(startGo2rtc, 3000);
    });
    console.log(`[bridge] go2rtc started (${cfg.go2rtcBin}) — RTSP on :8554`);
  }

  function stopGo2rtc() {
    stopping = true;
    state.flags.go2rtcProc?.kill();
  }

  return { writeGo2rtc, startGo2rtc, stopGo2rtc };
}
```

- [ ] **Step 2: Write the failing HTTP tests**

`server/test/http.test.mjs`:
```js
import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { createHttpHandler } from "../src/http.mjs";
import { createState } from "../src/state.mjs";

function ctxWith(over = {}) {
  const state = createState();
  state.flags.ready = true;
  const cams = [{ sn: "A", enabled: true }, { sn: "B", enabled: false }];
  return {
    cfg: { port: 3000 },
    state,
    SCHEMA_VERSION: 1,
    authStatus: () => ({ state: "ok" }),
    stallThresholdMs: () => 60_000,
    listCameras: () => cams,
    getCamera: (sn) => cams.find((c) => c.sn === sn),
    apiShape: (c, host) => ({ sn: c.sn, rtsp: `rtsp://${host}:8554/${c.sn}` }),
    streamStatus: () => ({ streaming: true, stalls: 0 }),
    attachConsumer: (sn, res) => { res.write(Buffer.from([0, 0, 0, 1, 0x67])); return () => {}; },
    isBlocked: (sn) => state.blocked.has(sn),
    eufy: {},
    ...over,
  };
}

async function withServer(ctx, fn) {
  const srv = http.createServer(createHttpHandler(ctx));
  await new Promise((r) => srv.listen(0, "127.0.0.1", r));
  const base = `http://127.0.0.1:${srv.address().port}`;
  try { await fn(base); } finally { srv.close(); }
}

test("healthz and api/cameras", async () => {
  await withServer(ctxWith(), async (base) => {
    const h = await (await fetch(`${base}/healthz`)).json();
    assert.equal(h.ok, true);
    assert.equal(h.auth.state, "ok");
    assert.equal(h.cameras, 2);
    const cams = await (await fetch(`${base}/api/cameras`)).json();
    assert.deepEqual(cams.map((c) => c.sn), ["A", "B"]);
    assert.match(cams[0].rtsp, /^rtsp:\/\/127\.0\.0\.1:8554\/A$/);
  });
});

test("stream: 200 for enabled, 404 disabled/unknown, 423 blocked, 503 not ready", async () => {
  const ctx = ctxWith();
  await withServer(ctx, async (base) => {
    const r = await fetch(`${base}/stream/A`);
    assert.equal(r.status, 200);
    assert.equal(r.headers.get("content-type"), "video/H264");
    const reader = r.body.getReader();
    const { value } = await reader.read();
    assert.deepEqual([...value], [0, 0, 0, 1, 0x67]);
    await reader.cancel();
    assert.equal((await fetch(`${base}/stream/B`)).status, 404);
    assert.equal((await fetch(`${base}/stream/ZZ`)).status, 404);
    ctx.state.blocked.set("A", "wan-path 1.2.3.4");
    assert.equal((await fetch(`${base}/stream/A`)).status, 423);
    ctx.state.flags.ready = false;
    assert.equal((await fetch(`${base}/stream/A`)).status, 503);
  });
});

test("auth endpoints drive login", async () => {
  const calls = [];
  const ctx = ctxWith({
    authStatus: () => ({ state: "require_2fa", method: "email" }),
    applyLogin: async (r) => calls.push(r),
    eufy: {
      submitVerifyCode: async (code) => ({ status: "ok", code }),
      solveCaptcha: async (ans) => ({ status: "ok", ans }),
      login: async () => ({ status: "ok" }),
    },
  });
  ctx.state.flags.lastLogin = { status: "captcha", image: "data:image/png;base64,aGk=" };
  await withServer(ctx, async (base) => {
    assert.equal((await (await fetch(`${base}/auth/status`)).json()).state, "require_2fa");
    const img = await fetch(`${base}/auth/captcha`);
    assert.equal(img.headers.get("content-type"), "image/png");
    assert.equal(Buffer.from(await img.arrayBuffer()).toString(), "hi");
    assert.equal((await fetch(`${base}/auth/tfa?code=123456`, { method: "POST" })).status, 200);
    assert.equal((await fetch(`${base}/auth/captcha?code=AB3D`, { method: "POST" })).status, 200);
    assert.equal((await fetch(`${base}/auth/retry`, { method: "POST" })).status, 200);
    assert.deepEqual(calls.map((c) => c.code ?? c.ans ?? c.status), ["123456", "AB3D", "ok"]);
    assert.equal((await fetch(`${base}/auth/tfa`, { method: "POST" })).status, 400);
  });
});
```

- [ ] **Step 3: Run to verify failure** — `cd server && node --test test/http.test.mjs` → FAIL.

- [ ] **Step 4: Implement `server/src/http.mjs`**

```js
// HTTP surface. Video is plain chunked HTTP (go2rtc pulls it); everything else is small JSON for curl,
// the display clients (/api/cameras) and monitoring (/healthz). First-run auth is driven with curl.

function json(res, code, body) {
  const s = JSON.stringify(body);
  res.writeHead(code, { "content-type": "application/json", "content-length": Buffer.byteLength(s) });
  res.end(s);
}

export function createHttpHandler(ctx) {
  const { state } = ctx;
  const { flags } = state;

  async function auth(req, res, url, kind) {
    if (kind === "status") return json(res, 200, ctx.authStatus());
    if (kind === "captcha" && req.method === "GET") {
      const img = flags.lastLogin?.image;
      if (!img) return json(res, 404, { error: "no captcha pending" });
      const m = /^data:(image\/\w+);base64,(.*)$/.exec(img);
      if (!m) return json(res, 500, { error: "unexpected captcha image format" });
      const buf = Buffer.from(m[2], "base64");
      res.writeHead(200, { "content-type": m[1], "content-length": buf.length });
      return res.end(buf);
    }
    if (req.method !== "POST") return json(res, 405, { error: "POST required" });
    const code = url.searchParams.get("code");
    try {
      if (kind === "tfa") {
        if (!/^\d{6}$/.test(code ?? "")) return json(res, 400, { error: "code must be 6 digits: POST /auth/tfa?code=123456" });
        await ctx.applyLogin(await ctx.eufy.submitVerifyCode(code));
      } else if (kind === "captcha") {
        if (!code) return json(res, 400, { error: "POST /auth/captcha?code=<answer>" });
        await ctx.applyLogin(await ctx.eufy.solveCaptcha(code));
      } else if (kind === "retry") {
        await ctx.applyLogin(await ctx.eufy.login());
      } else return json(res, 404, { error: "not found" });
      return json(res, 200, ctx.authStatus());
    } catch (e) {
      return json(res, 502, { error: String(e?.message ?? e), auth: ctx.authStatus() });
    }
  }

  return async function handle(req, res) {
    const url = new URL(req.url, `http://${req.headers.host ?? "localhost"}`);
    const [, kind, arg] = url.pathname.split("/");
    const host = (req.headers.host ?? "127.0.0.1").replace(/:\d+$/, "");

    if (url.pathname === "/healthz") {
      const idle = Date.now() - flags.lastActivity;
      return json(res, 200, {
        ok: true,
        schemaVersion: ctx.SCHEMA_VERSION,
        auth: ctx.authStatus(),
        sessionLost: flags.sessionLost,
        streaming: [...state.streaming],
        blocked: Object.fromEntries(state.blocked),
        cameras: ctx.listCameras().length,
        stalls: Object.fromEntries([...state.slots.values()].map((s) => [s.sn, s.stalls])),
        stalled: flags.ready && idle >= ctx.stallThresholdMs(),
        pushConnected: flags.pushConnected,
        go2rtc: flags.go2rtcProc ? "running" : "stopped",
      });
    }
    if (kind === "auth") return auth(req, res, url, arg);
    if (!flags.ready) return json(res, 503, { error: "not authenticated", auth: ctx.authStatus() });

    if (url.pathname === "/api/cameras") return json(res, 200, ctx.listCameras().map((c) => ctx.apiShape(c, host)));

    if (kind === "stream" && arg) {
      const cam = ctx.getCamera(arg);
      if (!cam || !cam.enabled) return json(res, 404, { error: "unknown or disabled camera" });
      if (ctx.isBlocked(arg)) return json(res, 423, { error: `blocked: ${state.blocked.get(arg)}` });
      res.writeHead(200, { "content-type": "video/H264", "cache-control": "no-cache", connection: "close" });
      const detach = ctx.attachConsumer(arg, res);
      req.on("close", detach);
      res.on("close", detach);
      return;
    }

    if (kind === "snapshot" && arg) {
      try {
        const cam = (await ctx.eufy.getDevice(arg)).camera?.();
        if (!cam?.snapshotLive) return json(res, 404, { error: "no camera on this device" });
        const { jpeg } = await cam.snapshotLive();
        res.writeHead(200, { "content-type": "image/jpeg", "content-length": jpeg.length });
        return res.end(jpeg);
      } catch (e) {
        return json(res, 502, { error: String(e?.message ?? e) });
      }
    }

    return json(res, 404, { error: "not found" });
  };
}
```

- [ ] **Step 5: Run tests** — `cd server && npm test` → pass.

- [ ] **Step 6: Commit**

```bash
git add server/src/go2rtc.mjs server/src/http.mjs server/test/http.test.mjs
git commit -m "feat(server): go2rtc supervisor and HTTP surface (stream, api, healthz, auth)"
```

---

### Task 9: Wiring (`server.mjs`) + boot sequence

**Files:**
- Create: `server/server.mjs`

**Interfaces:**
- Consumes everything above. Provides `ctx.completeBoot()` and `ctx.broadcast()` for the vendored auth module, `ctx.PUSH_STALL_MS`, `ctx.SCHEMA_VERSION`.
- Boot order after `LoginStatus.Ok`: `refreshCameras` → `attachLanGuard(eufy, "control")` → `applyAllPins` → `writeGo2rtc` → `startGo2rtc` → `ensureWarm` for each enabled camera → arm timers (`streamTick` every 2 s, `watchdogTick` every 2 min) → `ready`.

- [ ] **Step 1: Write `server/server.mjs`**

```js
// eufy-wall-bridge — wiring only. See docs/superpowers/specs/2026-09-18-eufy-wall-design.md.
//   HTTP :3000/stream/<sn>   Annex-B H.264 (go2rtc pulls this)     rtsp://host:8554/<sn>  (go2rtc)
//   HTTP :3000/api/cameras   camera list for display clients       /healthz  /auth/*
import http from "node:http";
import fs from "node:fs";
import { loadConfig } from "./src/config.mjs";
import { createState } from "./src/state.mjs";
import { createSdk } from "./src/sdk-adapter.mjs";
import { createCameras } from "./src/cameras.mjs";
import { createPins } from "./src/pins.mjs";
import { createStreamManager } from "./src/stream-manager.mjs";
import { createLanGuard } from "./src/lan-guard.mjs";
import { createGo2rtc } from "./src/go2rtc.mjs";
import { createHttpHandler } from "./src/http.mjs";
import { createAuth } from "./src/vendor/ha-bridge/auth.mjs";
import { createWatchdog } from "./src/vendor/ha-bridge/watchdog.mjs";

const { cfg, DEBUG } = loadConfig();
fs.mkdirSync(cfg.dataDir, { recursive: true });

const state = createState();
const { eufy, sdk } = createSdk({ cfg, DEBUG });
const ctx = { cfg, DEBUG, eufy, sdk, state, SCHEMA_VERSION: 1, PUSH_STALL_MS: 15 * 60_000 };

// Vendored auth/watchdog broadcast auth changes to "clients"; we have none in phase 1 → log.
ctx.broadcast = (evt) => console.log(`[bridge] event ${JSON.stringify(evt)}`);

Object.assign(ctx, createCameras(ctx), createPins(ctx), createLanGuard(ctx), createStreamManager(ctx), createGo2rtc(ctx), createAuth(ctx), createWatchdog(ctx));

/** Runs once after the first successful login (re-auth calls it again and it returns immediately). */
ctx.completeBoot = async function completeBoot() {
  const { flags, timers } = state;
  if (flags.ready || flags.booting) return;
  flags.booting = true;
  try {
    eufy.on("deviceState", ctx.bumpActivity);
    const cams = await ctx.refreshCameras();
    const enabled = cams.filter((c) => c.enabled);
    console.log(`[bridge] cameras: ${cams.map((c) => `${c.sn}(${c.name}${c.enabled ? "" : ", off"}${c.isDual ? ", dual" : ""})`).join(", ")}`);
    ctx.attachLanGuard(eufy, "control");
    await ctx.applyAllPins();
    await ctx.writeGo2rtc();
    ctx.startGo2rtc();
    flags.ready = true;
    flags.lastActivity = Date.now();
    for (const c of enabled) void ctx.ensureWarm(c.sn);
    timers.stream ??= setInterval(() => ctx.streamTick(), 2000);
    timers.watchdog ??= setInterval(() => void ctx.watchdogTick(), 2 * 60_000);
    console.log(`[bridge] ready — ${enabled.length} always-on camera(s); RTSP at rtsp://<this-host>:8554/<sn>`);
  } finally {
    flags.booting = false;
  }
};

eufy.on("error", (e) => {
  console.error(`[bridge] sdk error: ${e?.message ?? e}`);
  if (e?.name === "SessionExpiredError") ctx.maybeRecoverSession();
});
eufy.on("pushConnect", () => { state.flags.pushConnected = true; state.flags.pushSince = Date.now(); });
eufy.on("pushDisconnect", () => { state.flags.pushConnected = false; state.flags.pushSince = Date.now(); });
// After a re-login (kicked session / watchdog recovery) the cameras may need their pins again.
eufy.on("p2pConnect", () => { if (state.flags.ready) void ctx.applyAllPins().catch(() => {}); });

const server = http.createServer(createHttpHandler(ctx));

async function main() {
  server.listen(cfg.port, cfg.host, () => console.log(`[bridge] listening on ${cfg.host}:${cfg.port}`));
  try {
    await ctx.applyLogin(await eufy.login());
  } catch (e) {
    console.error(`[bridge] login failed: ${e?.message ?? e} — POST /auth/retry to try again`);
  }
  if (!state.flags.ready) {
    const a = ctx.authStatus();
    console.log(`[bridge] auth required: ${a.state} — see docs/runbook-server.md (curl /auth/status, /auth/captcha, /auth/tfa?code=…)`);
  }
}

async function shutdown() {
  for (const t of Object.values(state.timers)) if (t) clearInterval(t);
  ctx.stopGo2rtc();
  await ctx.stopAll();
  await sdk.closeStreamClients();
  await eufy.disconnect?.().catch(() => {});
  process.exit(0);
}
process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
process.on("unhandledRejection", (e) => console.error(`[bridge] unhandled rejection: ${e?.stack ?? e}`));

main().catch((e) => { console.error("[bridge] fatal:", e); process.exit(1); });
```

- [ ] **Step 2: Smoke-run without credentials**

Run: `cd server && node server.mjs`
Expected: exits with `eufy email/password are required …` (from config). Then `EUFY_EMAIL=x EUFY_PASSWORD=y node server.mjs` → logs `listening on 0.0.0.0:3000`, login fails (bad creds) with `auth required: pending`; `curl -s localhost:3000/healthz` returns JSON with `"auth":{"state":"pending"}`; Ctrl-C exits cleanly.

- [ ] **Step 3: Commit**

```bash
git add server/server.mjs
git commit -m "feat(server): wire modules, boot sequence and shutdown"
```

---

### Task 10: Spike A — real-device verification script

**Files:**
- Create: `server/spikes/spike-a.mjs`, `server/spikes/README.md`

Throwaway. Answers: does login/2FA work; which cameras are wired; what codec/resolution each emits; does the dual-view command take effect and what geometry results; is the peer address visible and on the LAN; does `streamingQuality` write work.

- [ ] **Step 1: Write `server/spikes/spike-a.mjs`**

```js
// Throwaway probe. Usage: EUFY_EMAIL=… EUFY_PASSWORD=… node spikes/spike-a.mjs [--sn T8214X --dual 12] [--quality "Full HD (1080P)"]
import { writeFileSync, mkdirSync } from "node:fs";
import { createInterface } from "node:readline/promises";
import { loadConfig } from "../src/config.mjs";
import { createSdk } from "../src/sdk-adapter.mjs";

const args = Object.fromEntries(process.argv.slice(2).map((a, i, all) => (a.startsWith("--") ? [a.slice(2), all[i + 1]] : [])).filter((x) => x.length));
const { cfg } = loadConfig({ configPath: process.env.BRIDGE_CONFIG || "./config.yaml" });
const { eufy, sdk } = createSdk({ cfg, DEBUG: false });
const rl = createInterface({ input: process.stdin, output: process.stdout });

let r = await eufy.login();
while (r.status !== sdk.LoginStatus.Ok) {
  if (r.status === sdk.LoginStatus.Captcha) {
    writeFileSync("captcha.png", Buffer.from(r.image.split(",")[1], "base64"));
    r = await eufy.solveCaptcha(await rl.question("captcha written to captcha.png — answer: "));
  } else r = await eufy.submitVerifyCode(await rl.question(`2FA code (${r.method}): `));
}
console.log("login ok");
mkdirSync("spike-out", { recursive: true });

for (const d of await eufy.getDevices()) {
  const m = await sdk.describe(d.sn).catch((e) => ({ sn: d.sn, error: e.message }));
  console.log(JSON.stringify(m));
  if (!m.isCamera) continue;
  if (args.sn && args.sn !== m.sn) continue;
  const client = await sdk.streamClientFor(m.sn);
  client.on("p2pConnect", (st) => console.log(`  ${m.sn}: p2pConnect ${st} peer=${sdk.sessionPeerHost(client, st) ?? "?"}`));
  if (args.dual && m.sn === args.sn) {
    await sdk.sendSetPayload(client, m.sn, 6243, { restore: 1, video_type: Number(args.dual) });
    console.log(`  ${m.sn}: sent dual view ${args.dual}`);
  }
  if (args.quality) await eufy.setProperty(m.sn, "streamingQuality", args.quality).then(() => console.log("  quality set")).catch((e) => console.log(`  quality set failed: ${e.message}`));
  try {
    const feed = await sdk.openFeed(client, m.sn);
    const out = [];
    let codec, geom;
    const t = setTimeout(() => feed.destroy(), 10_000);
    for await (const chunk of feed) {
      out.push(chunk);
      const sets = sdk.extractParamSets(chunk);
      if (sets && !codec) { codec = sets.codec; geom = sdk.codedGeometry(sets); }
    }
    clearTimeout(t);
    const file = `spike-out/${m.sn}.${codec === "h265" ? "h265" : "h264"}`;
    writeFileSync(file, Buffer.concat(out));
    console.log(`  ${m.sn}: codec=${codec} ${geom?.width}x${geom?.height} bytes=${Buffer.concat(out).length} → ${file} (verify: ffprobe ${file})`);
  } catch (e) {
    console.log(`  ${m.sn}: stream failed: ${e.message}`);
  }
}
await sdk.closeStreamClients();
await eufy.disconnect();
process.exit(0);
```

- [ ] **Step 2: Write `server/spikes/README.md`**

```markdown
# Spike A — real-device probe (throwaway)

Run from `server/` with a `config.yaml` (or env creds). Node ≥ 24.5.

    node spikes/spike-a.mjs                       # all cameras: codec, geometry, 10 s dump each, peer IPs
    node spikes/spike-a.mjs --sn T8214XXX --dual 12   # E340: set split-view, then dump → record resolution
    node spikes/spike-a.mjs --quality "Full HD (1080P)"   # does the SDK quality write land? (expect "failed: wire unverified" on 0.1.1)

Record the answers in docs/runbook-server.md → "Verified devices":
- per camera: model, wired/battery, codec, WxH, p2p peer IP (must be in lan.cidr)
- E340 in view 12: WxH and whether the frame is stacked (open the .h264 with `ffplay -f h264 file`)
- whether the dual-view command took effect (compare a dump before/after)
- eufy-sdk issue #200 (T8214 stream fails): does the doorbell stream at all?
```

- [ ] **Step 3: Run it against real hardware** (needs the user's account; do this together with the user)

Run: `cd server && cp config.example.yaml config.yaml` (fill creds) `&& node spikes/spike-a.mjs`
Expected: one JSON line per device; wired cameras dump a `.h264` file; `ffprobe spike-out/<sn>.h264` shows `h264` and the resolution; peer IPs inside the LAN CIDR. Write findings into `docs/runbook-server.md` (Task 11).

- [ ] **Step 4: Commit**

```bash
git add server/spikes
git commit -m "chore(server): spike A device probe script"
```

---

### Task 11: Deployment (install script, systemd unit, runbook)

**Files:**
- Create: `deploy/install-server.sh`, `deploy/eufy-wall-bridge.service`, `deploy/eufy-wall-bridge.env.example`, `docs/runbook-server.md`

- [ ] **Step 1: Write `deploy/eufy-wall-bridge.service`**

```ini
[Unit]
Description=eufy-wall-bridge (eufy P2P → RTSP for the camera wall)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=eufy-wall
Group=eufy-wall
WorkingDirectory=/opt/eufy-wall-bridge/server
EnvironmentFile=-/etc/eufy-wall-bridge.env
Environment=BRIDGE_CONFIG=/etc/eufy-wall-bridge.yaml
Environment=BRIDGE_DATA_DIR=/var/lib/eufy-wall-bridge
Environment=GO2RTC_BIN=/opt/eufy-wall-bridge/bin/go2rtc
ExecStart=/usr/bin/node /opt/eufy-wall-bridge/server/server.mjs
Restart=always
RestartSec=5
# The bridge exits(1) on purpose after 5 min of continuous stream failure; systemd brings it back.
StartLimitIntervalSec=0
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=/var/lib/eufy-wall-bridge

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Write `deploy/eufy-wall-bridge.env.example`**

```bash
# /etc/eufy-wall-bridge.env — secrets for the bridge (chmod 600). Non-secret settings live in
# /etc/eufy-wall-bridge.yaml (copy of server/config.example.yaml).
EUFY_EMAIL=wall@example.com
EUFY_PASSWORD=change-me
EUFY_COUNTRY=US
#BRIDGE_DEBUG=1
```

- [ ] **Step 3: Write `deploy/install-server.sh`**

```bash
#!/usr/bin/env bash
# Install eufy-wall-bridge on Debian/Ubuntu (amd64 or arm64) without Docker.
#   sudo deploy/install-server.sh            (run from the repo root)
# Installs Node 24 (NodeSource), a go2rtc release binary, the server under /opt/eufy-wall-bridge, a
# system user, config/env skeletons in /etc and a systemd unit. Idempotent: re-run to upgrade.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run as root (sudo)"; exit 1; }
REPO=$(cd "$(dirname "$0")/.." && pwd)
PREFIX=/opt/eufy-wall-bridge
GO2RTC_VERSION=${GO2RTC_VERSION:-1.9.9}

# 1. Node 24
if ! command -v node >/dev/null || [[ $(node -v | sed 's/v\([0-9]*\).*/\1/') -lt 24 ]]; then
  apt-get update && apt-get install -y ca-certificates curl gnupg
  curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
  apt-get install -y nodejs
fi
node -v

# 2. go2rtc binary
case "$(uname -m)" in
  x86_64) ARCH=amd64 ;; aarch64) ARCH=arm64 ;; armv7l) ARCH=arm ;; armv6l) ARCH=armv6 ;;
  *) echo "unsupported arch $(uname -m)"; exit 1 ;;
esac
mkdir -p "$PREFIX/bin"
curl -fsSL -o "$PREFIX/bin/go2rtc" "https://github.com/AlexxIT/go2rtc/releases/download/v${GO2RTC_VERSION}/go2rtc_linux_${ARCH}"
chmod +x "$PREFIX/bin/go2rtc"
"$PREFIX/bin/go2rtc" --version || true

# 3. app files + deps
id -u eufy-wall >/dev/null 2>&1 || useradd --system --home /var/lib/eufy-wall-bridge --shell /usr/sbin/nologin eufy-wall
mkdir -p "$PREFIX/server" /var/lib/eufy-wall-bridge
rsync -a --delete --exclude node_modules --exclude data --exclude spike-out "$REPO/server/" "$PREFIX/server/"
(cd "$PREFIX/server" && npm ci --omit=dev --no-audit --no-fund)
chown -R eufy-wall:eufy-wall "$PREFIX" /var/lib/eufy-wall-bridge

# 4. config skeletons (never overwrite)
[[ -f /etc/eufy-wall-bridge.yaml ]] || { cp "$REPO/server/config.example.yaml" /etc/eufy-wall-bridge.yaml; echo "edit /etc/eufy-wall-bridge.yaml"; }
[[ -f /etc/eufy-wall-bridge.env ]] || { cp "$REPO/deploy/eufy-wall-bridge.env.example" /etc/eufy-wall-bridge.env; chmod 600 /etc/eufy-wall-bridge.env; echo "edit /etc/eufy-wall-bridge.env"; }

# 5. systemd
cp "$REPO/deploy/eufy-wall-bridge.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable eufy-wall-bridge
echo "installed. next: edit /etc/eufy-wall-bridge.{env,yaml}; systemctl start eufy-wall-bridge; journalctl -fu eufy-wall-bridge"
```
Run: `chmod +x deploy/install-server.sh && bash -n deploy/install-server.sh` → no syntax errors. Check the latest go2rtc tag at https://github.com/AlexxIT/go2rtc/releases and update `GO2RTC_VERSION` default if newer.

- [ ] **Step 4: Write `docs/runbook-server.md`**

```markdown
# Runbook — eufy-wall-bridge (server)

## Install (Debian, arm64 or x86, no Docker)
    git clone <this repo> && cd eufy-p2p-rtsp-bridge
    sudo deploy/install-server.sh
    sudo nano /etc/eufy-wall-bridge.env      # EUFY_EMAIL / EUFY_PASSWORD / EUFY_COUNTRY (dedicated account!)
    sudo nano /etc/eufy-wall-bridge.yaml     # lan.cidr, cameras, defaults
    sudo systemctl start eufy-wall-bridge && journalctl -fu eufy-wall-bridge

## First-run login (2FA / captcha)
    curl -s localhost:3000/auth/status
    # {"state":"require_2fa","method":"email"}  → get the code from email/SMS:
    curl -s -X POST 'localhost:3000/auth/tfa?code=123456'
    # {"state":"require_captcha"} →
    curl -s localhost:3000/auth/captcha -o captcha.png   # open it
    curl -s -X POST 'localhost:3000/auth/captcha?code=AB3D'
    # {"state":"pending"} after a failure → curl -s -X POST localhost:3000/auth/retry
The session token is saved in /var/lib/eufy-wall-bridge/.eufy-session.json; later restarts need no code.
Opening the eufy phone app with the SAME account kicks the bridge (state "reauth") — use a dedicated account.

## Check
    curl -s localhost:3000/healthz | jq          # auth ok, streaming [...], blocked {}, go2rtc running
    curl -s localhost:3000/api/cameras | jq      # codec must be "h264" for Pi clients
    ffplay rtsp://<server>:8554/<sn>             # from any machine on the LAN

## Camera settings the wall depends on
- Streaming quality: 1080p or 720p ⇒ H.264. 2K/Max ⇒ H.265 (Pi cannot decode). The SDK 0.1.1 cannot
  write this setting; set it in the eufy app (camera → Settings → Video → Streaming quality). The bridge
  logs `stream is H265` if a camera is wrong; /api/cameras shows `codec`.
- Dual-lens (E340 doorbell/floodlight, S340): the bridge sends view mode `dual_view` (default split=12)
  on boot and after reconnects.

## Force-LAN
`lan.force: true` closes any P2P session whose peer is outside `lan.cidr` and marks the camera
`blocked: wan-path <ip>` in /healthz and /api/cameras (HTTP 423 on /stream). The stream manager retries
with backoff. Verify with: `sudo tcpdump -ni <iface> udp and not net <lan.cidr>` — no sustained traffic.
If a station keeps connecting via WAN, add its LAN IP under `lan.station_addresses`.

## Robustness behaviour
- 12 s without video bytes → feed restarted with backoff 2→60 s (`stalls` counter in /healthz).
- 45 s gap → HTTP consumers (go2rtc) disconnected so they reconnect cleanly.
- 5 min continuous failure on any camera → process exit(1); systemd restarts it (`Restart=always`).
- go2rtc child dies → restarted after 3 s.
- Cloud poll silent ≥ 30 min or push down ≥ 15 min → re-login in place, else exit(1).

## Upgrading
- SDK: bump `@mega-yfue/eufy-sdk` in server/package.json (exact version), `npm test` (contract test
  fails loudly on removed APIs), rerun install script.
- Vendored ha-eufy-sdk-bridge modules: `server/scripts/sync-upstream.sh` shows diffs; `--apply` copies;
  update the SHA in server/src/vendor/ha-bridge/VENDOR.md.

## Verified devices (fill in from spike A)
| sn | model | power | codec | WxH | dual view | p2p peer |
|----|-------|-------|-------|-----|-----------|----------|
```

- [ ] **Step 5: Commit**

```bash
git add deploy docs/runbook-server.md
git commit -m "feat(server): systemd deployment, install script and runbook"
```

---

### Task 12: End-to-end verification on real hardware

No new files except runbook edits. Do this with the user's account and LAN.

- [ ] **Step 1: Install on the server box** — `sudo deploy/install-server.sh`, fill env/yaml, start, complete auth per runbook.
- [ ] **Step 2: `/healthz`** shows `auth.state: ok`, `go2rtc: running`, every enabled camera in `streaming`, `blocked: {}`.
- [ ] **Step 3: `/api/cameras`** — every enabled camera `codec: "h264"`; the E340 shows `dual: true`, `dualView: "split"` and its measured `width`/`height` (record in the runbook table).
- [ ] **Step 4: `ffplay rtsp://<server>:8554/<sn>`** plays each camera from a laptop on the LAN. Let one run 30 min; `/healthz` `stalls` stays 0 (or small and recovering).
- [ ] **Step 5: Force-LAN** — `sudo tcpdump -ni eth0 udp and not net 192.168.1.0/24` (your CIDR) shows no sustained media traffic. Set `lan.cidr: 10.99.0.0/16` temporarily → restart → cameras appear in `blocked` and `/stream/<sn>` returns 423. Revert.
- [ ] **Step 6: Recovery** — power-cycle the HomeBase / unplug a camera 1 min: logs show stall → reopen with backoff → `streaming` again without a process restart. `kill -9` the go2rtc pid → restarted in 3 s. `systemctl restart eufy-wall-bridge` → back to ready without a new 2FA code.
- [ ] **Step 7: Commit runbook updates**

```bash
git add docs/runbook-server.md
git commit -m "docs: record verified devices and e2e results"
```

---

## Self-review notes

- Spec coverage: config (T1), vendoring + sync + contract test (T2), sdk-adapter (T3), camera set + battery exclusion (T4), pins incl. dual view 12 and quality with honest "unsupported" path (T5), always-on warm consumers + stall watchdog + exit-after-stall + codec sniff/warn (T6), force-LAN (T7), go2rtc + HTTP incl. first-run auth (T8), boot wiring + re-apply pins on reconnect (T9), spike A (T10), no-Docker deploy + runbook (T11), e2e (T12). Upstream PRs (`p2pLocalOnly`, dual-view capability) are follow-ups outside this plan.
- Type consistency: `Camera.{sn,name,model,modelName,battery,enabled,quality,isDual,viewModeCmd,dualView}` used identically in T4/T5/T8/T9; `streamStatus()` shape identical in T4 test, T6, T8; `ctx.sdk` function names identical in T3/T5/T6/T7/T10.
