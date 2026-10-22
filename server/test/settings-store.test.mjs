import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, readFileSync, statSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SettingsStore, SettingsConflictError, SettingsValidationError, validateSettings } from "../src/settings-store.mjs";
function setup(t, raw = {}) { const dir=mkdtempSync(join(tmpdir(),"wall-settings-")); t.after(()=>rmSync(dir,{recursive:true,force:true})); return {dir,store:new SettingsStore({dataDir:dir,legacyRaw:raw,logger:null})}; }

test("imports legacy operating policy once and excludes startup/account credentials",t=>{
  const {dir,store}=setup(t,{eufy:{email:"secret@example.com",password:"secret"},host:"0.0.0.0",port:3000,data_dir:"/secret",defaults:{hold_seconds:70},cameras:{CAM:{dual_view:"split"}}});
  assert.deepEqual(store.load(),{revision:1,settings:{defaults:{hold_seconds:70},cameras:{CAM:{dual_view:"split"}}}});
  const again=new SettingsStore({dataDir:dir,legacyRaw:{defaults:{hold_seconds:99}},logger:null});
  assert.equal(again.load().settings.defaults.hold_seconds,70);
  assert.doesNotMatch(readFileSync(store.path,"utf8"),/secret|data_dir|port/);
  assert.equal(statSync(store.path).mode & 0o777,0o600);
});

test("imports persisted dual views ahead of YAML only on first migration",t=>{
  const {dir}=setup(t);
  rmSync(join(dir,"settings.json"));
  writeFileSync(join(dir,"dual-view-modes.json"),JSON.stringify({CAM:"pip-br"}));
  const store=new SettingsStore({dataDir:dir,legacyRaw:{cameras:{CAM:{name:"Door",dual_view:"split"}}},logger:null});
  assert.deepEqual(store.load().settings.cameras.CAM,{name:"Door",dual_view:"pip-br"});
  store.patch({revision:1,settings:{cameras:{CAM:{dual_view:"single"}}}});
  assert.equal(new SettingsStore({dataDir:dir,logger:null}).load().settings.cameras.CAM.dual_view,"single");
});

test("embeds legacy external streams without persisting file paths",t=>{
  const {dir}=setup(t); rmSync(join(dir,"settings.json"));
  const sources=join(dir,"sources.yaml"); writeFileSync(sources,'streams:\n  external: rtsp://user:pass@camera.local/live\n');
  const store=new SettingsStore({dataDir:dir,legacyRaw:{go2rtc:{sources_file:sources,transcode:"auto"}},logger:null});
  rmSync(sources);
  assert.equal(store.load().settings.go2rtc.streams.external,"rtsp://user:pass@camera.local/live");
  assert.equal(store.load().settings.go2rtc.sources_file,undefined);
});

test("nested patch preserves siblings and null camera overrides restore inheritance",t=>{
  const {store}=setup(t,{cameras:{A:{name:"Door",dual_view:"split"},B:{enabled:false}},lan:{force:true,cidr:"192.168.1.0/24"}});
  let snapshot=store.patch({revision:1,settings:{cameras:{A:{dual_view:null}},lan:{station_addresses:{BASE:"192.168.1.20"}}}});
  assert.deepEqual(snapshot.settings.cameras,{A:{name:"Door"},B:{enabled:false}});
  assert.equal(snapshot.settings.lan.force,true);
  snapshot=store.patch({revision:2,settings:{cameras:{B:null}}});
  assert.equal(snapshot.settings.cameras.B,undefined);
  assert.equal(snapshot.revision,3);
});

test("stale concurrent writer cannot replace settings and failures preserve bytes",t=>{
  const {dir,store}=setup(t); const second=new SettingsStore({dataDir:dir,logger:null});
  store.patch({revision:1,settings:{poll_ms:1000}});
  const before=readFileSync(store.path,"utf8");
  assert.throws(()=>second.patch({revision:1,settings:{poll_ms:2000}}),SettingsConflictError);
  assert.throws(()=>store.patch({revision:2,settings:{poll_ms:"2000"}}),SettingsValidationError);
  assert.equal(readFileSync(store.path,"utf8"),before);
  assert.deepEqual(readdirSync(dir),["settings.json"]);
});

