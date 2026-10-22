import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync,writeFileSync,readFileSync,rmSync,statSync,readdirSync,symlinkSync,chmodSync,realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { parse } from "yaml";
import { SettingsStore } from "../src/settings-store.mjs";
import { retireLegacyConfig } from "../retire-legacy-config.mjs";
function fixture(t,yaml='eufy: {email: test@example.com, password: synthetic}\nport: 3000\nlan: {force: true, cidr: 192.168.1.0/24}\n') {
  const dir=realpathSync(mkdtempSync(join(tmpdir(),"wall-retire-")));t.after(()=>rmSync(dir,{recursive:true,force:true}));
  const path=join(dir,"bridge.yaml");writeFileSync(path,yaml,{mode:0o644});
  const store=new SettingsStore({dataDir:dir,legacyRaw:{cameras:{CAM:{dual_view:"split"}}},logger:null});
  return {dir,path,store,retire:()=>retireLegacyConfig({configPath:path,dataDir:dir})};
}
test("retires only operating fields, keeps startup comments/values, private exact backup and unchanged store",t=>{
  const yaml='# Bootstrap login stays private\neufy:\n  email: operator@example.com # account comment\n  password: "do not change this: #x"\n  country: US\nhost: 0.0.0.0 # bind comment\nport: 3000\nrtsp_port: 8554\ngo2rtc_api_port: 1984\nself_host: 127.0.0.1\ndata_dir: /var/lib/eufy-wall\ngo2rtc_bin: /usr/bin/go2rtc\ndefaults: {hold_seconds: 60}\ncameras: {CAM: {mode: always, power_override: always-on}}\nlan: {force: true, cidr: 192.168.1.0/24}\ngo2rtc: {transcode: auto, sources_file: /not/read/by/this/tool}\nfreshness: {enabled: true, max_age_ms: 500}\nstall: {exit_after_ms: 0}\npoll_ms: 0\n';
  const {path,store,retire}=fixture(t,yaml),beforeStore=readFileSync(store.path),beforeStat=statSync(path);
  const result=retire();assert.equal(result.changed,true);assert.equal(result.removed.length,7);
  assert.equal(readFileSync(result.backupPath,"utf8"),yaml);assert.equal(statSync(result.backupPath).mode&0o777,0o600);
  const output=readFileSync(path,"utf8"),raw=parse(output),original=parse(yaml);
  for(const key of ["eufy","host","port","rtsp_port","go2rtc_api_port","self_host","data_dir","go2rtc_bin"])assert.deepEqual(raw[key],original[key]);
  for(const key of result.removed)assert.equal(raw[key],undefined);
  assert.match(output,/# Bootstrap login stays private/);assert.match(output,/# account comment/);assert.match(output,/# bind comment/);
  assert.deepEqual(readFileSync(store.path),beforeStore);assert.equal(statSync(path).mode&0o777,beforeStat.mode&0o777);
  assert.equal(statSync(path).uid,beforeStat.uid);assert.equal(statSync(path).gid,beforeStat.gid);
});
test("second run is a byte-exact no-op with no extra backup",t=>{
  const {dir,path,retire}=fixture(t);retire();const before=readFileSync(path),files=readdirSync(dir),stat=statSync(path);
  const result=retire();assert.equal(result.changed,false);assert.equal(result.backupPath,undefined);
  assert.deepEqual(readFileSync(path),before);assert.deepEqual(readdirSync(dir),files);assert.equal(statSync(path).ino,stat.ino);
});
test("missing or corrupt store refuses retirement without creating/importing or modifying YAML",t=>{
  const {dir,path,store,retire}=fixture(t);const before=readFileSync(path);
  rmSync(store.path);assert.throws(retire,/ENOENT/);assert.deepEqual(readdirSync(dir),["bridge.yaml"]);
  writeFileSync(store.path,'{"version":1,"revision":1,"settings":{"unknown":true}}');
  assert.throws(retire,/supported setting/);assert.deepEqual(readFileSync(path),before);assert.equal(readdirSync(dir).length,2);
  writeFileSync(store.path,'{"version":2,"revision":1,"settings":{}}');assert.throws(retire,/unsupported format/);
  writeFileSync(store.path,'broken json');assert.throws(retire);assert.deepEqual(readFileSync(path),before);
});
test("invalid YAML, duplicate keys and unknown root/nested keys fail without backup",t=>{
  for(const yaml of ['eufy: [broken','port: 3000\nport: 4000\n','unknown: true\n','eufy: {password: x, unexpected: true}\n','lan: {unexpected: true}\n','cameras: {CAM: {enabled: "false"}}\n']) {
    const {dir,path,retire}=fixture(t,yaml);assert.throws(retire);assert.equal(readFileSync(path,"utf8"),yaml);assert.equal(readdirSync(dir).length,2);
  }
});
test("config and store symlinks are rejected without touching targets",t=>{
  for(const target of ["config","store"]) {
    const {dir,path,store,retire}=fixture(t);const linkTarget=join(dir,"original");const originalPath=target==="config"?path:store.path;const bytes=readFileSync(originalPath);
    writeFileSync(linkTarget,bytes);rmSync(originalPath);symlinkSync(linkTarget,originalPath);
    assert.throws(retire,/symlink/);assert.deepEqual(readFileSync(linkTarget),bytes);
  }
});
test("existing restrictive config permissions remain restrictive",t=>{
  const {path,retire}=fixture(t);chmodSync(path,0o640);retire();assert.equal(statSync(path).mode&0o777,0o640);
});
test("CLI requires explicit paths, does not create missing stores, and reports sanitized result",t=>{
  const {dir,path,retire}=fixture(t);const script=fileURLToPath(new URL("../retire-legacy-config.mjs", import.meta.url));
  let result=spawnSync(process.execPath,[script],{encoding:"utf8"});assert.equal(result.status,1);assert.match(result.stderr,/Usage/);
  result=spawnSync(process.execPath,[script,"--config",path,"--data-dir",dir],{encoding:"utf8"});assert.equal(result.status,0);assert.equal(JSON.parse(result.stdout).changed,true);assert.doesNotMatch(result.stdout,/synthetic|test@example.com/);
  result=spawnSync(process.execPath,[script,"--config",path,"--data-dir",dir],{encoding:"utf8"});assert.equal(result.status,0);assert.equal(JSON.parse(result.stdout).changed,false);
});

test("symlink directory components are rejected as well as final files",t=>{
  const {dir,path}=fixture(t);const alias=join(dir,"alias");symlinkSync(dir,alias,"dir");
  assert.throws(()=>retireLegacyConfig({configPath:join(alias,"bridge.yaml"),dataDir:dir}),/symlink/);
  assert.throws(()=>retireLegacyConfig({configPath:path,dataDir:alias}),/symlink/);
});
