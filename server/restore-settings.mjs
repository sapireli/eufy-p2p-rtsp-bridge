#!/usr/bin/env node
// Explicit offline recovery for exported settings, including an initialized store that cannot boot.
import { constants, lstatSync, readFileSync, openSync, closeSync, writeFileSync, fsyncSync, fchmodSync, fchownSync, renameSync, unlinkSync, chownSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { randomUUID } from "node:crypto";
import { SettingsStore, validateSettings } from "./src/settings-store.mjs";
function entryExists(path) { try { lstatSync(path); return true; } catch(error) { if(error.code==="ENOENT")return false; throw error; } }
const object=value=>value!==null&&typeof value==="object"&&!Array.isArray(value);
function checkPath(path,kind="file") {
  for(let current=path;current!==dirname(current);current=dirname(current)) {
    if(lstatSync(current).isSymbolicLink()) throw new Error("Restore refuses symlink paths");
  }
  const stat=lstatSync(path);
  if(kind==="directory"?!stat.isDirectory():!stat.isFile()) throw new Error(`Restore requires a regular ${kind}`);
  return stat;
}
function writePrivate(path,bytes,owner) {
  const fd=openSync(path,"wx",0o600);
  try {
    if(process.getuid?.()===0) fchownSync(fd,owner.uid,owner.gid);
    fchmodSync(fd,0o600);writeFileSync(fd,bytes);fsyncSync(fd);
  }finally{closeSync(fd);}
}
export function restoreSettingsBackup({dataDir,backupPath,replace=false}={}) {
  if(typeof dataDir!=="string"||!dataDir||typeof backupPath!=="string"||!backupPath||typeof replace!=="boolean") throw new Error("dataDir, backupPath and a boolean replace are required");
  const directory=resolve(dataDir),source=resolve(backupPath),path=resolve(directory,"settings.json"),marker=resolve(directory,"settings-initialized");
  checkPath(source);
  let exported;
  try { exported=JSON.parse(readFileSync(source,"utf8")); } catch { throw new Error("Invalid settings backup JSON"); }
  if(!object(exported)||exported.format!=="eufy-wall-settings"||exported.version!==1||Object.keys(exported).some(key=>!["format","version","settings"].includes(key))) throw new Error("Unsupported settings backup format");
  const settings=validateSettings(exported.settings);
  const directoryStat=checkPath(directory,"directory");
  const markerExists=entryExists(marker),markerStat=markerExists?checkPath(marker):undefined;
  let revision=1,original,previousStat;
  const existed=entryExists(path);
  if(existed) {
    previousStat=checkPath(path);
    if(!replace) throw new Error("Settings store exists; pass --replace to back it up and restore explicitly");
    original=readFileSync(path);
    try {
      const current=SettingsStore.prototype.load.call({path});
      if(current.revision===Number.MAX_SAFE_INTEGER) throw new Error("Settings revision is exhausted");
      revision=current.revision+1;
    }catch(error){
      if(error.message==="Settings revision is exhausted") throw error;
      revision=1; // Corrupt stores have no trustworthy revision; retain their exact bytes in the backup.
    }
  }
  const owner=previousStat??markerStat??directoryStat;
  const temporary=`${path}.${randomUUID()}.tmp`;
  let previousBackup;
  if(existed) {
    previousBackup=`${path}.before-offline-restore-${new Date().toISOString().replaceAll(":","-")}-${randomUUID()}.bak`;
    writePrivate(previousBackup,original,owner);
  }
  try {
    writePrivate(temporary,JSON.stringify({version:1,revision,settings},null,2)+"\n",owner);
    // Refuse a concurrent replacement or creation rather than overwriting a newly changed store.
    if(existed) {
      const now=checkPath(path);
      if(now.ino!==previousStat.ino||now.dev!==previousStat.dev||now.mtimeMs!==previousStat.mtimeMs||!readFileSync(path).equals(original)) throw new Error("Settings changed during offline restore");
    }else if(entryExists(path)) throw new Error("Settings appeared during offline restore");
    renameSync(temporary,path);
    const dirFd=openSync(directory,constants.O_RDONLY);
    try{fsyncSync(dirFd);}finally{closeSync(dirFd);}
    new SettingsStore({dataDir:directory});
    if(!markerExists&&process.getuid?.()===0) chownSync(marker,owner.uid,owner.gid);
  }finally{
    try{unlinkSync(temporary);}catch(error){if(error.code!=="ENOENT")throw error;}
  }
  return {restored:true,revision,...(previousBackup?{previousBackup}:{})};
}
function cli(args) {
  const values={};let replace=false;
  for(let index=0;index<args.length;index++) {
    const key=args[index];
    if(key==="--replace") {if(replace)throw new Error("Duplicate --replace");replace=true;continue;}
    if(!["--data-dir","--backup"].includes(key)||!args[index+1]||Object.hasOwn(values,key)) throw new Error("Usage: node restore-settings.mjs --data-dir PATH --backup PATH [--replace]");
    values[key]=args[++index];
  }
  if(!values["--data-dir"]||!values["--backup"])throw new Error("Usage: node restore-settings.mjs --data-dir PATH --backup PATH [--replace]");
  console.log(JSON.stringify(restoreSettingsBackup({dataDir:values["--data-dir"],backupPath:values["--backup"],replace})));
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  try{cli(process.argv.slice(2));}catch(error){console.error(`Settings restore failed: ${error.message}`);process.exitCode=1;}
}
