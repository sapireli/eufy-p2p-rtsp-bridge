import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parse } from 'yaml';
import { buildConfig, isValidCidr } from '../src/config.mjs';

// These fixtures exercise normalized in-memory settings, not the retired YAML
// operating-config reader. Only loadConfig owns startup files and persistence.
const fromYaml = (text, env = {}) => buildConfig({env, raw:parse(text)});
const credentials = 'eufy: { email: a@b.c, password: yamlpw, country: US }\n';

test('normalizes stored settings, applies defaults and startup credential overrides', () => {
  const {cfg}=fromYaml(`${credentials}
lan: { cidr: 10.0.0.0/8, force: true, station_addresses: { T8010X: 10.0.0.5 } }
defaults: { quality: "Full HD (1080P)" }
cameras:
  T8410X: { name: Garage }
  T8214X: { dual_view: split, power_override: always-on }
  T8113X: { enabled: false }
`,{EUFY_PASSWORD:'envpw'});
  assert.equal(cfg.email,'a@b.c');assert.equal(cfg.password,'envpw');assert.equal(cfg.country,'US');
  assert.equal(cfg.port,3000);assert.equal(cfg.host,'0.0.0.0');assert.equal(cfg.selfHost,'127.0.0.1');
  assert.equal(cfg.lan.cidr,'10.0.0.0/8');assert.equal(cfg.lan.force,true);
  assert.deepEqual(cfg.lan.stationAddresses,{T8010X:'10.0.0.5'});
  assert.equal(cfg.defaults.quality,'Full HD (1080P)');assert.equal(cfg.defaults.dualView,null);
  assert.deepEqual(cfg.cameras.T8410X,{name:'Garage'});
  assert.deepEqual(cfg.cameras.T8214X,{dualView:'split',powerOverride:'always-on'});
  assert.deepEqual(cfg.cameras.T8113X,{enabled:false});
  assert.deepEqual(cfg.stall,{stallMs:5000,startupStallMs:30000,gapMs:45000,exitAfterMs:300000,recreateClientAfter:1,backoffMs:[1000,2000,4000,8000]});
  assert.equal(cfg.go2rtcBin,'go2rtc');
});

test('explicit camera-source retirement thresholds remain authoritative',()=>{
  for(const threshold of [0,1,3,8])assert.equal(fromYaml(`${credentials}stall: { recreate_client_after: ${threshold} }`).cfg.stall.recreateClientAfter,threshold);
});

test('normalization requires credentials and valid CIDR',()=>{
  assert.throws(()=>fromYaml('eufy: { email: a@b.c }\nlan: { cidr: nope }'),/password/);
  assert.throws(()=>fromYaml(`${credentials}lan: { cidr: nope }`),/lan.cidr/);
});

test('CIDR validation rejects octets outside 0–255 and prefixes outside 0–32',()=>{
  for(const bad of ['192.168.1.0/33','192.168.1.0/99','256.0.0.0/8','10.0.0.999/8','1.2.3/8','10.0.0.0']){
    assert.equal(isValidCidr(bad),false,bad);
    assert.throws(()=>fromYaml(`${credentials}lan: { cidr: "${bad}" }`),/lan\.cidr/,bad);
  }
  for(const good of ['0.0.0.0/0','255.255.255.255/32','10.0.0.0/8','192.168.1.0/24']){
    assert.equal(isValidCidr(good),true,good);
    assert.equal(fromYaml(`${credentials}lan: { cidr: "${good}" }`).cfg.lan.cidr,good);
  }
});

test('dual view remains unset unless explicitly requested and typos fail',()=>{
  assert.equal(fromYaml(credentials).cfg.defaults.dualView,null);
  assert.equal(fromYaml(`${credentials}defaults: { dual_view: pip-tl }`).cfg.defaults.dualView,'pip-tl');
  assert.throws(()=>fromYaml(`${credentials}defaults: { dual_view: sideways }`),/dual_view must be one of/);
});

test('power override accepts automatic, hardwired and battery policy',()=>{
  for(const value of ['auto','always-on','battery'])assert.equal(fromYaml(`${credentials}cameras: { T8214X: { power_override: ${value} } }`).cfg.cameras.T8214X.powerOverride,value);
  assert.throws(()=>fromYaml(`${credentials}cameras: { T8214X: { power_override: wired } }`),/power_override must be one of/);
});

test('per-camera transcode mode overrides global mode',()=>{
  for(const value of ['never','auto','always'])assert.equal(fromYaml(`${credentials}go2rtc: { transcode: always }\ncameras: { T8423X: { transcode: ${value} } }`).cfg.cameras.T8423X.transcode,value);
  assert.throws(()=>fromYaml(`${credentials}cameras: { T8423X: { transcode: sometimes } }`),/transcode must be one of/);
});

test('VAAPI decoding defaults hardware first with explicit stored false override',()=>{
  assert.equal(fromYaml(credentials).cfg.go2rtcVaapiDecode,true);
  assert.equal(fromYaml(`${credentials}go2rtc: { vaapi_device: /dev/dri/renderD128, vaapi_decode: true }`).cfg.go2rtcVaapiDecode,true);
  assert.equal(fromYaml(`${credentials}go2rtc: { vaapi_decode: false }`).cfg.go2rtcVaapiDecode,false);
});

test('normalized external sources come from stored streams and preserve private URL bytes',()=>{
  const {cfg}=fromYaml(`${credentials}go2rtc: { streams: { wyze_cam: "wyze://192.0.2.5?enr=synthetic" } }`);
  assert.equal(cfg.go2rtcImported.streams.wyze_cam,'wyze://192.0.2.5?enr=synthetic');
  assert.equal(cfg.go2rtcImported.api,undefined);assert.equal(cfg.go2rtcApiPort,1984);
});

test('per-camera VAAPI decode accepts booleans without changing global default',()=>{
  const {cfg}=fromYaml(`${credentials}cameras:\n  ONE: { vaapi_decode: false }\n  TWO: { vaapi_decode: true }\n  THREE: {}\n`);
  assert.equal(cfg.go2rtcVaapiDecode,true);assert.equal(cfg.cameras.ONE.vaapiDecode,false);assert.equal(cfg.cameras.TWO.vaapiDecode,true);assert.equal(cfg.cameras.THREE.vaapiDecode,undefined);
  for(const invalid of ['"false"','1','[]'])assert.throws(()=>fromYaml(`${credentials}cameras: { ONE: { vaapi_decode: ${invalid} } }`),/vaapi_decode must be a boolean/);
});
