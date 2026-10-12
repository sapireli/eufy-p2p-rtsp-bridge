import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { initializeEgressTiming } from "../src/egress-timing-diagnostic.mjs";

function timers() {
  const jobs=[];
  return {jobs,setTimer:(fn,ms)=>{const job={fn,ms,unref(){this.unreferenced=true;}};jobs.push(job);return job;},clearTimer:job=>{if(job)job.cleared=true;}};
}
function fixture(run) {
  const dir=fs.mkdtempSync(path.join(os.tmpdir(),"egress-timing-"));
  try {run(path.join(dir,"private.jsonl"));} finally {fs.rmSync(dir,{recursive:true,force:true});}
}
test("unset diagnostic environment creates no hook, clocks, files or timers",()=>{
  const ctx={},t=timers();
  const stop=initializeEgressTiming(ctx,{env:{},io:new Proxy({}, {get(){throw Error("unexpected IO");}}),...t});
  assert.deepEqual(ctx,{});assert.equal(t.jobs.length,0);stop();
});
test("private diagnostic activates after15s, expires by fixed deadline, and cleanup is idempotent",()=>fixture(file=>{
  const ctx={},t=timers();const stop=initializeEgressTiming(ctx,{env:{EGRESS_TIMING_FILE:file,EGRESS_TIMING_SECONDS:"30"},...t});
  assert.equal(fs.statSync(file).mode & 0o777,0o600);
  assert.equal(ctx.freshEgressTimingActive,false);
  assert.deepEqual(t.jobs.map(j=>j.ms),[15000,45000]);assert.ok(t.jobs.every(j=>j.unreferenced));
  ctx.onFreshEgressTiming({frameId:"before"});assert.equal(fs.readFileSync(file,"utf8"),"");
  t.jobs[0].fn();assert.equal(ctx.freshEgressTimingActive,true);
  ctx.onFreshEgressTiming({frameId:"aged",ingressMonoUs:"123"});
  assert.deepEqual(JSON.parse(fs.readFileSync(file,"utf8")),{frameId:"aged",ingressMonoUs:"123"});
  t.jobs[1].fn();assert.equal(ctx.freshEgressTimingActive,false);
  ctx.onFreshEgressTiming({frameId:"after"});stop();stop();
  assert.equal(fs.readFileSync(file,"utf8").trim().split("\n").length,1);assert.ok(t.jobs.every(j=>j.cleared));
}));
test("early shutdown closes output and pending startup cannot reactivate diagnostics",()=>fixture(file=>{
  const ctx={},t=timers();const stop=initializeEgressTiming(ctx,{env:{EGRESS_TIMING_FILE:file},...t});
  stop();t.jobs[0].fn();assert.equal(ctx.freshEgressTimingActive,false);ctx.onFreshEgressTiming({frameId:"after shutdown"});
  assert.equal(fs.readFileSync(file,"utf8"),"");
}));
test("exclusive file and symlink collisions never overwrite or activate a diagnostic",()=>fixture(file=>{
  fs.writeFileSync(file,"preserve");const ctx={},t=timers(),warnings=[];
  initializeEgressTiming(ctx,{env:{EGRESS_TIMING_FILE:file},...t,warn:s=>warnings.push(s)});
  assert.deepEqual(ctx,{});assert.equal(t.jobs.length,0);assert.equal(fs.readFileSync(file,"utf8"),"preserve");
  const link=file+".link";fs.symlinkSync(file,link);
  initializeEgressTiming(ctx,{env:{EGRESS_TIMING_FILE:link},...t,warn:s=>warnings.push(s)});
  assert.deepEqual(ctx,{});assert.equal(fs.readFileSync(file,"utf8"),"preserve");assert.equal(warnings.length,2);
}));
test("record bound closes diagnostic at256 and invalid durations create no file",()=>fixture(file=>{
  const ctx={},t=timers();initializeEgressTiming(ctx,{env:{EGRESS_TIMING_FILE:file,EGRESS_TIMING_SECONDS:"45"},...t});t.jobs[0].fn();
  for(let i=0;i<300;i++)ctx.onFreshEgressTiming({frameId:String(i)});
  assert.equal(ctx.freshEgressTimingActive,false);assert.equal(fs.readFileSync(file,"utf8").trim().split("\n").length,256);
  for(const seconds of ["46","0","-1","NaN"]){const invalid=file+seconds;const other={};initializeEgressTiming(other,{env:{EGRESS_TIMING_FILE:invalid,EGRESS_TIMING_SECONDS:seconds},warn:()=>{}});assert.equal(fs.existsSync(invalid),false);assert.deepEqual(other,{});}
}));
test("short diagnostic output writes close the hook instead of retaining a corrupt writer",()=>{
  const ctx={},t=timers();let closed=0;
  initializeEgressTiming(ctx,{env:{EGRESS_TIMING_FILE:"/tmp/isolated-test"},...t,warn:()=>{},io:{openSync:()=>7,writeSync:()=>1,closeSync:()=>closed++}});
  t.jobs[0].fn();ctx.onFreshEgressTiming({frameId:"aged"});assert.equal(closed,1);assert.equal(ctx.freshEgressTimingActive,false);
});
