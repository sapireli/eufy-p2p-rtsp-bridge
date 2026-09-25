import { isDeepStrictEqual } from "node:util";
import { stringify } from "yaml";
import { FIELDS, TOP_LEVEL, loadConfig, parseConfigText } from "./config.mjs";

const validationEnv = { EUFY_EMAIL: "migration-check@example.invalid", EUFY_PASSWORD: "migration-check" };
const mapping = (value) => value && typeof value === "object" && !Array.isArray(value);

function knownFields(value, allowed, path, unsupported) {
  if (!mapping(value)) return value;
  return Object.fromEntries(Object.entries(value).flatMap(([key, entry]) => {
    const field = `${path}.${key}`;
    if (!allowed.includes(key)) { unsupported.push(field); return []; }
    if (field === "lan.upgrade") entry = knownFields(entry, FIELDS["lan.upgrade"], field, unsupported);
    return [[key, entry]];
  }));
}

function candidateFrom(raw, unsupported) {
  const candidate = { schema_version: 2 };
  for (const [key, value] of Object.entries(raw)) {
    if (key === "schema_version") continue;
    if (!TOP_LEVEL.has(key)) { unsupported.push(key); continue; }
    if (key === "cameras" && mapping(value)) {
      candidate.cameras = Object.fromEntries(Object.entries(value).map(([sn, camera]) => [sn, knownFields(camera, FIELDS.camera, `cameras.${sn}`, unsupported)]));
    } else if (FIELDS[key]) {
      candidate[key] = knownFields(value, FIELDS[key], key, unsupported);
    } else candidate[key] = value;
  }
  return candidate;
}

function diffPaths(previous, next, prefix = "", result = { added: [], removed: [], changed: [] }) {
  const keys = new Set([...Object.keys(previous), ...Object.keys(next)]);
  for (const key of keys) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (!Object.hasOwn(previous, key)) { result.added.push(path); continue; }
    if (!Object.hasOwn(next, key)) { result.removed.push(path); continue; }
    if (mapping(previous[key]) && mapping(next[key])) diffPaths(previous[key], next[key], path, result);
    else if (!isDeepStrictEqual(previous[key], next[key])) result.changed.push(path);
  }
  return result;
}

/** Convert the permissive v1 shape to a strict v2 candidate without writing or restarting anything. */
export function migrateLegacyConfig(sourceText, activeText = null) {
  const source = parseConfigText(sourceText);
  if (source.schema_version === 2) throw new Error("source already uses schema_version: 2; use config validate instead");
  loadConfig({ rawText: sourceText, env: validationEnv });
  const unsupportedPaths = [];
  const candidate = candidateFrom(source, unsupportedPaths);
  const candidateYaml = stringify(candidate);
  loadConfig({ rawText: candidateYaml, env: validationEnv });
  let diff = { added: Object.keys(candidate), removed: [], changed: [] };
  if (activeText != null) {
    try { diff = diffPaths(parseConfigText(activeText), candidate); }
    catch (error) { diff = { unavailable: `active config cannot be parsed: ${error.message}` }; }
  }
  return { sourceSchemaVersion: source.schema_version ?? 1, lossless: unsupportedPaths.length === 0,
    unsupportedPaths, candidateYaml, diff };
}
