// Operator-selected dual-lens layouts. Stored outside config.yaml so a TV can switch modes without
// editing deployment configuration or losing its choice on a service restart.
import { readFileSync, writeFileSync, renameSync, existsSync } from "node:fs";
import { join } from "node:path";
import { DUAL_VIEW_VALUES } from "./cameras.mjs";

export function createViewModes(dataDir) {
  const path = join(dataDir, "dual-view-modes.json");
  let modes = {};
  if (existsSync(path)) {
    const saved = JSON.parse(readFileSync(path, "utf8"));
    if (saved && typeof saved === "object" && !Array.isArray(saved)) {
      modes = Object.fromEntries(Object.entries(saved).filter(([, mode]) => Object.hasOwn(DUAL_VIEW_VALUES, mode)));
    }
  }
  return {
    get: (sn) => modes[sn],
    save(sn, mode) {
      if (!Object.hasOwn(DUAL_VIEW_VALUES, mode)) throw new Error("invalid dual view mode");
      const next = { ...modes, [sn]: mode };
      const temp = `${path}.tmp`;
      writeFileSync(temp, JSON.stringify(next, null, 2) + "\n", { mode: 0o600 });
      renameSync(temp, path);
      modes = next;
    },
  };
}
