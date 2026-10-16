import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { freshTranscodeArgs, createFreshEgress, TIMED_OUTPUT_MAX_QUEUED_BYTES } from "../src/fresh-egress.mjs";
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
function payload(flv) {
  let at = flv.subarray(0,3).toString() === "FLV" ? 13 : 0;
  while (at + 15 <= flv.length) {
    const size = flv.readUIntBE(at+1,3), body = flv.subarray(at+11,at+11+size);
    if (flv[at] === 9 && (body[1] === 1 || ((body[0] & 128) && (body[0] & 15) === 3))) {
      const nals=[];
      for(let p=5;p<body.length;) {
        const length=body.readUInt32BE(p);p+=4;
        nals.push(Buffer.from([0,0,0,1]),body.subarray(p,p+length));p+=length;
      }
      return Buffer.concat(nals);
    }
    at += 11 + size + 4;
  }
  throw new Error("FLV write contains no coded video picture");
}
function fakeProcess() {
  const child = new EventEmitter();
  child.stdin = new PassThrough(); child.stdout = new PassThrough(); child.stderr = new PassThrough();
  child.stdio = [child.stdin, child.stdout, child.stderr, new PassThrough()];
  child.kill = (signal) => { child.killed = signal; };
  return child;
}
function harness({ mode = "never", spawnFn, rendition, codec = "h264", timingHook, timingLimit, timingActive } = {}) {
  const res = new EventEmitter();
  res.writes = []; res.writableNeedDrain = false; res.destroyed = false;
  res.write = (data) => { res.writes.push(data); return true; };
  res.destroy = () => { if (!res.destroyed) { res.destroyed = true; res.emit("close"); } };
  const slot = { codec };
  let source, onFrame, detached = 0, nowUs = 2000000n, clockReads = 0;
  const cfg = { go2rtcTranscode: mode, go2rtcVaapiDevice: "/dev/dri/renderD128", go2rtcMaxHeight: 0 };
  const ctx = { cfg, state: { slots: new Map([["camera", slot]]) }, getCamera: () => ({}), sdk: { extractParamSets: paramSets }, freshness: { nowUs: () => { clockReads++; return nowUs; } },
    attachConsumer: (_sn, consumer, options) => { source = consumer; onFrame = options.onFrame; return () => { detached++; }; }, spawn: spawnFn, onFreshEgressTiming: timingHook, maxFreshEgressTimingRecords: timingLimit, freshEgressTimingActive: timingActive };
  const api = createFreshEgress(ctx);
  const stop = api.attachFreshConsumer("camera", res, { rendition });
  let frameId = 0n;
  function feed(data = idr, ts = 1000, ageMs = 100, metadata = true, dispatchTiming) {
    if (source.destroyed) return;
    if (source._ewbDropping && codec === "h264" && !paramSets(data)) return;
    source._ewbDropping = false;
    const frame = { data, sourceTimestamp: ts, codec, width:128,height:96,keyframe: Boolean(paramSets(data)) };
    const stamp = { serverEpoch: Buffer.alloc(8, 0x44), sourceTimestamp: ts, ingressMonoUs: nowUs - BigInt(ageMs)*1000n, frameId: frameId++, maxAgeMs: 500 };
    onFrame(frame, metadata ? stamp : undefined, dispatchTiming);
    return stamp;
  }
  return { ctx, api, stop, res, slot, feed, source: () => source, detached: () => detached, clockReads: () => clockReads, setNow: (value) => { nowUs = value; } };
}

test("fresh encoder arguments retain hardware-first software fallback, copyts and passthrough packet association", () => {
  const args = freshTranscodeArgs({ go2rtcVaapiDevice: "auto", go2rtcMaxHeight: 1680 });
  const attempts = transcodeAttempts(args, { devices: ["/dev/dri/renderD128"] });
  assert.deepEqual(attempts.map((a) => a.name), ["hardware-decode-encode", "software-decode-hardware-encode", "software-decode-encode"]);
  for (const { args: expanded } of attempts) {
    assert.ok(expanded.includes("-copyts"));
    assert.ok(expanded.includes("-noautoscale"));
    assert.equal(expanded[expanded.indexOf("-reinit_filter")+1], "1");
    for (const [name, value] of [["-fps_mode:v","passthrough"],["-enc_time_base:v","1:90000"],["-stats_mux_pre_fmt","{n} {pts} {tb} {size}"],["-f","data"]]) assert.equal(expanded[expanded.lastIndexOf(name)+1], value);
  }
});

