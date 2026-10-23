import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,mkdtempSync,rmSync,writeFileSync,existsSync,unlinkSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {parse} from 'yaml';
import {loadConfig} from '../src/config.mjs';
import {SettingsStore} from '../src/settings-store.mjs';

function fixture(t){
  const dir=mkdtempSync(join(tmpdir(),'wall-bootstrap-'));
  t.after(()=>rmSync(dir,{recursive:true,force:true}));
  const dataDir=join(dir,'data');
  return {dir,dataDir,path:join(dir,'bootstrap.yaml'),env:{EUFY_EMAIL:'synthetic@example.com',EUFY_PASSWORD:'synthetic',BRIDGE_DATA_DIR:dataDir}};
}

test('fresh install bootstrap creates empty operating store with hardware-first defaults',t=>{
  const f=fixture(t);const path=new URL('../bootstrap.example.yaml',import.meta.url);
  const bootstrap=parse(readFileSync(path,'utf8'));
  const startupKeys=new Set(['host','port','rtsp_port','go2rtc_api_port','self_host','data_dir','go2rtc_bin']);
  for(const key of Object.keys(bootstrap))assert.ok(startupKeys.has(key),`Operating field ${key} must be set through a client`);
  const {cfg,settingsStore}=loadConfig({configPath:path,env:f.env});
  assert.deepEqual(settingsStore.load(),{revision:1,settings:{}});
  assert.equal(cfg.email,'synthetic@example.com');assert.equal(cfg.go2rtcVaapiDecode,true);
  assert.equal(cfg.host,'0.0.0.0');assert.equal(cfg.port,3000);
});

test('legacy YAML operating settings fail with migration instructions before store creation',t=>{
  const fields={defaults:{hold_seconds:90},cameras:{CAM:{power_override:'always-on'}},lan:{cidr:'192.0.2.0/24',force:true},go2rtc:{transcode:'auto'},freshness:{mode:'observe'},stall:{stall_ms:5000},poll_ms:1000};
  for(const [key,value] of Object.entries(fields)){
    const f=fixture(t);writeFileSync(f.path,JSON.stringify({[key]:value}));
    assert.throws(()=>loadConfig({configPath:f.path,env:f.env}),error=>/migrat/i.test(error.message)&&error.message.includes(key));
    assert.equal(existsSync(join(f.dataDir,'settings.json')),false,`${key} must not create a store before rejecting legacy input`);
  }
});

test('retired operating environment overrides fail before creating a store',t=>{
  for(const key of ['BRIDGE_GO2RTC_TRANSCODE','BRIDGE_GO2RTC_MAX_HEIGHT','BRIDGE_GO2RTC_VAAPI_DEVICE','BRIDGE_GO2RTC_VAAPI_DECODE']){
    const f=fixture(t);writeFileSync(f.path,'host: 127.0.0.1\n');
    assert.throws(()=>loadConfig({configPath:f.path,env:{...f.env,[key]:'legacy'}}),error=>/migrat|settings/i.test(error.message)&&error.message.includes(key));
    assert.equal(existsSync(join(f.dataDir,'settings.json')),false);
  }
});

test('bootstrap supplies startup options while persistent JSON exclusively controls operations',t=>{
  const f=fixture(t);
  writeFileSync(f.path,'host: 127.0.0.1\nport: 3101\n');
  const store=new SettingsStore({dataDir:f.dataDir,initialSettings:{lan:{cidr:'192.0.2.0/24',force:true},go2rtc:{transcode:'auto',vaapi_decode:true},cameras:{CAM:{mode:'on_motion',power_override:'battery'}}}});
  const {cfg,settingsStore}=loadConfig({configPath:f.path,env:f.env});
  assert.equal(cfg.host,'127.0.0.1');assert.equal(cfg.port,3101);assert.equal(cfg.email,'synthetic@example.com');
  assert.equal(cfg.lan.force,true);assert.equal(cfg.go2rtcTranscode,'auto');assert.equal(cfg.go2rtcVaapiDecode,true);assert.equal(cfg.cameras.CAM.powerOverride,'battery');
  store.patch({revision:1,settings:{cameras:{CAM:{power_override:'always-on',mode:'always'}}}});
  const reloaded=loadConfig({configPath:f.path,env:f.env});
  assert.equal(reloaded.cfg.cameras.CAM.powerOverride,'always-on');assert.equal(reloaded.cfg.cameras.CAM.mode,'always');
  assert.equal(reloaded.settingsStore.load().revision,2);
  assert.equal(JSON.stringify(settingsStore.export()).includes('synthetic@example.com'),false);
});

test('deleting settings store never causes automatic legacy YAML reimport',t=>{
  const f=fixture(t);writeFileSync(f.path,'host: 127.0.0.1\n');
  const store=new SettingsStore({dataDir:f.dataDir,initialSettings:{cameras:{CAM:{power_override:'always-on'}}}});
  assert.equal(store.load().settings.cameras.CAM.power_override,'always-on');
  unlinkSync(store.path);
  writeFileSync(f.path,'host: 127.0.0.1\ncameras: { CAM: { power_override: battery } }\n');
  assert.throws(()=>loadConfig({configPath:f.path,env:f.env}),/migrat/i);
  assert.equal(existsSync(store.path),false);
  writeFileSync(f.path,'host: 127.0.0.1\n');
  assert.throws(()=>loadConfig({configPath:f.path,env:f.env}),/initialized.*missing.*restore.*backup/i);
  assert.equal(existsSync(store.path),false,'missing initialized settings must not be replaced by defaults');
});

test('missing bootstrap uses startup environment and empty settings in requested data directory',t=>{
  const f=fixture(t);
  const {cfg,settingsStore}=loadConfig({configPath:f.path,env:{...f.env,EUFY_COUNTRY:'DE',BRIDGE_HOST:'127.0.0.1',BRIDGE_PORT:'3201'}});
  assert.equal(cfg.country,'DE');assert.equal(cfg.host,'127.0.0.1');assert.equal(cfg.port,3201);assert.equal(cfg.dataDir,f.dataDir);
  assert.deepEqual(settingsStore.load(),{revision:1,settings:{}});assert.equal(existsSync(join(f.dataDir,'settings.json')),true);
  assert.deepEqual(cfg.cameras,{});assert.equal(cfg.lan.force,false);
});

test('bootstrap login remains supported and environment credentials win',t=>{
  const f=fixture(t);writeFileSync(f.path,'eufy: { email: bootstrap@example.com, password: bootstrap-private, country: US }\n');
  const {cfg,settingsStore}=loadConfig({configPath:f.path,env:{...f.env,EUFY_COUNTRY:'DE'}});
  assert.equal(cfg.email,'synthetic@example.com');assert.equal(cfg.password,'synthetic');assert.equal(cfg.country,'DE');
  assert.deepEqual(settingsStore.load().settings,{});
  assert.equal(JSON.stringify(settingsStore.export()).includes('bootstrap-private'),false);
});

test('existing settings store does not hide forbidden or misspelled bootstrap settings',t=>{
  const f=fixture(t);const store=new SettingsStore({dataDir:f.dataDir,initialSettings:{go2rtc:{transcode:'auto'}}});
  for(const text of ['cameras: { CAM: { mode: always } }\n','go2trc: { transcode: never }\n']){
    writeFileSync(f.path,text);assert.throws(()=>loadConfig({configPath:f.path,env:f.env}),/bootstrap|migrat|unsupported|unknown/i);
    assert.deepEqual(store.load(),{revision:1,settings:{go2rtc:{transcode:'auto'}}});
  }
});
