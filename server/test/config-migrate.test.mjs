import { test } from "node:test";
import assert from "node:assert/strict";
import { parse } from "yaml";
import { migrateLegacyConfig } from "../src/config-migrate.mjs";

test("migration preserves supported legacy fields, including nested camera and LAN policy", () => {
  const source = `schema_version: 1
eufy: { email: wall@example.com, password: private, country: US }
host: 192.168.1.10
port: 3001
self_host: 127.0.0.1
data_dir: /var/lib/bridge
go2rtc_bin: /usr/bin/go2rtc
poll_ms: 1000
lan:
  cidr: 192.168.1.0/24
  force: true
  station_addresses: { HOME: 192.168.1.4 }
  upgrade: { enabled: false, interval_ms: 120000, window_ms: 6000, stable_ms: 15000, initial_window_ms: 14000, max_backoff_ms: 900000 }
defaults: { quality: high, dual_view: split, hold_seconds: 60, motion_events: [motion] }
cameras:
  DOOR: { name: Door, enabled: true, mode: on_motion, power_override: battery, hold_seconds: 90, codec: h265, quality: high, dual_view: pip-tr }
stall: { stall_ms: 30000, gap_ms: 45000, exit_after_ms: 300000, recreate_client_after: 8, backoff_ms: [1000, 2000] }
go2rtc: { transcode: never }
`;
  const report = migrateLegacyConfig(source, "schema_version: 2\nhost: 127.0.0.1\n");
  assert.equal(report.lossless, true);
  assert.deepEqual(report.unsupportedPaths, []);
  assert.deepEqual(parse(report.candidateYaml), { ...parse(source), schema_version: 2 });
  assert.ok(report.diff.changed.includes("host"));
  assert.ok(report.diff.added.includes("cameras"));
  assert.deepEqual(report.diff.removed, []);
});

test("migration reports every omitted unsupported path and never mistakes dynamic serials for unknown fields", () => {
  const source = `host: 0.0.0.0
extra: ignored
lan:
  station_addresses: { HOME: 192.168.1.4 }
  upgrade: { enabled: true, mystery: ignored }
  unknown_lan: ignored
cameras:
  DOOR: { mode: on_motion, misspelled_mode: always }
`;
  const report = migrateLegacyConfig(source);
  assert.equal(report.lossless, false);
  assert.deepEqual(report.unsupportedPaths, ["extra", "lan.upgrade.mystery", "lan.unknown_lan", "cameras.DOOR.misspelled_mode"]);
  assert.equal(parse(report.candidateYaml).lan.station_addresses.HOME, "192.168.1.4");
  assert.equal(parse(report.candidateYaml).cameras.DOOR.mode, "on_motion");
  assert.equal(report.candidateYaml.includes("misspelled_mode"), false);
});

test("migration rejects invalid known values and already-versioned inputs", () => {
  assert.throws(() => migrateLegacyConfig("lan: { force: true }\n"), /requires lan.cidr/);
  assert.throws(() => migrateLegacyConfig("cameras: { DOOR: null }\n"), /cameras.DOOR must be a mapping/);
  assert.throws(() => migrateLegacyConfig("schema_version: 2\n"), /already uses schema_version/);
  assert.throws(() => migrateLegacyConfig("schema_version: 1\nport: 1\nport: 2\n"), /keys must be unique/i);
});