test("production egress pins explicit canvas before hardware encoder startup", () => {
  let launched;
  const h = harness({ mode: "always", spawnFn: (_bin, args) => { launched=args; return fakeProcess(); } });
  h.ctx.cfg.go2rtcMaxHeight=1080;
  h.feed();
  assert.ok(launched.includes("scale_vaapi=w=1440:h=1080:format=nv12"));
  assert.ok(launched.includes("-noautoscale"));
  h.stop();
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
test("direct consumer fails closed for absent receipt metadata and actual bounded downstream backlog", () => {
  const missing = harness(); missing.feed(idr,1000,10,false);
  assert.equal(missing.res.destroyed,true);
  assert.equal(missing.res.writes.length,0);
  const blocked = harness(); blocked.res.writableNeedDrain=true; blocked.res.writableLength=TIMED_OUTPUT_MAX_QUEUED_BYTES; blocked.feed();
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


test("native consumer bypasses global always policy without changing the compatible producer", () => {
  let spawned = 0;
  const h = harness({ mode: "always", rendition: "native", spawnFn: () => { spawned++; return fakeProcess(); } });
  const stamp = h.feed();
  assert.equal(spawned, 0);
  assert.equal(h.slot.freshness, undefined);
  assert.equal(h.slot.nativeFreshness.outputFrames, 1);
  assert.equal(h.res.writes.length, 1);
  assert.equal(parseFrameStamp(payload(h.res.writes[0]), "h264").frameId, stamp.frameId);
  h.stop();
  const compatible = harness({ mode: "always", spawnFn: () => { spawned++; return fakeProcess(); } });
  compatible.feed();
  assert.equal(spawned, 1);
  compatible.stop();
});


test("native HEVC copy delivers real source pictures and exact stamps under global always without encoder", t => {
  const dir=mkdtempSync(join(tmpdir(),"ewb-native-hevc-"));
  let h;
  try {
    const stats=join(dir,"stats.txt");
    const fixture=spawnSync("ffmpeg",["-v","error","-f","lavfi","-i","testsrc2=size=128x96:rate=15","-frames:v","3","-c:v","libx265","-x265-params","pools=1:frame-threads=1:bframes=0:keyint=3:log-level=error","-map","0:v:0","-f","data","-stats_mux_pre",stats,"-stats_mux_pre_fmt","{n} {pts} {tb} {size}","pipe:1"],{maxBuffer:1024*1024});
    assert.equal(fixture.status,0,fixture.stderr.toString());
    const frames=[];const reader=new EncodedFrameReader({onFrame:row=>frames.push(row)});
    reader.pushData(fixture.stdout);reader.pushStats(readFileSync(stats));reader.end();
    h=harness({mode:"always",codec:"h265",rendition:"native",spawnFn:()=>{throw Error("native must not create an encoder");}});
    const stamps=frames.map((row,i)=>h.feed(row.data,1000+i*67));
    assert.equal(h.res.writes.length,3);
    const restored=h.res.writes.map(bytes=>payload(bytes));
    for(let i=0;i<restored.length;i++){
      const stamp=parseFrameStamp(restored[i],"h265");
      assert.equal(stamp.frameId,stamps[i].frameId);
      assert.equal(stamp.sourceTimestamp,stamps[i].sourceTimestamp);
      assert.equal(stamp.ingressMonoUs,stamps[i].ingressMonoUs);
    }
    const decode=data=>spawnSync("ffmpeg",["-v","error","-threads","1","-f","hevc","-i","pipe:0","-fps_mode:v","passthrough","-f","framemd5","pipe:1"],{input:data,encoding:"utf8"});
    const original=decode(Buffer.concat(frames.map(row=>row.data))),copied=decode(Buffer.concat(restored));
    assert.equal(original.status,0,original.stderr);assert.equal(copied.status,0,copied.stderr);
    assert.equal(copied.stdout,original.stdout,"coded native pictures preserve decoded pixels/timing");
  } finally {h?.stop();rmSync(dir,{recursive:true,force:true});}
});


test("output high-water hint accepts consecutive references until the explicit byte bound", () => {
  const h=harness();h.res.writableNeedDrain=true;h.res.writableLength=200000;
  h.feed();h.feed(inter,1067);
  assert.equal(h.res.destroyed,false);assert.equal(h.res.writes.length,2);
  h.res.writableLength=TIMED_OUTPUT_MAX_QUEUED_BYTES;
  h.feed(inter,1134);
  assert.equal(h.res.destroyed,true);assert.equal(h.res.writes.length,2);
  h.stop();
});

test("real Node HTTP delivers a large keyframe and synchronous next AU before future input or EOF", {timeout:5000}, async () => {
  let onFrame,source,stop,serverResponse;
  const writes=[];
  const ctx={cfg:{go2rtcTranscode:"always"},state:{slots:new Map([["camera",{codec:"h264"}]])},getCamera:()=>({}),freshness:{nowUs:()=>2000000n},
    spawn:()=>{throw Error("native profile must bypass encoder");},
    attachConsumer:(_sn,consumer,options)=>{source=consumer;onFrame=options.onFrame;return ()=>{};}};
  const api=createFreshEgress(ctx);
  const server=http.createServer((_req,res)=>{
    serverResponse=res;res.writeHead(200,{"content-type":"video/x-flv"});
    const write=res.write.bind(res);res.write=bytes=>{writes.push(bytes);return write(bytes);};
    stop=api.attachFreshConsumer("camera",res,{rendition:"native"});
    const filler=Buffer.alloc(150000,0x55),length=[5];let remaining=filler.length;
    while(remaining>=255){length.push(255);remaining-=255;}length.push(remaining);
    const large=Buffer.concat([Buffer.from([0,0,0,1,6,...length]),filler,Buffer.from([128]),idr]);
    const stamp=(frameId,sourceTimestamp)=>({serverEpoch:Buffer.alloc(8,0x44),sourceTimestamp,ingressMonoUs:1900000n,frameId,maxAgeMs:500});
    onFrame({data:large,codec:"h264",sourceTimestamp:1000},stamp(1n,1000));
    assert.equal(res.writableNeedDrain,true,"real first write exceeds Node response high-water mark");
    assert.ok(res.writableLength>0 && res.writableLength<TIMED_OUTPUT_MAX_QUEUED_BYTES);
    onFrame({data:inter,codec:"h264",sourceTimestamp:1067},stamp(2n,1067));
  });
  let request,response;
  try{
    await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));
    const delivered=await new Promise((resolve,reject)=>{
      request=http.get(`http://127.0.0.1:${server.address().port}`,res=>{
        response=res;const chunks=[];let total=0;
        res.on("error",reject);res.on("data",chunk=>{
          chunks.push(chunk);total+=chunk.length;
          if(writes.length===2 && total===writes.reduce((n,b)=>n+b.length,0))resolve(Buffer.concat(chunks));
        });res.on("end",()=>reject(Error("response ended before both AUs arrived")));
      });request.on("error",reject);
    });
    assert.equal(source.destroyed,false);assert.equal(serverResponse.writableEnded,false);
    assert.deepEqual(delivered,Buffer.concat(writes));
    assert.deepEqual(writes.map(bytes=>parseFrameStamp(payload(bytes),"h264").frameId),[1n,2n]);
    assert.deepEqual(writes.map(bytes=>parseFrameStamp(payload(bytes),"h264").sourceTimestamp),[1000,1067]);
  }finally{stop?.();response?.destroy();request?.destroy();await new Promise(resolve=>server.close(resolve));}
});


test("real blocked HTTP consumer is closed at the byte bound rather than retaining unbounded pictures", {timeout:5000}, async () => {
  let onFrame,stop,response,request,serverResponse;
  const ctx={cfg:{go2rtcTranscode:"never"},state:{slots:new Map([["camera",{codec:"h264"}]])},getCamera:()=>({}),freshness:{nowUs:()=>2000000n},
    attachConsumer:(_sn,_consumer,options)=>{onFrame=options.onFrame;return ()=>{};}};
  const api=createFreshEgress(ctx),written=[];
  const large=Buffer.concat([idr,Buffer.alloc(150000,0x55)]);
  const feed=i=>onFrame({data:large,codec:"h264",sourceTimestamp:1000+i*67},{serverEpoch:Buffer.alloc(8),sourceTimestamp:1000+i*67,ingressMonoUs:1900000n,frameId:BigInt(i),maxAgeMs:500});
  const server=http.createServer((_req,res)=>{
    serverResponse=res;res.writeHead(200,{"content-type":"video/x-flv"});
    const write=res.write.bind(res);res.write=bytes=>{written.push(bytes.length);return write(bytes);};
    stop=api.attachFreshConsumer("camera",res);feed(0);
  });
  try{
    await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));
    await new Promise((resolve,reject)=>{
      request=http.get(`http://127.0.0.1:${server.address().port}`,res=>{response=res;res.pause();resolve();});
      request.on("error",reject);
    });
    let lastAccepted=0;
    for(let i=1;i<100&&!serverResponse.destroyed;i++){
      lastAccepted=serverResponse.writableLength;feed(i);
      if(!serverResponse.destroyed)assert.ok(serverResponse.writableLength<=TIMED_OUTPUT_MAX_QUEUED_BYTES);
    }
    assert.equal(serverResponse.destroyed,true);
    assert.ok(written.length>2 && written.length<100,"accepted multiple pictures then bounded actual response queue");
    assert.ok(lastAccepted>7*1024*1024);
  }finally{stop?.();response?.destroy();request?.destroy();await new Promise(resolve=>server.close(resolve));}
});


