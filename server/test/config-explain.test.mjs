import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { explainConfigField, missingExplainFields } from "../src/config-explain.mjs";

test("every strict v2 server field has specific inline help", () => {
  assert.deepEqual(missingExplainFields(), []);
  assert.match(explainConfigField("host").explanation, /0\.0\.0\.0/);
  assert.match(explainConfigField("cameras.T8214A.mode").explanation, /on_demand/);
  assert.match(explainConfigField("lan.station_addresses.T8010A").explanation, /station serial/);
  assert.throws(() => explainConfigField("cameras.T8214A.unknown"), /unknown config field/);
});

test("server CLI explains an indexed camera field without loading credentials", () => {
  const cli = resolve(fileURLToPath(new URL("../cli.mjs", import.meta.url)));
  const result = spawnSync(process.execPath, [cli, "config", "explain", "cameras.T8214A.codec", "--json"],
    { encoding: "utf8", env: { ...process.env, BRIDGE_ENV: "/does/not/exist" } });
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout).path, "cameras.T8214A.codec");
  assert.match(JSON.parse(result.stdout).explanation, /h264 or h265/);
  const extra = spawnSync(process.execPath, [cli, "config", "explain", "host", "ignored"], { encoding: "utf8" });
  assert.equal(extra.status, 1);
  assert.match(extra.stderr, /usage: eufy-bridge config explain/);
});
