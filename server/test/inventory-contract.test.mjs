import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import http from "node:http";

const fixture = new URL("../../config-contract/inventory-v1.json", import.meta.url);
const cli = new URL("../cli.mjs", import.meta.url);

function exportInventory(file, config, envFile) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [cli.pathname, "inventory", "export", file, "--host", "bridge.local", "--json"], {
      env: { ...process.env, BRIDGE_CONFIG: config, BRIDGE_ENV: envFile, EUFY_EMAIL: "test@example.com", EUFY_PASSWORD: "test-password" },
    });
    let stdout = "", stderr = "";
    child.stdout.on("data", (chunk) => stdout += chunk);
    child.stderr.on("data", (chunk) => stderr += chunk);
    child.on("error", reject);
    child.on("close", (code) => resolve({ code, stdout, stderr }));
  });
}

test("inventory exporter conforms to the shared version 1 contract", async (t) => {
  const expected = JSON.parse(await readFile(fixture, "utf8"));
  const dir = await mkdtemp(join(tmpdir(), "inventory-contract-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const apiCameras = expected.cameras.map(({ rtsp, ...camera }) => ({ ...camera, accessToken: "must-not-export" }));
  const server = http.createServer((request, response) => {
    assert.equal(request.url, "/api/cameras");
    response.setHeader("content-type", "application/json");
    response.end(JSON.stringify(apiCameras));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));

  const config = join(dir, "bridge.yaml"), destination = join(dir, "inventory.json");
  await writeFile(config, `schema_version: 2\nhost: 127.0.0.1\nport: ${server.address().port}\n`);
  const result = await exportInventory(destination, config, join(dir, "absent.env"));
  assert.equal(result.code, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), { ok: true, file: destination, cameras: 3 });

  const produced = JSON.parse(await readFile(destination, "utf8"));
  assert.equal(produced.schema_version, expected.schema_version);
  assert.ok(!Number.isNaN(Date.parse(produced.exported_at)));
  assert.equal(produced.bridge_url, `http://bridge.local:${server.address().port}`);
  assert.deepEqual(produced.cameras, expected.cameras);
  assert.equal(produced.cameras[1].rtsp, "rtsp://bridge.local:8554/entry%2Fside%20door%20%231");
  assert.equal(produced.cameras[2].codec, null);
  assert.doesNotMatch(JSON.stringify(produced), /must-not-export/);
});