test("compatible encoder drains multiple complete output AUs without treating HTTP high water as loss", () => {
  let child;
  const h=harness({mode:"always",spawnFn:()=>child=fakeProcess()});
  const original=h.res.write;
  h.res.write=bytes=>{original(bytes);h.res.writableLength=(h.res.writableLength??0)+bytes.length;h.res.writableNeedDrain=true;return false;};
  const first=h.feed(idr,1000),second=h.feed(inter,1067);
  child.stdout.write(Buffer.concat([idr,inter]));
  child.stdio[3].write(`0 0 1/90000 ${idr.length}\n1 6030 1/90000 ${inter.length}\n`);
  assert.equal(h.res.destroyed,false);assert.equal(h.res.writes.length,2);
  assert.deepEqual(h.res.writes.map(bytes=>parseFrameStamp(payload(bytes),"h264").frameId),[first.frameId,second.frameId]);
  h.stop();
});


test("late source dispatch diagnostic preserves identity and exposes separate bridge/consumer boundaries", () => {
  const records=[];
  const h=harness({timingHook:r=>records.push(r)});
  const stamp=h.feed(idr,1000,650,true,{bridgeEntryUs:1900000n,consumerDispatchUs:1990000n});
  assert.equal(records.length,1);
  const r=records[0];
  assert.equal(r.ingressMonoUs,"1350000");
  assert.equal(r.bridgeEntryUs,"1900000");
  assert.equal(r.consumerDispatchUs,"1990000");
  assert.equal(r.consumerEntryUs,"2000000");
  assert.equal(r.stdinQueuedUs,undefined);
  assert.equal(r.encodedJoinedUs,undefined);
  assert.equal(parseFrameStamp(payload(h.res.writes[0]),"h264").frameId,stamp.frameId);
  assert.equal(parseFrameStamp(payload(h.res.writes[0]),"h264").ingressMonoUs,stamp.ingressMonoUs);
  h.stop();
});
test("late encoder diagnostic separates timely delivery from enqueue-to-packet-join hold", () => {
  const records=[]; let child;
  const h=harness({mode:"always",spawnFn:()=>child=fakeProcess(),timingHook:r=>records.push(r)});
  const stamp=h.feed(idr,1000,10,true,{bridgeEntryUs:2000000n,consumerDispatchUs:2000000n});
  h.setNow(2700000n);
  child.stdio[3].write(`0 0 1/90000 ${idr.length}\n`);
  child.stdout.write(idr);
  assert.equal(records.length,1);
  assert.equal(records[0].consumerEntryUs,"2000000");
  assert.equal(records[0].stdinQueuedUs,"2000000");
  assert.equal(records[0].encodedJoinedUs,"2700000");
  assert.equal(records[0].httpWriteUs,"2700000");
  assert.equal(records[0].pendingFrames,1);
  assert.equal(parseFrameStamp(payload(h.res.writes[0]),"h264").ingressMonoUs,stamp.ingressMonoUs);
  h.stop();
});
test("late diagnostic is row-bounded and exceptions cannot change reference delivery", () => {
  let calls=0;
  const h=harness({timingLimit:1,timingHook:()=>{calls++;throw Error("observer failure");}});
  h.feed(idr,1000,600);
  const afterFirst=h.clockReads();
  h.feed(inter,1067,600);
  assert.equal(calls,1);
  assert.equal(h.res.writes.length,2);
  assert.equal(h.res.destroyed,false);
  assert.equal(h.clockReads()-afterFirst,1,"after row bound only existing output-age clock is read");
  h.stop();
});
test("disabled diagnostic adds no clocks, records, or encoded-byte changes", () => {
  const plain=harness(); const rows=[]; const observed=harness({timingHook:r=>rows.push(r)});
  plain.feed(idr,1000,10); observed.feed(idr,1000,10);
  assert.equal(plain.clockReads(),1,"existing output-age read only");
  assert.equal(rows.length,0,"fresh frames produce no diagnostic records");
  assert.deepEqual(plain.res.writes,observed.res.writes);
  plain.stop();observed.stop();
});

test("expiring an installed timing hook stops all extra clocks without changing delivery",()=>{
  const rows=[];const h=harness({timingHook:r=>rows.push(r),timingActive:false});
  h.feed(idr,1000,600);assert.equal(h.clockReads(),1);assert.equal(rows.length,0);
  h.ctx.freshEgressTimingActive=true;h.feed(inter,1067,600);assert.equal(rows.length,1);
  const after=h.clockReads();h.ctx.freshEgressTimingActive=false;h.feed(inter,1134,600);
  assert.equal(h.clockReads()-after,1);assert.equal(rows.length,1);assert.equal(h.res.writes.length,3);h.stop();
});
