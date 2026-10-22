import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {parse} from 'yaml';
import {loadConfig} from '../src/config.mjs';

test('fresh install bootstrap creates an empty operating store and keeps hardware-first defaults', t => {
  const dataDir=mkdtempSync(join(tmpdir(),'wall-bootstrap-'));
  t.after(()=>rmSync(dataDir,{recursive:true,force:true}));
  const path=new URL('../bootstrap.example.yaml',import.meta.url);
  const bootstrap=parse(readFileSync(path,'utf8'));
  const startupKeys=new Set(['host','port','rtsp_port','go2rtc_api_port','self_host','data_dir','go2rtc_bin']);
  for(const key of Object.keys(bootstrap))assert.ok(startupKeys.has(key),`Operating field ${key} must be set through a client`);
  const {cfg,settingsStore}=loadConfig({configPath:path,persistent:true,env:{EUFY_EMAIL:'synthetic@example.com',EUFY_PASSWORD:'synthetic',BRIDGE_DATA_DIR:dataDir}});
  assert.deepEqual(settingsStore.load(),{revision:1,settings:{}});
  assert.equal(cfg.email,'synthetic@example.com');
  assert.equal(cfg.go2rtcVaapiDecode,true);
  assert.equal(cfg.host,'0.0.0.0');
  assert.equal(cfg.port,3000);
  settingsStore.patch({revision:1,settings:{lan:{cidr:'192.0.2.0/24',force:true}}});
  const reloaded=loadConfig({configPath:path,persistent:true,env:{EUFY_EMAIL:'synthetic@example.com',EUFY_PASSWORD:'synthetic',BRIDGE_DATA_DIR:dataDir}});
  assert.equal(reloaded.cfg.lan.force,true);
  assert.equal(reloaded.cfg.lan.cidr,'192.0.2.0/24');
});
