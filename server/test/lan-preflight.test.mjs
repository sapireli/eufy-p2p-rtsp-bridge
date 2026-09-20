import { test } from "node:test";
import assert from "node:assert/strict";
import { preflightLan, explain, reportLanPreflight } from "../src/lan-preflight.mjs";

// A host that refuses to send UDP produces the most misleading failure this bridge has: the station still
// reaches us, so sessions look connected and only the handshake never completes. These tests pin the
// diagnostic that tells an operator it is their machine and not the camera.

test("a station we cannot send to is reported with an actionable hint", async () => {
  const probe = async (host) => (host === "10.0.0.5" ? "EHOSTUNREACH" : undefined);
  const out = await preflightLan({ STA1: "10.0.0.5", STA2: "10.0.0.6" }, probe);
  const bad = out.find((r) => r.sn === "STA1");
  assert.equal(bad.ok, false);
  assert.equal(bad.code, "EHOSTUNREACH");
  assert.match(bad.hint, /Local Network/, "must name the setting that most often causes it");
  assert.match(bad.hint, /reject route/);
  assert.equal(out.find((r) => r.sn === "STA2").ok, true);
});

test("a station address with a port is probed by host", async () => {
  const seen = [];
  await preflightLan({ STA: "192.168.1.9:32108" }, async (h) => {
    seen.push(h);
    return undefined;
  });
  assert.deepEqual(seen, ["192.168.1.9"]);
});

test("only failures are logged, and the probe never throws the bridge down", async () => {
  const lines = [];
  await reportLanPreflight({ A: "10.0.0.5", B: "10.0.0.6" }, (l) => lines.push(l), async (h) =>
    h === "10.0.0.5" ? "EHOSTUNREACH" : undefined,
  );
  assert.equal(lines.length, 1, "a reachable station is not worth a line");
  assert.match(lines[0], /^\[bridge\] A: /);

  const out = await reportLanPreflight({ A: "10.0.0.5" }, () => {}, async () => {
    throw new Error("probe exploded");
  });
  assert.deepEqual(out, [], "a broken diagnostic must not take the bridge down");
});

test("explain covers permission errors and stays quiet on success", () => {
  assert.equal(explain(undefined, "10.0.0.1"), undefined);
  assert.match(explain("EACCES", "10.0.0.1"), /firewall or sandbox/);
  assert.match(explain("EWEIRD", "10.0.0.1"), /EWEIRD/);
});
