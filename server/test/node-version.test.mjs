import { test } from "node:test";
import assert from "node:assert/strict";
import { supportedNode } from "../src/node-version.mjs";

test("doctor uses the same minimum Node release as the package", () => {
  for (const version of ["23.99.0", "24.0.0", "24.4.99", "24.5", "unknown"])
    assert.equal(supportedNode(version), false, version);
  for (const version of ["24.5.0", "24.5.1", "24.12.0", "25.0.0", "26.0.0"])
    assert.equal(supportedNode(version), true, version);
});
