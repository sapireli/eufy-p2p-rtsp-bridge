import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync,readFileSync,writeFileSync,rmSync,statSync,readdirSync,realpathSync,symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { join,resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { SettingsStore } from "../src/settings-store.mjs";
import { restoreSettingsBackup } from "../restore-settings.mjs";
function fixture(t) {
  const dir=realpathSync(mkdtempSync(join(tmpdir(),"wall-restore-")));t.after(()=>rmSync(dir,{recursive:true,force:true}));
  const store=new SettingsStore({dataDir:dir,initialSettings:{cameras:{CAM:{mode:"on_motion"}},go2rtc:{streams:{external:"rtsp://user:password@host/live"}}}});
  const backupPath=join(dir,"export.json");writeFileSync(backupPath,JSON.stringify(store.export()));
  return {dir,store,backupPath,restore:(replace=false)=>restoreSettingsBackup({dataDir:dir,backupPath,replace})};
}
test("offline export restore recovers a deleted initialized store exactly",t=>{
  const {dir,store,restore}=fixture(t);const exported=store.export(),owner=statSync(store.path),marker=readFileSync(join(dir,"settings-initialized"));
  rmSync(store.path);assert.throws(()=>new SettingsStore({dataDir:dir}),/restore settings/);
  const result=restore();assert.equal(result.restored,true);assert.equal(result.revision,1);
  assert.deepEqual(new SettingsStore({dataDir:dir}).export(),exported);
  assert.equal(statSync(store.path).mode&0o777,0o600);assert.equal(statSync(store.path).uid,owner.uid);assert.equal(statSync(store.path).gid,owner.gid);
  assert.deepEqual(readFileSync(join(dir,"settings-initialized")),marker);
});
test("existing stores need explicit replace and get private exact rollback backup",t=>{
  const {store,restore}=fixture(t);store.patch({revision:1,settings:{cameras:{CAM:{mode:"on_demand"}}}});const original=readFileSync(store.path),owner=statSync(store.path);
  assert.throws(()=>restore(),/--replace/);assert.deepEqual(readFileSync(store.path),original);
  const result=restore(true);assert.equal(result.revision,3);assert.deepEqual(readFileSync(result.previousBackup),original);
  assert.equal(statSync(result.previousBackup).mode&0o777,0o600);assert.equal(statSync(result.previousBackup).uid,owner.uid);assert.equal(statSync(result.previousBackup).gid,owner.gid);
  assert.equal(store.load().settings.cameras.CAM.mode,"on_motion");
});
test("corrupt store recovery retains corrupt bytes and starts at revision one",t=>{
  const {store,restore}=fixture(t);writeFileSync(store.path,'corrupt bytes with private data');
  const result=restore(true);assert.equal(result.revision,1);assert.equal(readFileSync(result.previousBackup,"utf8"),'corrupt bytes with private data');assert.equal(store.load().revision,1);
});
test("invalid exports cannot modify store, create marker or make backups",t=>{
  const {dir,store,backupPath,restore}=fixture(t);const original=readFileSync(store.path);
  for(const content of ['bad json',JSON.stringify({format:"wrong",version:1,settings:{}}),JSON.stringify({format:"eufy-wall-settings",version:1,settings:{eufy:{password:"secret"}}}),JSON.stringify({format:"eufy-wall-settings",version:1,settings:{},unknown:true})]) {
    writeFileSync(backupPath,content);const files=readdirSync(dir);assert.throws(()=>restore(true));assert.deepEqual(readdirSync(dir),files);assert.deepEqual(readFileSync(store.path),original);
  }
});
test("fresh restore creates private marker and preserves data directory ownership",t=>{
  const {dir,store,restore}=fixture(t);rmSync(store.path);rmSync(join(dir,"settings-initialized"));const owner=statSync(dir);
  restore();for(const name of ["settings.json","settings-initialized"]) {const stat=statSync(join(dir,name));assert.equal(stat.mode&0o777,0o600);assert.equal(stat.uid,owner.uid);assert.equal(stat.gid,owner.gid);}
});
test("restore refuses symlink inputs, store, marker and parent directories",t=>{
  for(const kind of ["backup","store","marker","directory"]) {
    const {dir,store,backupPath,restore}=fixture(t);let path=kind==="backup"?backupPath:kind==="store"?store.path:join(dir,"settings-initialized");
    if(kind==="directory") {const alias=join(dir,"alias");symlinkSync(dir,alias,"dir");assert.throws(()=>restoreSettingsBackup({dataDir:alias,backupPath,replace:true}),/symlink/);continue;}
    const bytes=readFileSync(path);rmSync(path);symlinkSync(join(dir,"missing-target"),path);assert.throws(()=>restore(true),/symlink/);
  }
});
test("CLI requires paths and emits only sanitized restore metadata",t=>{
  const {dir,store,backupPath}=fixture(t);const script=fileURLToPath(new URL("../restore-settings.mjs", import.meta.url));
  let result=spawnSync(process.execPath,[script],{encoding:"utf8"});assert.equal(result.status,1);assert.match(result.stderr,/Usage/);
  rmSync(store.path);result=spawnSync(process.execPath,[script,"--data-dir",dir,"--backup",backupPath],{encoding:"utf8"});assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).restored,true);assert.doesNotMatch(result.stdout+result.stderr,/password@|rtsp:/);
});

test("malformed private backup contents never appear in CLI errors",t=>{
  const {dir,backupPath}=fixture(t);writeFileSync(backupPath,'{"settings": "super-secret-password@host", broken');
  const result=spawnSync(process.execPath,[fileURLToPath(new URL("../restore-settings.mjs", import.meta.url)),"--data-dir",dir,"--backup",backupPath,"--replace"],{encoding:"utf8"});
  assert.equal(result.status,1);assert.match(result.stderr,/Invalid settings backup JSON/);assert.doesNotMatch(result.stderr+result.stdout,/super-secret|password@/);
});
