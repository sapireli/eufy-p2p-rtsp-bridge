#!/usr/bin/env node
// Explicit offline migration. Import once, then retire YAML operating fields without starting services.
import { readFileSync, existsSync, lstatSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { parseEnv } from "node:util";
import { parse, parseDocument, isMap } from "yaml";
import { SettingsStore, validateSettings } from "./src/settings-store.mjs";
import { validateBootstrap } from "./src/config.mjs";
import { retireLegacyConfig } from "./retire-legacy-config.mjs";

const OPERATING = ["defaults", "cameras", "lan", "go2rtc", "freshness", "stall", "poll_ms"];
const STARTUP = ["eufy", "host", "port", "rtsp_port", "go2rtc_api_port", "self_host", "data_dir", "go2rtc_bin"];
const object = value => value !== null && typeof value === "object" && !Array.isArray(value);
function regular(path) {
  for (let current = resolve(path); current !== dirname(current); current = dirname(current)) {
    if (lstatSync(current).isSymbolicLink()) throw new Error("Migration refuses symlink path components");
  }
  if (!lstatSync(path).isFile()) throw new Error("Migration requires regular files");
}
function legacyRaw(path) {
  regular(path);
  const document = parseDocument(readFileSync(path,"utf8"),{uniqueKeys:true});
  if (document.errors.length || !isMap(document.contents)) throw new Error("Legacy YAML must be a valid mapping without duplicate keys");
  const raw = document.toJS({maxAliasCount:100});
  for (const key of Object.keys(raw)) if (![...OPERATING,...STARTUP].includes(key)) throw new Error(`Unknown legacy key: ${key}`);
  if (raw.eufy !== undefined) {
    if (!object(raw.eufy)) throw new Error("eufy must contain a mapping");
    for (const key of Object.keys(raw.eufy)) if (!["email","password","country"].includes(key)) throw new Error(`Unknown eufy key: ${key}`);
  }
  return raw;
}
function settingsFromLegacy(raw, dataDir, env) {
  const settings = Object.fromEntries(OPERATING.filter(key=>Object.hasOwn(raw,key)).map(key=>[key,structuredClone(raw[key])]));
  const fields = {BRIDGE_GO2RTC_TRANSCODE:"transcode",BRIDGE_GO2RTC_MAX_HEIGHT:"max_height",BRIDGE_GO2RTC_VAAPI_DEVICE:"vaapi_device",BRIDGE_GO2RTC_VAAPI_DECODE:"vaapi_decode"};
  for (const [key,field] of Object.entries(fields)) if (env[key] !== undefined) {
    settings.go2rtc ??= {};
    let value = env[key];
    if (field === "max_height") { if (!/^\d+$/.test(value)) throw new Error("BRIDGE_GO2RTC_MAX_HEIGHT must be a nonnegative integer"); value=Number(value); }
    if (field === "vaapi_decode") {
      if (!["true","false","1","0"].includes(String(value).toLowerCase())) throw new Error("BRIDGE_GO2RTC_VAAPI_DECODE must be true, false, 1 or 0");
      value=["true","1"].includes(String(value).toLowerCase());
    }
    settings.go2rtc[field]=value;
  }
  if (settings.go2rtc?.sources_file !== undefined) {
    const sources=settings.go2rtc.sources_file;
    if (typeof sources !== "string" || !sources.trim()) throw new Error("go2rtc.sources_file must be a nonempty string");
    const sourcePath=resolve(sources);regular(sourcePath);
    const imported=parse(readFileSync(sourcePath,"utf8")) ?? {};
    if (!object(imported)) throw new Error("Legacy sources file must contain a mapping");
    if (imported.streams !== undefined && !object(imported.streams)) throw new Error("Legacy sources streams must be a mapping");
    settings.go2rtc.streams={...(settings.go2rtc.streams ?? {}),...(imported.streams ?? {})};
    delete settings.go2rtc.sources_file;
  }
  const modesPath=resolve(dataDir,"dual-view-modes.json");
  if (existsSync(modesPath)) {
    regular(modesPath);
    const modes=JSON.parse(readFileSync(modesPath,"utf8"));
    if (!object(modes)) throw new Error("Legacy dual view modes must be a mapping");
    settings.cameras ??={};
    for (const [sn,mode] of Object.entries(modes)) settings.cameras[sn]={...(settings.cameras[sn] ?? {}),dual_view:mode};
  }
  return validateSettings(settings);
}

/** Existing settings always win. env contains legacy operating overrides only during this operation. */
export function migrateLegacyConfig({configPath,dataDir,env=process.env}={}) {
  if (typeof configPath!=="string" || !configPath || typeof dataDir!=="string" || !dataDir) throw new Error("configPath and dataDir are required");
  const path=resolve(configPath), directory=resolve(dataDir), storePath=resolve(directory,"settings.json");
  const supportedEnv=new Set(["BRIDGE_GO2RTC_TRANSCODE","BRIDGE_GO2RTC_MAX_HEIGHT","BRIDGE_GO2RTC_VAAPI_DEVICE","BRIDGE_GO2RTC_VAAPI_DECODE","BRIDGE_GO2RTC_API_PORT"]);
  if (!object(env)) throw new Error("env must be an object");
  for (const key of Object.keys(env)) if (key.startsWith("BRIDGE_GO2RTC_") && !supportedEnv.has(key)) throw new Error(`Unsupported legacy operating environment key: ${key}`);
  if (existsSync(resolve(directory,"settings-initialized")) && !existsSync(storePath)) throw new Error("Initialized settings store is missing; restore settings.json from backup");
  const raw=legacyRaw(path);
  validateBootstrap(Object.fromEntries(Object.entries(raw).filter(([key])=>!OPERATING.includes(key))));
  const legacyPolicy=Object.fromEntries(OPERATING.filter(key=>Object.hasOwn(raw,key)).map(key=>[key,structuredClone(raw[key])]));
  if (legacyPolicy.go2rtc?.sources_file !== undefined) {
    if (typeof legacyPolicy.go2rtc.sources_file !== "string" || !legacyPolicy.go2rtc.sources_file.trim()) throw new Error("go2rtc.sources_file must be a nonempty string");
    delete legacyPolicy.go2rtc.sources_file;
  }
  validateSettings(legacyPolicy);
  let created=false,store;
  if (existsSync(storePath)) {
    regular(storePath);
    // Constructor never imports now; validate the existing file and preserve its exact revision.
    store=new SettingsStore({dataDir:directory});
  } else {
    // Validate all known legacy policy before creating the durable store.
    const settings=settingsFromLegacy(raw,directory,env);
    store=new SettingsStore({dataDir:directory,initialSettings:settings});
    created=true;
  }
  const revision=store.load().revision;
  const retirement=retireLegacyConfig({configPath:path,dataDir:directory});
  return {created,revision,retirement};
}

/** Node's literal environment parser never sources scripts or expands shell code. */
export function parseLegacyEnvFile(text) { return parseEnv(text); }
function cli(args) {
  const values={};
  for(let index=0;index<args.length;index+=2) {
    const key=args[index];
    if (!["--config","--data-dir","--env-file"].includes(key)||!args[index+1]||Object.hasOwn(values,key)) throw new Error("Usage: node migrate-legacy-config.mjs --config PATH --data-dir PATH [--env-file PATH]");
    values[key]=args[index+1];
  }
  if (!values["--config"]||!values["--data-dir"]) throw new Error("Usage: node migrate-legacy-config.mjs --config PATH --data-dir PATH [--env-file PATH]");
  let env=process.env;
  if(values["--env-file"]) { const path=resolve(values["--env-file"]);regular(path);env={...env,...parseLegacyEnvFile(readFileSync(path,"utf8"))}; }
  console.log(JSON.stringify(migrateLegacyConfig({configPath:values["--config"],dataDir:values["--data-dir"],env})));
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  try{cli(process.argv.slice(2));}catch(error){console.error(`Legacy config migration failed: ${error.message}`);process.exitCode=1;}
}
