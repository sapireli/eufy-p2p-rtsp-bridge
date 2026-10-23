import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync,writeFileSync,readFileSync,rmSync,readdirSync,realpathSync,mkdirSync,existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join,resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { parse } from "yaml";
import { SettingsStore } from "../src/settings-store.mjs";
import { migrateLegacyConfig,parseLegacyEnvFile } from "../migrate-legacy-config.mjs";
function fixture(t,yaml='eufy: {email: test@example.com, password: synthetic}\nport: 3000\ncameras: {CAM: {name: Door, dual_view: split}}\n') {
  const dir=realpathSync(mkdtempSync(join(tmpdir(),"wall-explicit-migrate-")));t.after(()=>rmSync(dir,{recursive:true,force:true}));
  const path=join(dir,"bridge.yaml");writeFileSync(path,yaml);return {dir,path,migrate:(env={})=>migrateLegacyConfig({configPath:path,dataDir:dir,env})};
}
test("explicit migration imports YAML, external streams, saved dual modes and environment exactly once",t=>{
  const {dir,path,migrate}=fixture(t);const sources=join(dir,"external.yaml");
  writeFileSync(sources,'streams: {wyze: "rtsp://user:password@camera/live"}\napi: {listen: ":9999"}\nwyze: {username: ignored, password: not-imported}\n');
  writeFileSync(path,readFileSync(path,"utf8")+`go2rtc: {transcode: never, sources_file: ${JSON.stringify(sources)}}\n`);
  writeFileSync(join(dir,"dual-view-modes.json"),JSON.stringify({CAM:"pip-br"}));
  const result=migrate({EUFY_PASSWORD:"not-stored",BRIDGE_GO2RTC_TRANSCODE:"auto",BRIDGE_GO2RTC_MAX_HEIGHT:"1080",BRIDGE_GO2RTC_VAAPI_DECODE:"false"});
  assert.equal(result.created,true);assert.equal(result.revision,1);assert.equal(result.retirement.changed,true);
  const snapshot=new SettingsStore({dataDir:dir}).load();
  assert.equal(snapshot.settings.cameras.CAM.dual_view,"pip-br");assert.equal(snapshot.settings.cameras.CAM.name,"Door");
  assert.deepEqual(snapshot.settings.go2rtc,{transcode:"auto",max_height:1080,vaapi_decode:false,streams:{wyze:"rtsp://user:password@camera/live"}});
  assert.doesNotMatch(readFileSync(join(dir,"settings.json"),"utf8"),/not-stored|synthetic|test@example.com|sources_file|not-imported|9999/);
  assert.deepEqual(parse(readFileSync(path,"utf8")),{eufy:{email:"test@example.com",password:"synthetic"},port:3000});
  assert.equal(readFileSync(result.retirement.backupPath,"utf8").includes("sources_file"),true);
  const second=migrate({BRIDGE_GO2RTC_TRANSCODE:"never"});assert.equal(second.created,false);assert.equal(second.retirement.changed,false);
  assert.deepEqual(new SettingsStore({dataDir:dir}).load(),snapshot);
});
test("existing store/revision remains authoritative and no missing legacy sources are read",t=>{
  const {dir,path,migrate}=fixture(t);const store=new SettingsStore({dataDir:dir,initialSettings:{defaults:{hold_seconds:120}}});
  store.patch({revision:1,settings:{cameras:{CAM:{dual_view:"single"}}}});const before=readFileSync(store.path);
  writeFileSync(path,readFileSync(path,"utf8")+'go2rtc: {sources_file: /missing/source-file}\n');
  writeFileSync(join(dir,"dual-view-modes.json"),'broken json');
  const result=migrate({BRIDGE_GO2RTC_TRANSCODE:"invalid-not-imported"});
  assert.equal(result.created,false);assert.equal(result.revision,2);assert.equal(result.retirement.changed,true);
  assert.deepEqual(readFileSync(store.path),before);
});
test("corrupt stores and marked missing stores fail closed without YAML changes",t=>{
  const {dir,path,migrate}=fixture(t);const before=readFileSync(path);
  writeFileSync(join(dir,"settings.json"),'broken');assert.throws(migrate);assert.deepEqual(readFileSync(path),before);
  rmSync(join(dir,"settings.json"));writeFileSync(join(dir,"settings-initialized"),'{}');
  assert.throws(migrate,/restore settings.json/);assert.deepEqual(readFileSync(path),before);
  assert.equal(readdirSync(dir).includes("settings.json"),false);
});
test("invalid legacy policy/env is rejected before creating store or altering input",t=>{
  for(const [yaml,env] of [['port: 99999\n',{}],['eufy: {password: 123}\n',{}],['host: []\n',{}],['unknown: true\n',{}],['lan: {unknown: true}\n',{}],['go2rtc: {transcode: always}\n',{BRIDGE_GO2RTC_VAAPI_DECODE:"maybe"}],['port: 3000\n',{BRIDGE_GO2RTC_MAX_HEIGHT:"no"}],['port: 3000\n',{BRIDGE_GO2RTC_UNKNOWN:"x"}]]) {
    const {dir,path,migrate}=fixture(t,yaml);assert.throws(()=>migrate(env));assert.deepEqual(readdirSync(dir),["bridge.yaml"]);assert.equal(readFileSync(path,"utf8"),yaml);
  }
});
test("literal env parser preserves password punctuation and never executes or expands",()=>{
  const result=parseLegacyEnvFile(`# comment\nEUFY_PASSWORD='punctuation! #with spaces' # comment\nexport BRIDGE_GO2RTC_TRANSCODE="auto" # comment\nEUFY_EMAIL=operator@example.com # trailing comment\nLITERAL=$(touch /tmp/never-run)\n`);
  assert.equal(result.EUFY_PASSWORD,"punctuation! #with spaces");assert.equal(result.BRIDGE_GO2RTC_TRANSCODE,"auto");assert.equal(result.EUFY_EMAIL,"operator@example.com");assert.equal(result.LITERAL,"$(touch /tmp/never-run)");
  assert.equal(Object.keys(parseLegacyEnvFile("source /tmp/file")).length,0);
});
test("CLI imports quoted env values without modifying env file or emitting credentials",t=>{
  const {dir,path}=fixture(t);const envPath=join(dir,"bridge.env");const env='EUFY_PASSWORD="synthetic!with punctuation"\nBRIDGE_GO2RTC_TRANSCODE="auto"\nBRIDGE_GO2RTC_API_PORT=1984\n';writeFileSync(envPath,env);
  const result=spawnSync(process.execPath,[fileURLToPath(new URL("../migrate-legacy-config.mjs", import.meta.url)),"--config",path,"--data-dir",dir,"--env-file",envPath],{encoding:"utf8"});
  assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).created,true);assert.doesNotMatch(result.stdout+result.stderr,/synthetic|operator@example/);
  assert.equal(readFileSync(envPath,"utf8"),env);assert.equal(new SettingsStore({dataDir:dir}).load().settings.go2rtc.transcode,"auto");
});

