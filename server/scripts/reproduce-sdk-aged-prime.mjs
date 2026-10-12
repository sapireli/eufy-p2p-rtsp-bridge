// Offline metadata-only regression: real SDK source, synthetic transport/clock.
import assert from "node:assert/strict";
import {resolve,join} from "node:path";
import {execFileSync} from "node:child_process";
const sdkRoot=process.argv[2] ? resolve(process.argv[2]) : undefined;
if (!sdkRoot) throw new Error("Usage: node reproduce-sdk-aged-prime.mjs /path/to/sdk-checkout [--out result.json]");
const {build}=createRequire(join(sdkRoot,"package.json"))("esbuild");
import {createRequire} from "node:module";
import {EventEmitter} from "node:events";
import {writeFileSync} from "node:fs";
import {createFreshness} from "../src/freshness.mjs";
const bundled=await build({stdin:{contents:`export {SharedLiveSource} from './src/transport/p2p/shared-live-source.ts'; export {openReadableFromConsumer} from './src/transport/p2p/readable-egress.ts';`,resolveDir:sdkRoot},bundle:true,platform:"node",format:"cjs",write:false});
const mod={exports:{}};new Function("require","module","exports",bundled.outputFiles[0].text)(createRequire(import.meta.url),mod,mod.exports);
const {SharedLiveSource,openReadableFromConsumer}=mod.exports;
let wall=1000,mono=1000000n;const dateNow=Date.now;Date.now=()=>wall;
const stream=new EventEmitter();stream.start=()=>stream;stream.stop=()=>{};stream.requestKeyframe=()=>{};stream.sendStart=()=>{};
const source=new SharedLiveSource({makeStream:()=>stream,lingerMs:8000,warmRetryMs:2000,warmTimeoutMs:20000});
const freshness=createFreshness({cfg:{freshness:{enabled:true,mode:"observe",maxAgeMs:500},port:3000}},{epoch:Buffer.alloc(8,1),clock:()=>mono});
let first,second;const frames=[];const timed=[];const bytes=Buffer.from([0,0,0,1,0x65,1,2,3]);const frame={codec:"h264",width:16,height:16,keyframe:true,sourceTimestamp:123456,data:bytes};
try {
 const a=source.attach();a.onMedia(item=>timed.push({consumer:"first",timestampMs:item.timestampMs}));first=openReadableFromConsumer(a,{objectMode:true});first.on("error",()=>{});first.on("data",f=>frames.push({consumer:"first",frame:f,receipt:freshness.record(f)}));
 stream.emit("video",frame);await new Promise(r=>setImmediate(r));assert.equal(frames.length,1);first.destroy();
 wall=6000;mono=6000000n;
 const b=source.attach();b.onMedia(item=>timed.push({consumer:"reused",timestampMs:item.timestampMs}));second=openReadableFromConsumer(b,{objectMode:true});second.on("error",()=>{});second.on("data",f=>frames.push({consumer:"reused",frame:f,receipt:freshness.record(f)}));
 await new Promise(r=>setImmediate(r));assert.equal(frames.length,2);assert.equal(frames[1].frame,frame);assert.equal(timed[1].timestampMs,1000);assert.equal(frames[1].receipt.ingressMonoUs,6000000n);
 const result={sdkSourceHead:execFileSync("git",["-C",sdkRoot,"rev-parse","HEAD"],{encoding:"utf8"}).trim(),scope:"offline-real-SDK-source-aged-prime",liveDevice:false,sourceTimestamp:frame.sourceTimestamp,sameFrameObject:frames[0].frame===frames[1].frame,readableFrameKeys:Object.keys(frames[1].frame),originalSourceTimestampMs:timed[0].timestampMs,retainedReplayTimestampMs:timed[1].timestampMs,deliveryWallMs:wall,knownRetainedAgeMs:wall-timed[1].timestampMs,firstBridgeReceiptMonoUs:String(frames[0].receipt.ingressMonoUs),replayedBridgeReceiptMonoUs:String(frames[1].receipt.ingressMonoUs),frameIds:frames.map(x=>String(x.receipt.frameId)),claimedBridgeIngressAgeUs:String(mono-frames[1].receipt.ingressMonoUs),sourceState:source.state};
 const outIndex=process.argv.indexOf("--out");if(outIndex>=0){if(!process.argv[outIndex+1])throw new Error("--out requires a filename");writeFileSync(process.argv[outIndex+1],JSON.stringify(result,null,2));}console.log(JSON.stringify(result));
} finally {first?.destroy();second?.destroy();source.dispose();Date.now=dateNow;}