test("backup restores complete settings including external sources but never login data",t=>{
  const {store}=setup(t,{eufy:{password:"not-in-backup"},defaults:{dual_view:"split"},go2rtc:{streams:{x:["rtsp://user:pass@host/live"]}}});
  const backup=store.export();
  assert.doesNotMatch(JSON.stringify(backup),/not-in-backup/);
  store.patch({revision:1,settings:{defaults:{dual_view:"single"}}});
  assert.equal(store.import({revision:2,backup}).settings.defaults.dual_view,"split");
  assert.deepEqual(store.load().settings.go2rtc.streams,backup.settings.go2rtc.streams);
  assert.throws(()=>store.import({revision:2,backup}),SettingsConflictError);
  assert.throws(()=>store.import({revision:3,backup:{...backup,settings:{eufy:{password:"bad"}}}}),SettingsValidationError);
});

test("strict validation rejects coercion, unknown settings, invalid enums, paths and unsafe keys",t=>{
  const {store}=setup(t);
  for (const settings of [
    {unknown:true},{cameras:{CAM:{enabled:"false"}}},{cameras:{CAM:{mode:"sometimes"}}},
    {cameras:{CAM:{unknown:null}}},{go2rtc:{sources_file:"/etc/passwd"}},{go2rtc:{vaapi_device:"/tmp/device"}},
    {go2rtc:{streams:{x:"http://host/"}}},{freshness:{mode:"fast"}},{freshness:{max_age_ms:99}},{freshness:{max_age_ms:60001}},{lan:{force:true}},
    {lan:{cidr:"999.1.1.1/24"}},{stall:{backoff_ms:[]}},{poll_ms:-1},{poll_ms:NaN},{poll_ms:2147483648},{stall:{backoff_ms:[2147483648]}},
    JSON.parse('{"cameras":{"__proto__":{"name":"bad"}}}'),
  ]) assert.throws(()=>store.patch({revision:1,settings}),SettingsValidationError,JSON.stringify(settings));
  assert.equal(store.load().revision,1);
});

test("stored corruption and future versions fail closed rather than reimporting YAML",t=>{
  const {dir,store}=setup(t);
  writeFileSync(store.path,JSON.stringify({version:2,revision:1,settings:{}}));
  assert.throws(()=>new SettingsStore({dataDir:dir,legacyRaw:{poll_ms:500},logger:null}),SettingsValidationError);
  assert.equal(JSON.parse(readFileSync(store.path)).version,2);
});

test("validator accepts supported runtime operating fields without changing types",()=>{
  const settings={defaults:{quality:null,dual_view:null,hold_seconds:60,motion_events:["motion"]},cameras:{CAM:{power_override:"always-on",mode:"always",codec:"h265",transcode:"auto",vaapi_decode:true,quality:"Max"}},lan:{force:true,cidr:"192.168.23.0/24",station_addresses:{BASE:"192.168.23.10"},upgrade:{enabled:true,interval_ms:120000,window_ms:6000,stable_ms:15000,initial_window_ms:14000,max_backoff_ms:900000}},go2rtc:{max_height:0,vaapi_device:"",vaapi_decode:true,transcode:"never"},freshness:{enabled:true,mode:"observe",max_age_ms:500},stall:{stall_ms:5000,startup_stall_ms:30000,gap_ms:45000,exit_after_ms:0,recreate_client_after:1,backoff_ms:[1000,2000]},poll_ms:0};
  assert.deepEqual(validateSettings(settings),settings);
});


test("preview validates updates and restore without mutating durable state",t=>{
  const {store}=setup(t,{defaults:{hold_seconds:60}});
  assert.equal(store.previewPatch({revision:1,settings:{defaults:{hold_seconds:90}}}).defaults.hold_seconds,90);
  const backup=store.export(); backup.settings.defaults.hold_seconds=120;
  assert.equal(store.previewImport({revision:1,backup}).defaults.hold_seconds,120);
  assert.equal(store.load().revision,1);
  assert.equal(store.load().settings.defaults.hold_seconds,60);
  assert.throws(()=>store.previewPatch({revision:0,settings:{}}),SettingsConflictError);
  assert.throws(()=>store.previewImport({revision:0,backup}),SettingsConflictError);
});
