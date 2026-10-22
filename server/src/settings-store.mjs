// Durable operating settings. Startup paths and account credentials remain outside this store.
import { readFileSync, existsSync, mkdirSync, openSync, writeFileSync, fsyncSync, closeSync, renameSync, unlinkSync, chmodSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { randomUUID } from "node:crypto";
import { isIP } from "node:net";
import { parse } from "yaml";

export class SettingsValidationError extends Error { constructor(message) { super(message); this.name = "SettingsValidationError"; this.statusCode = 400; } }
export class SettingsConflictError extends Error { constructor(revision) { super("Settings changed; reload before saving"); this.name = "SettingsConflictError"; this.statusCode = 409; this.revision = revision; } }
const fail = (path, message) => { throw new SettingsValidationError(`${path} ${message}`); };
const object = (v) => v !== null && typeof v === "object" && !Array.isArray(v) && [Object.prototype, null].includes(Object.getPrototypeOf(v));
const clone = (v) => structuredClone(v);
const string = (v, p) => { if (typeof v !== "string" || !v.trim() || v.length > 4096) fail(p, "must be a nonempty string of at most 4096 characters"); };
const bool = (v, p) => { if (typeof v !== "boolean") fail(p, "must be a boolean"); };
const number = (min = 0) => (v, p) => { if (typeof v !== "number" || !Number.isFinite(v) || v < min || v > Number.MAX_SAFE_INTEGER) fail(p, `must be a finite number >= ${min}`); };
const boundedInteger = (min, max) => (v,p) => { integer(min)(v,p); if (v > max) fail(p, `must be <= ${max}`); };
const integer = (min = 0) => (v, p) => { number(min)(v, p); if (!Number.isSafeInteger(v)) fail(p, "must be an integer"); };
const milliseconds = (min = 0) => boundedInteger(min, 2_147_483_647);
const choice = (...values) => (v, p) => { if (!values.includes(v)) fail(p, `must be one of ${values.join(", ")}`); };
const nullable = (validate) => (v, p) => { if (v !== null) validate(v, p); };
const dual = choice("split", "pip-tl", "pip-tr", "pip-bl", "pip-br", "single");
const transcode = choice("never", "auto", "always");
const cidr = (v, p) => { if (v === null) return; if (typeof v !== "string" || !/^\d+\.\d+\.\d+\.\d+\/\d+$/.test(v)) fail(p, "must be an IPv4 CIDR"); const [ip,bits] = v.split("/"); if (isIP(ip) !== 4 || Number(bits) > 32) fail(p, "must be an IPv4 CIDR"); };
const map = (validate) => (v, p) => { if (!object(v)) fail(p, "must be an object"); for (const [k, x] of Object.entries(v)) { if (!/^[A-Za-z0-9_.-]{1,128}$/.test(k) || ["__proto__", "constructor", "prototype"].includes(k)) fail(p, "contains an invalid key"); validate(x, `${p}.${k}`); } };
const shape = (fields) => (v, p) => { if (!object(v)) fail(p, "must be an object"); for (const [k,x] of Object.entries(v)) { if (!Object.hasOwn(fields,k)) fail(`${p}.${k}`, "is not a supported setting"); fields[k](x,`${p}.${k}`); } };
const list = (validate, min = 0) => (v,p) => { if (!Array.isArray(v) || v.length < min || v.length > 1024) fail(p, "must be a bounded array"); v.forEach((x,i) => validate(x,`${p}[${i}]`)); };
const CAMERA_FIELDS = { name:string, enabled:bool, mode:choice("always","on_motion","on_demand"), power_override:choice("auto","always-on","battery"), hold_seconds:number(0.001), codec:choice("h264","h265"), transcode, vaapi_decode:bool, quality:string, dual_view:dual };
const camera = shape(CAMERA_FIELDS);
const stream = (v,p) => { const urls = Array.isArray(v) ? v : [v]; list((url,path) => { string(url,path); if (!/^(wyze|rtsp|rtsps):\/\//.test(url) || /[\r\n]/.test(url)) fail(path,"must be a Wyze or RTSP URL"); },1)(urls,p); };
const validate = shape({
  defaults:shape({ quality:nullable(string), dual_view:nullable(dual), hold_seconds:number(0.001), motion_events:list(string) }),
  cameras:map(camera),
  lan:shape({ cidr, force:bool, station_addresses:map((v,p) => { if (typeof v !== "string" || isIP(v) === 0) fail(p,"must be an IP address"); }), upgrade:shape({ enabled:bool, interval_ms:milliseconds(1), window_ms:milliseconds(1), stable_ms:milliseconds(0), initial_window_ms:milliseconds(1), max_backoff_ms:milliseconds(1) }) }),
  go2rtc:shape({ transcode, max_height:integer(0), vaapi_device:(v,p) => { if (v !== "" && (typeof v !== "string" || !/^\/dev\/dri\/renderD\d+$/.test(v))) fail(p,"must be a /dev/dri/renderD device or empty string"); }, vaapi_decode:bool, streams:map(stream) }),
  freshness:shape({ enabled:bool, mode:choice("observe","enforce"), max_age_ms:boundedInteger(100,60000) }),
  stall:shape({ stall_ms:milliseconds(1), startup_stall_ms:milliseconds(1), gap_ms:milliseconds(1), exit_after_ms:milliseconds(0), recreate_client_after:integer(0), backoff_ms:list(milliseconds(1),1) }),
  poll_ms:milliseconds(0),
});
export function validateSettings(settings) { validate(settings,"settings"); if (settings.lan?.force && !settings.lan?.cidr) fail("settings.lan.force","requires lan.cidr"); return clone(settings); }
const KEYS = ["defaults","cameras","lan","go2rtc","freshness","stall","poll_ms"];
function legacySettings(raw) {
  if (!object(raw)) fail("legacy config","must be an object");
  const settings = Object.fromEntries(KEYS.filter(k => raw[k] !== undefined).map(k => [k,clone(raw[k])]));
  if (settings.go2rtc?.sources_file !== undefined) {
    const imported = parse(readFileSync(resolve(settings.go2rtc.sources_file),"utf8")) ?? {};
    settings.go2rtc.streams = { ...(settings.go2rtc.streams ?? {}), ...(imported.streams ?? {}) };
    delete settings.go2rtc.sources_file;
  }
  return validateSettings(settings);
}
function merge(base, patch, path = "") {
  if (!object(patch)) return clone(patch);
  const out = object(base) ? clone(base) : {};
  for (const [key,value] of Object.entries(patch)) {
    if (["__proto__","prototype","constructor"].includes(key)) fail(path || "settings","contains an invalid key");
    // Null removes a camera override (including a whole serial). This restores inherited/default policy.
    if (value === null && (path === "cameras" || path.startsWith("cameras."))) delete out[key];
    else out[key] = object(value) ? merge(out[key],value,path ? `${path}.${key}` : key) : clone(value);
  }
  return out;
}
function atomicWrite(path, doc) {
  mkdirSync(dirname(path),{ recursive:true, mode:0o700 });
  const tmp = `${path}.${randomUUID()}.tmp`;
  let fd;
  try { fd=openSync(tmp,"wx",0o600); writeFileSync(fd,JSON.stringify(doc,null,2)+"\n"); fsyncSync(fd); closeSync(fd); fd=undefined; renameSync(tmp,path); chmodSync(path,0o600); const dirFd=openSync(dirname(path),"r"); try { fsyncSync(dirFd); } finally { closeSync(dirFd); } }
  finally { if (fd !== undefined) closeSync(fd); if (existsSync(tmp)) unlinkSync(tmp); }
}

export class SettingsStore {
  constructor({ dataDir, legacyRaw = {}, logger = console } = {}) {
    if (typeof dataDir !== "string" || !dataDir) throw new TypeError("dataDir is required");
    this.path=resolve(dataDir,"settings.json"); this.logger=logger;
    if (!existsSync(this.path)) {
      const settings=legacySettings(legacyRaw);
      const modesPath=resolve(dataDir,"dual-view-modes.json");
      if (existsSync(modesPath)) {
        const modes=JSON.parse(readFileSync(modesPath,"utf8"));
        map(dual)(modes,"legacy dual view modes");
        settings.cameras ??={};
        for (const [sn,mode] of Object.entries(modes)) settings.cameras[sn]={...(settings.cameras[sn] ?? {}),dual_view:mode};
      }
      atomicWrite(this.path,{ version:1, revision:1, settings:validateSettings(settings) });
    }
    this.load();
    if (KEYS.some(k => Object.hasOwn(legacyRaw,k))) logger?.warn?.("[bridge] Legacy YAML operating settings are deprecated and ignored after initial migration; manage settings through a client.");
  }
  load() {
    const doc=JSON.parse(readFileSync(this.path,"utf8"));
    if (!object(doc) || doc.version !== 1 || !Number.isSafeInteger(doc.revision) || doc.revision < 1 || Object.keys(doc).some(k => !["version","revision","settings"].includes(k))) fail("settings file","has an unsupported format");
    return { revision:doc.revision, settings:validateSettings(doc.settings) };
  }
  // Reads and backups include external source URLs and must require admin authorization.
  publicSnapshot() { return this.load(); }
  previewPatch(input) {
    if (!object(input) || Object.keys(input).some(k => !["revision","settings"].includes(k))) fail("settings request","must contain revision and settings only");
    const { revision, settings }=input;
    const current=this.load();
    if (revision !== current.revision) throw new SettingsConflictError(current.revision);
    if (!object(settings)) fail("settings patch","must be an object");
    if (settings.cameras !== undefined) {
      map((entry,path) => {
        if (entry === null) return;
        if (!object(entry)) fail(path,"must be an object or null");
        for (const [key,value] of Object.entries(entry)) {
          if (!Object.hasOwn(CAMERA_FIELDS,key)) fail(`${path}.${key}`,"is not a supported setting");
          if (value !== null) CAMERA_FIELDS[key](value,`${path}.${key}`);
        }
      })(settings.cameras,"settings.cameras");
    }
    return validateSettings(merge(current.settings,settings));
  }
  patch(input) {
    const next=this.previewPatch(input);
    if (input.revision === Number.MAX_SAFE_INTEGER) fail("revision","has reached its maximum");
    atomicWrite(this.path,{version:1,revision:input.revision+1,settings:next});
    return this.load();
  }
  export() { const snapshot=this.publicSnapshot(); return {format:"eufy-wall-settings",version:1,settings:snapshot.settings}; }
  previewImport(input) {
    if (!object(input) || Object.keys(input).some(k => !["revision","backup"].includes(k))) fail("import request","must contain revision and backup only");
    const { revision, backup }=input;
    if (!object(backup) || backup.format !== "eufy-wall-settings" || backup.version !== 1 || Object.keys(backup).some(k => !["format","version","settings"].includes(k))) fail("backup","has an unsupported format");
    const settings=validateSettings(backup.settings);
    const current=this.load();
    if (revision !== current.revision) throw new SettingsConflictError(current.revision);
    return settings;
  }
  import(input) {
    const settings=this.previewImport(input);
    if (input.revision === Number.MAX_SAFE_INTEGER) fail("revision","has reached its maximum");
    atomicWrite(this.path,{version:1,revision:input.revision+1,settings});
    return this.load();
  }
}
export const createSettingsStore = (options) => new SettingsStore(options);