test('upgrade preserves untracked relative source files until the offline importer reads them', t => {
  const available=spawnSync('rsync',['--version']);
  if(available.error?.code==='ENOENT'){t.skip('rsync unavailable');return;}
  assert.equal(available.status,0);
  const {dir,path}=fixture(t,'port: 3000\ngo2rtc: {sources_file: private-source.yaml}\n');
  const incoming=join(dir,'incoming'),working=join(dir,'old-service'),control=join(dir,'delete-control');
  for(const p of [incoming,working,control])mkdirSync(p);
  writeFileSync(join(incoming,'new-code.mjs'),'// replacement source\n');
  const source='streams: {camera: "rtsp://192.0.2.20/live"}\n';
  for(const p of [working,control])writeFileSync(join(p,'private-source.yaml'),source);
  const previous=spawnSync('rsync',['-a','--delete',`${incoming}/`,`${control}/`]);
  assert.equal(previous.status,0);assert.equal(existsSync(join(control,'private-source.yaml')),false,'old update removed the source before migration');
  const preserved=spawnSync('rsync',['-a','--exclude','node_modules','--exclude','data','--exclude','spike-out',`${incoming}/`,`${working}/`]);
  assert.equal(preserved.status,0);assert.equal(readFileSync(join(working,'private-source.yaml'),'utf8'),source);
  const cli=fileURLToPath(new URL('../migrate-legacy-config.mjs',import.meta.url));
  const result=spawnSync(process.execPath,[cli,'--config',path,'--data-dir',dir],{cwd:working,encoding:'utf8',env:{...process.env,BRIDGE_GO2RTC_TRANSCODE:'never'}});
  assert.equal(result.status,0,result.stderr);
  assert.equal(new SettingsStore({dataDir:dir}).load().settings.go2rtc.streams.camera,'rtsp://192.0.2.20/live');
  assert.deepEqual(parse(readFileSync(path,'utf8')),{port:3000});
});
