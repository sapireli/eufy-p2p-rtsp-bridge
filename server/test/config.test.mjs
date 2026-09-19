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
