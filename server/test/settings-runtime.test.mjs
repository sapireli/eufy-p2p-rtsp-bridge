import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync,rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SettingsStore } from "../src/settings-store.mjs";
import { buildConfig } from "../src/config.mjs";
import { createSettingsRuntime } from "../src/settings-runtime.mjs";
function fixture(t) {
  const dir=mkdtempSync(join(tmpdir(),"wall-runtime-")); t.after(()=>rmSync(dir,{recursive:true,force:true}));
  const raw={cameras:{A:{mode:"always",power_override:"always-on"},B:{mode:"always",power_override:"always-on"}}};
  const store=new SettingsStore({dataDir:dir,initialSettings:raw,logger:null});
  const cfg=buildConfig({env:{},raw:{...raw,eufy:{email:"test@example.com",password:"synthetic"},data_dir:dir}}).cfg;
  const calls=[];
  const render=(config)=>Object.entries(config.cameras).map(([sn,c])=>({sn,enabled:true,...c}));
  let cameras=render(cfg);
  const ctx={cfg,state:{flags:{ready:true}},listCameras:()=>cameras,
    refreshCameras:async({config=cfg,commit=true}={})=>{calls.push(["refresh",commit]); if(commit)cameras=render(config); return render(config);},
    holds:{isHeld:()=>false},stopCamera:sn=>calls.push(["stop",sn]),sdk:{dropStreamClient:async sn=>calls.push(["drop",sn])},
    ensureWarm:async sn=>calls.push(["warm",sn]),applyPins:async sn=>{calls.push(["pin",sn]);return {dualView:"set",quality:"skip"};},
    broadcastEvent:e=>calls.push(["event",e]),ws:{broadcastSnapshot:async()=>{}}};
  return {ctx,calls,store,runtime:createSettingsRuntime(ctx,store)};
}
test("metadata/device policy rejection occurs before persistence or playback changes",async t=>{
  const {ctx,calls,store,runtime}=fixture(t);
  ctx.refreshCameras=async()=>{throw new Error("Battery camera cannot stream always");};
  await assert.rejects(runtime.patch({revision:1,settings:{cameras:{A:{power_override:"battery"}}}}),/Battery/);
  assert.equal(store.load().revision,1);assert.equal(calls.length,0);
});
test("camera mode edit affects target feed and preserves sibling warm playback",async t=>{
  const {calls,runtime,ctx}=fixture(t);
  const result=await runtime.patch({revision:1,settings:{cameras:{A:{mode:"on_motion"}}}});
  assert.equal(result.apply.state,"applied");
  assert.deepEqual(calls.filter(c=>c[0]==="stop"),[["stop","A"]]);
  assert.equal(calls.some(c=>c[0]==="drop"),false);
  assert.equal(ctx.cfg.cameras.B.mode,"always");
  assert.equal(calls.some(c=>c[0]==="warm"&&c[1]==="B"),true);
});
test("queued simultaneous revision requests reject stale writer without overwriting first",async t=>{
  const {runtime,store}=fixture(t);
  const results=await Promise.allSettled([runtime.patch({revision:1,settings:{stall:{stall_ms:4000}}}),runtime.patch({revision:1,settings:{stall:{stall_ms:3000}}})]);
  assert.equal(results[0].status,"fulfilled");assert.equal(results[1].status,"rejected");
  assert.equal(results[1].reason.statusCode,409);
  assert.equal(store.load().revision,2);assert.equal(store.load().settings.stall.stall_ms,4000);
});
test("startup-dependent update remains durable and reports pending instead of claiming applied",async t=>{
  const {runtime,store,ctx}=fixture(t);
  const result=await runtime.patch({revision:1,settings:{go2rtc:{transcode:"auto"}}});
  assert.equal(result.apply.state,"restart-required");
  assert.ok(result.apply.restartRequired.includes("go2rtc"));
  assert.equal(store.load().settings.go2rtc.transcode,"auto");
  assert.equal(ctx.cfg.go2rtcTranscode,"never");
});
test("backup import uses the same device validation and application path",async t=>{
  const {runtime,store}=fixture(t);const backup=store.export();backup.settings.cameras.A.mode="on_motion";
  const result=await runtime.import({revision:1,backup});
  assert.equal(result.settings.cameras.A.mode,"on_motion");assert.equal(result.apply.state,"applied");
});

test('pre-login settings wait for boot and unsupported camera setters are reported honestly', async t => {
  const {ctx,store}=fixture(t);
  ctx.state.flags.ready=false;
  const runtime=createSettingsRuntime(ctx,store);
  assert.equal(runtime.snapshot().apply.state,'waiting-for-login');
  const saved=await runtime.patch({revision:1,settings:{defaults:{hold_seconds:61}}});
  assert.equal(saved.apply.state,'waiting-for-login');
  ctx.state.flags.booting=true;
  await assert.rejects(runtime.patch({revision:2,settings:{defaults:{hold_seconds:62}}}),error=>error.statusCode===503);
  assert.equal(store.load().revision,2);
  ctx.state.flags.ready=true;ctx.state.flags.booting=false;
  runtime.markReady({A:{quality:'unsupported'}});
  assert.equal(runtime.snapshot().apply.state,'failed');
  assert.match(runtime.snapshot().apply.error,/A/);
});
