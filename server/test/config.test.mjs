import { test } from "node:test";
import assert from "node:assert/strict";
import { writeFileSync, mkdtempSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { loadConfig, isValidCidr } from "../src/config.mjs";

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
  T8214X: { dual_view: split, power_override: always-on }
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
  // Unset by default: dual view belongs to the eufy app (owner account only), and writing it from the
  // bridge is a SET_PAYLOAD to a possibly-live camera for a setting nobody asked us to change.
  assert.equal(cfg.defaults.dualView, null);
  assert.deepEqual(cfg.cameras.T8410X, { name: "Garage" });
  assert.deepEqual(cfg.cameras.T8214X, { dualView: "split", powerOverride: "always-on" });
  assert.deepEqual(cfg.cameras.T8113X, { enabled: false });
  assert.deepEqual(cfg.stall, { stallMs: 30000, gapMs: 45000, exitAfterMs: 300000, recreateClientAfter: 8, backoffMs: [1000, 2000, 4000, 8000] });
  assert.equal(cfg.go2rtcBin, "go2rtc");
});

test("rejects missing credentials and bad cidr", () => {
  const p = tmpYaml(`eufy: { email: a@b.c }\nlan: { cidr: nope }`);
  assert.throws(() => loadConfig({ env: {}, configPath: p }), /password/);
  const p2 = tmpYaml(`eufy: { email: a@b.c, password: x }\nlan: { cidr: nope }`);
  assert.throws(() => loadConfig({ env: {}, configPath: p2 }), /lan.cidr/);
});

test("lan.cidr: octets must be 0–255 and prefix 0–32, not just regex-shaped", () => {
  for (const bad of ["192.168.1.0/33", "192.168.1.0/99", "256.0.0.0/8", "10.0.0.999/8", "1.2.3/8", "10.0.0.0"]) {
    assert.equal(isValidCidr(bad), false, bad);
    const p = tmpYaml(`eufy: { email: a@b.c, password: x }\nlan: { cidr: "${bad}" }`);
    assert.throws(() => loadConfig({ env: {}, configPath: p }), /lan\.cidr/, bad);
  }
  for (const good of ["0.0.0.0/0", "255.255.255.255/32", "10.0.0.0/8", "192.168.1.0/24"]) {
    assert.equal(isValidCidr(good), true, good);
    const p = tmpYaml(`eufy: { email: a@b.c, password: x }\nlan: { cidr: "${good}" }`);
    assert.equal(loadConfig({ env: {}, configPath: p }).cfg.lan.cidr, good);
  }
});

test("missing config file → env-only config", () => {
  const { cfg } = loadConfig({ env: { EUFY_EMAIL: "e", EUFY_PASSWORD: "p", EUFY_COUNTRY: "DE" }, configPath: "/nonexistent.yaml" });
  assert.equal(cfg.country, "DE");
  assert.deepEqual(cfg.cameras, {});
  assert.equal(cfg.lan.force, false);
});

test("dual view is only written when the operator asks for it", () => {
  const off = loadConfig({ env: {}, configPath: tmpYaml(`eufy: { email: a@b.c, password: p, country: US }\n`) }).cfg;
  assert.equal(off.defaults.dualView, null, "nothing set → the bridge writes nothing to the camera");

  const on = loadConfig({
    env: {},
    configPath: tmpYaml(`eufy: { email: a@b.c, password: p, country: US }\ndefaults: { dual_view: pip-tl }\n`),
  }).cfg;
  assert.equal(on.defaults.dualView, "pip-tl", "asked for explicitly → honoured");

  assert.throws(
    () => loadConfig({ env: {}, configPath: tmpYaml(`eufy: { email: a@b.c, password: p, country: US }\ndefaults: { dual_view: sideways }\n`) }),
    /dual_view must be one of/,
    "a typo is still rejected rather than silently ignored",
  );
});

test("power override is an explicit SDK policy with validated values", () => {
  const config = (value) => tmpYaml(`eufy: { email: a@b.c, password: p }\ncameras: { T8214X: { power_override: ${value} } }\n`);
  for (const value of ["auto", "always-on", "battery"])
    assert.equal(loadConfig({ env: {}, configPath: config(value) }).cfg.cameras.T8214X.powerOverride, value);
  assert.throws(() => loadConfig({ env: {}, configPath: config("wired") }), /power_override must be one of/);
});
