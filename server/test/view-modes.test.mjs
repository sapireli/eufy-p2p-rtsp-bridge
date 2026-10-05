import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createViewModes } from "../src/view-modes.mjs";

test("dual view choices survive a bridge restart", () => {
  const dir = mkdtempSync(join(tmpdir(), "eufy-views-"));
  try {
    const modes = createViewModes(dir);
    modes.save("FRONT", "pip-br");
    modes.save("GARAGE", "split");
    const reopened = createViewModes(dir);
    assert.equal(reopened.get("FRONT"), "pip-br");
    assert.equal(reopened.get("GARAGE"), "split");
    assert.throws(() => reopened.save("FRONT", "bad-mode"), /invalid/);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
