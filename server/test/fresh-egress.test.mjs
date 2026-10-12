import test from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { freshTranscodeArgs, createFreshEgress } from "../src/fresh-egress.mjs";
import { transcodeAttempts } from "../src/ffmpeg-hardware.mjs";
import { EncodedFrameReader } from "../src/encoded-frame-reader.mjs";
import { parseFrameStamp } from "../src/frame-stamp.mjs";

const idr = Buffer.concat([Buffer.from([0,0,0,1]),Buffer.from("6764000aacb2041b6022000003000200057e401e244c90","hex"),Buffer.from([0,0,0,1]),Buffer.from("68ebc3cb22c0","hex"),Buffer.from([0,0,0,1,0x65,0x88])]);
const inter = Buffer.from([0,0,0,1,1,0x88]);
function paramSets(data) {
  let sps = false, pps = false;
  for (let i = 0; i < data.length - 4; i++) if (data[i] === 0 && data[i+1] === 0 && (data[i+2] === 1 || (data[i+2] === 0 && data[i+3] === 1))) {
    const type = data[i + (data[i+2] === 1 ? 3 : 4)] & 31;
    sps ||= type === 7; pps ||= type === 8;
  }
  return sps && pps ? {} : undefined;
}
function payload(ts) {
  const parts = [];
  for (let at = 0; at < ts.length; at += 188) {
    const p = ts.subarray(at, at+188);
    if ((((p[1]&31)<<8)|p[2]) !== 0x100) continue;
    const start = (p[3]&32) ? 5+p[4] : 4;
    parts.push(p.subarray(start));
  }
  const pes = Buffer.concat(parts);
  return pes.subarray(9 + pes[8]);
}
function fakeProcess() {
  const child = new EventEmitter();
  child.stdin = new PassThrough(); child.stdout = new PassThrough(); child.stderr = new PassThrough();
  child.stdio = [child.stdin, child.stdout, child.stderr, new PassThrough()];
  child.kill = (signal) => { child.killed = signal; };
  return child;
}
function harness({ mode = "never", spawnFn } = {}) {
  const res = new EventEmitter();
  res.writes = []; res.writableNeedDrain = false; res.destroyed = false;
  res.write = (data) => { res.writes.push(data); return true; };
  res.destroy = () => { if (!res.destroyed) { res.destroyed = true; res.emit("close"); } };
  const slot = { codec: "h264" };
  let source, onFrame, detached = 0, nowUs = 2000000n;
  const cfg = { go2rtcTranscode: mode, go2rtcVaapiDevice: "/dev/dri/renderD128", go2rtcMaxHeight: 0 };
  const ctx = { cfg, state: { slots: new Map([["camera", slot]]) }, getCamera: () => ({}), sdk: { extractParamSets: paramSets }, freshness: { nowUs: () => nowUs },
    attachConsumer: (_sn, consumer, options) => { source = consumer; onFrame = options.onFrame; return () => { detached++; }; }, spawn: spawnFn };
  const api = createFreshEgress(ctx);
  const stop = api.attachFreshConsumer("camera", res);
  let frameId = 0n;
  function feed(data = idr, ts = 1000, ageMs = 100, metadata = true) {
    if (source.destroyed) return;
    if (source._ewbDropping && !paramSets(data)) return;
    source._ewbDropping = false;
    const frame = { data, sourceTimestamp: ts, codec: "h264", width:128,height:96,keyframe: Boolean(paramSets(data)) };
    const stamp = { serverEpoch: Buffer.alloc(8, 0x44), sourceTimestamp: ts, ingressMonoUs: nowUs - BigInt(ageMs)*1000n, frameId: frameId++, maxAgeMs: 500 };
    onFrame(frame, metadata ? stamp : undefined);
    return stamp;
  }
  return { api, stop, res, slot, feed, source: () => source, detached: () => detached, setNow: (value) => { nowUs = value; } };
}

