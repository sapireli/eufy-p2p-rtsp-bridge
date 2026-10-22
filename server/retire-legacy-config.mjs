#!/usr/bin/env node
// Offline cleanup after settings.json has been imported and verified. Never starts the bridge.
import { constants, openSync, closeSync, readFileSync, readSync, fstatSync, lstatSync, writeFileSync, fsyncSync, fchownSync, fchmodSync, renameSync, unlinkSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { isDeepStrictEqual } from "node:util";
import { randomUUID, createHash } from "node:crypto";
import { parseDocument, isMap } from "yaml";
import { SettingsStore, validateSettings } from "./src/settings-store.mjs";

const OPERATING = ["defaults", "cameras", "lan", "go2rtc", "freshness", "stall", "poll_ms"];
const STARTUP = ["eufy", "host", "port", "rtsp_port", "go2rtc_api_port", "self_host", "data_dir", "go2rtc_bin"];
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
const object = value => value !== null && typeof value === "object" && !Array.isArray(value);
function regular(path) {
  for (let current = path; current !== dirname(current); current = dirname(current)) {
    if (lstatSync(current).isSymbolicLink()) throw new Error("Retirement refuses symlink path components");
  }
  const stat = lstatSync(path);
  if (stat.isSymbolicLink() || !stat.isFile()) throw new Error("Retirement requires regular files, without symlinks");
  return stat;
}
function openExisting(path) {
  regular(path);
  const fd = openSync(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
  const stat = fstatSync(fd);
  if (!stat.isFile()) { closeSync(fd); throw new Error("Retirement requires regular files"); }
  return fd;
}
function bytesAtStart(fd) {
  const bytes = Buffer.alloc(fstatSync(fd).size);
  let at = 0;
  while (at < bytes.length) {
    const count = readSync(fd, bytes, at, bytes.length - at, at);
    if (!count) throw new Error("File changed while reading");
    at += count;
  }
  return bytes;
}
function assertUnchanged(path, initialStat, bytes) {
  const current = regular(path);
  if (current.dev !== initialStat.dev || current.ino !== initialStat.ino || current.size !== initialStat.size ||
      current.mtimeMs !== initialStat.mtimeMs || current.ctimeMs !== initialStat.ctimeMs || !readFileSync(path).equals(bytes)) {
    throw new Error("File changed during retirement; no further changes were made");
  }
}
function validateLegacy(raw) {
  if (!object(raw)) throw new Error("Legacy YAML must contain a mapping");
  for (const key of Object.keys(raw)) if (![...OPERATING, ...STARTUP].includes(key)) throw new Error(`Unknown legacy key: ${key}`);
  if (raw.eufy !== undefined) {
    if (!object(raw.eufy)) throw new Error("eufy must contain a mapping");
    for (const key of Object.keys(raw.eufy)) if (!["email", "password", "country"].includes(key)) throw new Error(`Unknown eufy key: ${key}`);
  }
  const settings = Object.fromEntries(OPERATING.filter(key => Object.hasOwn(raw, key)).map(key => [key, structuredClone(raw[key])]));
  if (object(settings.go2rtc) && Object.hasOwn(settings.go2rtc, "sources_file")) {
    if (typeof settings.go2rtc.sources_file !== "string" || !settings.go2rtc.sources_file.trim()) throw new Error("go2rtc.sources_file must be a nonempty string");
    delete settings.go2rtc.sources_file;
  }
  validateSettings(settings);
}
function privateFile(path, bytes, originalStat, mode = 0o600) {
  const fd = openSync(path, "wx", 0o600);
  try {
    if (process.getuid?.() === 0) fchownSync(fd, originalStat.uid, originalStat.gid);
    fchmodSync(fd, mode);
    writeFileSync(fd, bytes);
    fsyncSync(fd);
  } finally { closeSync(fd); }
}

/** Remove imported operating keys only; callers must arrange offline operation themselves. */
export function retireLegacyConfig({ configPath, dataDir } = {}) {
  if (typeof configPath !== "string" || !configPath || typeof dataDir !== "string" || !dataDir) throw new Error("configPath and dataDir are required");
  const path = resolve(configPath), storePath = resolve(dataDir, "settings.json");
  if (path === storePath) throw new Error("Config and settings paths must differ");
  // Open only existing files. Calling the constructor here could create/import a missing store.
  const storeFd = openExisting(storePath);
  let configFd;
  try {
    const storeStat = fstatSync(storeFd), storeBytes = bytesAtStart(storeFd);
    const snapshot = SettingsStore.prototype.load.call({ path: storeFd });
    configFd = openExisting(path);
    const originalStat = fstatSync(configFd), original = bytesAtStart(configFd);
    const document = parseDocument(original.toString("utf8"), { uniqueKeys: true });
    if (document.errors.length || !isMap(document.contents)) throw new Error("Legacy YAML must be a valid mapping without duplicate keys");
    const raw = document.toJS({ maxAliasCount: 100 });
    validateLegacy(raw);
    const removed = OPERATING.filter(key => document.has(key));
    assertUnchanged(storePath, storeStat, storeBytes);
    assertUnchanged(path, originalStat, original);
    if (!removed.length) return { changed: false, removed: [], revision: snapshot.revision, storeSha256: hash(storeBytes) };
    for (const key of removed) document.delete(key);
    const next = Buffer.from(String(document));
    const remaining = parseDocument(next.toString("utf8"), { uniqueKeys: true });
    if (remaining.errors.length) throw new Error("Removing legacy keys would invalidate bootstrap YAML");
    const expected = Object.fromEntries(Object.entries(raw).filter(([key]) => !OPERATING.includes(key)));
    if (!isDeepStrictEqual(remaining.toJS({ maxAliasCount: 100 }), expected)) throw new Error("Removing legacy keys would change bootstrap values");
    const backupPath = `${path}.before-settings-retirement-${new Date().toISOString().replaceAll(":", "-")}-${randomUUID()}.bak`;
    const temporary = `${path}.${randomUUID()}.tmp`;
    privateFile(backupPath, original, originalStat);
    try {
      privateFile(temporary, next, originalStat, originalStat.mode & 0o777);
      assertUnchanged(storePath, storeStat, storeBytes);
      assertUnchanged(path, originalStat, original);
      renameSync(temporary, path);
      const dirFd = openSync(dirname(path), constants.O_RDONLY);
      try { fsyncSync(dirFd); } finally { closeSync(dirFd); }
      assertUnchanged(storePath, storeStat, storeBytes);
    } finally {
      try { unlinkSync(temporary); } catch (error) { if (error.code !== "ENOENT") throw error; }
    }
    return { changed: true, removed, backupPath, revision: snapshot.revision, storeSha256: hash(storeBytes) };
  } finally { if (configFd !== undefined) closeSync(configFd); closeSync(storeFd); }
}

function cli(args) {
  const values = {};
  for (let index = 0; index < args.length; index += 2) {
    const key = args[index];
    if (!["--config", "--data-dir"].includes(key) || !args[index + 1] || Object.hasOwn(values, key)) throw new Error("Usage: node retire-legacy-config.mjs --config PATH --data-dir PATH");
    values[key] = args[index + 1];
  }
  if (!values["--config"] || !values["--data-dir"]) throw new Error("Usage: node retire-legacy-config.mjs --config PATH --data-dir PATH");
  console.log(JSON.stringify(retireLegacyConfig({ configPath: values["--config"], dataDir: values["--data-dir"] })));
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { cli(process.argv.slice(2)); }
  catch (error) { console.error(`Legacy config retirement failed: ${error.message}`); process.exitCode = 1; }
}