test("fresh encoder arguments retain hardware-first software fallback, copyts and passthrough packet association", () => {
  const args = freshTranscodeArgs({ go2rtcVaapiDevice: "auto", go2rtcMaxHeight: 1680 });
  const attempts = transcodeAttempts(args, { devices: ["/dev/dri/renderD128"] });
  assert.deepEqual(attempts.map((a) => a.name), ["hardware-decode-encode", "software-decode-hardware-encode", "software-decode-encode"]);
  for (const { args: expanded } of attempts) {
    assert.ok(expanded.includes("-copyts"));
    for (const [name, value] of [["-fps_mode:v","passthrough"],["-enc_time_base:v","1:90000"],["-stats_mux_pre_fmt","{n} {pts} {tb} {size}"],["-f","data"]]) assert.equal(expanded[expanded.lastIndexOf(name)+1], value);
  }
});
test("direct copy retains expired reference stamps so a fresh inter frame recovers without another keyframe", () => {
  const h = harness();
  const first = h.feed();
  assert.equal(h.res.writes.length, 1);
  assert.equal(parseFrameStamp(payload(h.res.writes[0]), "h264").frameId, first.frameId);
  h.feed(inter,1067,500); // Deadline is exclusive: exactly 500ms already expired.
  h.feed(inter,1134,10);
  assert.equal(h.res.writes.length,3,"reference packets remain available to the decoder");
  const expired=parseFrameStamp(payload(h.res.writes[1]),"h264");
  assert.equal(expired.ingressMonoUs,1500000n);assert.equal(expired.maxAgeMs,500);
  assert.equal(parseFrameStamp(payload(h.res.writes[2]),"h264").sourceTimestamp,1134);
  h.feed(idr,1201,10);
  assert.equal(h.res.writes.length,4);
  assert.equal(h.slot.freshness.staleFrames,1);
  assert.equal(h.slot.freshness.expiredReferenceFrames,1);
  assert.equal(h.slot.freshness.maximumEgressAgeMs,500);
  h.stop();
  assert.equal(h.detached(),1);
});
test("direct consumer fails closed for absent receipt metadata and stopped downstream drain", () => {
  const missing = harness(); missing.feed(idr,1000,10,false);
  assert.equal(missing.res.destroyed,true);
  assert.equal(missing.res.writes.length,0);
  const blocked = harness(); blocked.res.writableNeedDrain=true; blocked.feed();
  assert.equal(blocked.res.destroyed,true);
  assert.equal(blocked.res.writes.length,0);
});
test("encoder unknown PTS stops instead of matching the closest/newest source metadata", () => {
  let child;
  const h = harness({ mode:"always", spawnFn: () => (child=fakeProcess()) });
  h.feed();
  child.stdio[3].write(`0 9999 1/90000 ${idr.length}\n`);
  child.stdout.write(idr);
  assert.equal(h.res.destroyed,true);
  assert.equal(h.slot.freshness.unmappedFrames,1);
  assert.equal(h.res.writes.length,0);
});
test("stale encoder references retain exact expired stamps and fresh dependent output resumes immediately", () => {
  let child;
  const h = harness({mode:"always",spawnFn:()=> (child=fakeProcess())});
  h.feed(idr,1000,600); h.feed(inter,1067,10); h.feed(idr,1134,10);
  for(const [n,pts,data] of [[0,0,idr],[1,6030,inter],[2,12060,idr]]) {
    child.stdio[3].write(`${n} ${pts} 1/90000 ${data.length}\n`); child.stdout.write(data);
  }
  assert.equal(h.slot.freshness.staleFrames,1);
  assert.equal(h.res.writes.length,3);
  assert.equal(h.slot.freshness.expiredReferenceFrames,1);
  assert.equal(parseFrameStamp(payload(h.res.writes[0]),"h264").ingressMonoUs,1400000n);
  assert.equal(parseFrameStamp(payload(h.res.writes[1]),"h264").sourceTimestamp,1067,"fresh dependent packet doesn't await another IDR");
  h.stop(); assert.equal(child.killed,"SIGTERM");
});
test("hardware failure advances fallback only on a fresh source keyframe; ordinary errors close", () => {
  const children=[], args=[];
  const h=harness({mode:"always",spawnFn:(_bin,a)=> {args.push(a);const child=fakeProcess();children.push(child);return child;}});
  h.feed();
  children[0].stderr.write("Device creation failed: unsupported hardware\n"); children[0].emit("close",1);
  assert.equal(h.res.destroyed,false); assert.equal(h.source()._ewbDropping,true);
  h.feed(inter,1067); assert.equal(children.length,1);
  h.feed(idr,1134); assert.equal(children.length,2);
  assert.ok(args[0].includes("-hwaccel")); assert.ok(!args[1].includes("-hwaccel")); assert.ok(args[1].includes("h264_vaapi"));
  children[1].stderr.write("Invalid input data\n"); children[1].emit("close",1);
  assert.equal(h.res.destroyed,true);
});
test("encoder EOF validates complete data/stats and closes rather than retaining a partial picture", () => {
  let child;
  const h=harness({mode:"always",spawnFn:()=> (child=fakeProcess())});h.feed();
  child.stdio[3].write(`0 0 1/90000 ${idr.length}\n`);child.stdout.write(idr.subarray(0,2));child.emit("close",0);
  assert.equal(h.res.destroyed,true);assert.equal(h.res.writes.length,0);assert.equal(h.detached(),1);
});
test("a large source keyframe cannot turn transient encoder stdin backpressure into reference-frame loss", () => {
  let child;
  const h = harness({mode:"always",spawnFn:()=> (child=fakeProcess())});
  const large = Buffer.concat([idr, Buffer.alloc(250000,0x55)]);
  h.feed(large);
  assert.equal(child.stdin.writableNeedDrain,true,"fixture crosses the real stream high-water mark");
  assert.equal(h.source().writableNeedDrain,false,"generic consumer drop-to-keyframe policy must not see encoder pipe backpressure");
  h.feed(inter,1067);h.feed(inter,1134);
  assert.equal(h.slot.freshness.inputFrames,3,"dependent frames reach the bounded encoder queue");
  assert.equal(h.source()._ewbDropping,false);
  h.stop();
});
test("encoder input queue has a hard bound instead of silently dropping reference frames", () => {
  let child;
  const h=harness({mode:"always",spawnFn:()=> (child=fakeProcess())});
  h.feed(Buffer.concat([idr,Buffer.alloc(4*1024*1024,0x55)]));
  assert.equal(h.res.destroyed,true);
  assert.equal(h.res.writes.length,0);
  assert.equal(child.stdin.writableLength,0,"oversized input is rejected before queueing bytes");
});
test("real continuously fed production transcoder delivers stamped frames before stdin EOF", async (t) => {
  if(spawnSync("ffmpeg",["-version"]).status!==0)return t.skip("ffmpeg unavailable");
  const dir=mkdtempSync(join(tmpdir(),"ewb-fresh-continuous-"));
  let child,h,feeder,timeout;
  try {
    const statsPath=join(dir,"stats.txt");
    const generated=spawnSync("ffmpeg",["-hide_banner","-v","error","-f","lavfi","-i","testsrc2=size=128x96:rate=15","-frames:v","3","-c:v","libx264","-g","3","-bf","0","-fps_mode:v","passthrough","-enc_time_base:v","1:90000","-map","0:v:0","-f","data","-stats_mux_pre",statsPath,"-stats_mux_pre_fmt","{n} {pts} {tb} {size}","pipe:1"],{maxBuffer:1024*1024});
    assert.equal(generated.status,0,generated.stderr.toString());
    const units=[];const reader=new EncodedFrameReader({onFrame:unit=>units.push(unit)});reader.pushData(generated.stdout);reader.pushStats(readFileSync(statsPath));reader.end();
    // Valid large unknown user-data SEI: exercises the input pipe high-water mark
    // without changing picture bytes or requiring an expensive giant fixture.
    const filler=Buffer.alloc(150000,0x55),size=[5];let remaining=filler.length;
    while(remaining>=255){size.push(255);remaining-=255;}size.push(remaining);
    const largePrefix=Buffer.concat([Buffer.from([0,0,0,1,6,...size]),filler,Buffer.from([128])]);
    const largeIdr=Buffer.concat([largePrefix,units[0].data]);
    let resolveReady,rejectReady;
    const ready=new Promise((done,fail)=>{resolveReady=done;rejectReady=fail;});
    h=harness({mode:"always",spawnFn:(_bin,args,options)=>{
      child=spawn("ffmpeg",transcodeAttempts(freshTranscodeArgs({go2rtcVaapiDevice:"/dev/dri/renderD128"})).at(-1).args,options);
      child.once("error",rejectReady);
      child.once("close",code=>{if(h.res.writes.length<8)rejectReady(Error(`Continuous encoder closed prematurely (${code})`));});
      return child;
    }});
    const originalWrite=h.res.write;
    h.res.write=(bytes)=>{
      originalWrite(bytes);
      if(h.res.writes.length>=8){
        assert.equal(child.stdin.writableEnded,false,"successful output must precede EOF");
        assert.equal(child.exitCode,null);
        resolveReady();
      }
      return true;
    };
    let frame=0;
    const feed=()=>{
      if(h.source().writableNeedDrain){h.source()._ewbDropping=true;return;}
      const data=frame%3===0?largeIdr:units[frame%3].data;
      h.feed(data,1000+frame*67,10);frame++;
    };
    feed();feeder=setInterval(feed,30);
    timeout=setTimeout(()=>rejectReady(Error("No stable output while the encoder input remained live")),5000);
    await ready;
    const stamps=h.res.writes.map(bytes=>parseFrameStamp(payload(bytes),"h264"));
    // One stats/data read can drain several packets before the promise resumes.
    // Validate every delivered prefix picture rather than an IO-chunk boundary.
    assert.ok(stamps.length>=8);
    assert.ok(h.slot.freshness.inputFrames>=stamps.length);
    assert.equal(h.slot.freshness.unmappedFrames,0);
    assert.deepEqual(stamps.map(stamp=>stamp.sourceTimestamp),Array.from({length:stamps.length},(_,i)=>1000+i*67));
    assert.deepEqual(stamps.map(stamp=>stamp.frameId),Array.from({length:stamps.length},(_,i)=>BigInt(i)));
  }finally{
    clearInterval(feeder);clearTimeout(timeout);h?.stop();child?.kill("SIGKILL");rmSync(dir,{recursive:true,force:true});
  }
});
test("real software transcoder preserves irregular source PTS and stamps their exact source records", async (t) => {
  if(spawnSync("ffmpeg",["-version"]).status!==0)return t.skip("ffmpeg unavailable");
  const dir=mkdtempSync(join(tmpdir(),"ewb-fresh-egress-"));
  try {
    const path=join(dir,"stats.txt");
    const generated=spawnSync("ffmpeg",["-hide_banner","-v","error","-f","lavfi","-i","testsrc2=size=128x96:rate=15","-frames:v","8","-c:v","libx264","-g","3","-bf","0","-fps_mode:v","passthrough","-enc_time_base:v","1:90000","-map","0:v:0","-f","data","-stats_mux_pre",path,"-stats_mux_pre_fmt","{n} {pts} {tb} {size}","pipe:1"],{maxBuffer:1024*1024});
    assert.equal(generated.status,0,generated.stderr.toString());
    const input=[];const parser=new EncodedFrameReader({onFrame:frame=>input.push(frame)});parser.pushData(generated.stdout);parser.pushStats(readFileSync(path));parser.end();
    let child, close;
    const h=harness({mode:"always",spawnFn:(_bin,args,options)=> {
      // Select the production software fallback deterministically on the test host.
      const software=transcodeAttempts(freshTranscodeArgs({go2rtcVaapiDevice:"/dev/dri/renderD128"})).at(-1);
      child=spawn("ffmpeg",software.args,options);
      close=new Promise((done,fail)=> {child.on("close",done);child.on("error",fail);});
      return child;
    }});
    const source=[1000,1067,1134,1300,1367,1434,1501,1568];
    const expected=[];
    for(let i=0;i<input.length;i++)expected.push(h.feed(input[i].data,source[i],10));
    child.stdin.end();
    const timeout=setTimeout(()=>child.kill("SIGKILL"),15000);
    const code=await close;clearTimeout(timeout);
    assert.equal(code,0);
    assert.equal(h.slot.freshness.unmappedFrames,0);
    assert.equal(h.res.writes.length,input.length,"every encoded packet has exactly one original source stamp");
    const seen=h.res.writes.map(bytes=>parseFrameStamp(payload(bytes),"h264"));
    assert.deepEqual(seen.map(row=>row.sourceTimestamp),source);
    assert.deepEqual(seen.map(row=>row.frameId),expected.map(row=>row.frameId));
    assert.deepEqual(seen.map(row=>row.ingressMonoUs),expected.map(row=>row.ingressMonoUs));
  } finally {rmSync(dir,{recursive:true,force:true});}
});
