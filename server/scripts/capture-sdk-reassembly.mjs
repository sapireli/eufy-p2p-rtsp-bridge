#!/usr/bin/env node
// Bounded logical-frame assembly diagnostics; no byte retention or camera controls.
import { createRequire } from "node:module";
import { writeFileSync, mkdirSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { pathToFileURL } from "node:url";
import { randomUUID } from "node:crypto";
import { exportCaptureRows, clearCaptureCache } from "./inspector-capture-export.mjs";

/** Time synchronous assembly segments before frame dispatch. Consumer work and
 * record allocation are excluded; OS preemption and timer-read overhead remain.
 * Contexts and counters are reused: only completed video frames allocate rows.
 */
export async function captureReassembly(sdk, seconds, id, maxRows = 6000, clock = () => process.hrtime.bigint()) {
  if(globalThis.__ewbReassemblyCleanup || globalThis.__ewbProducerCleanup || globalThis.__ewbGapCleanup || globalThis.__ewbGapCapture) throw Error("Another observer is active");
  const proto=sdk.P2PSession.prototype,originalReassemble=proto.reassemble,originalHandle=proto.handleFrame;
  if(typeof originalReassemble!=="function" || typeof originalHandle!=="function")throw Error("SDK assembly methods unavailable");
  const started=Date.now(),mono=clock(),cpuStart=process.cpuUsage(),rows=[],sessions=[],owners=new WeakMap();
  const limits=[5000,10000,25000,50000,100000,250000,500000,1000000,2000000,5000000,10000000,20000000,50000000,100000000,Infinity];
  const pool=Array.from({length:8},()=>({}));let depth=0,overflow=0,cleaned=false;
  const now=()=>Number(clock()-mono);
  const metric=()=>({cpuNs:0,fragments:0,copies:0,partial:false});
  function session(owner) {
    let state=owners.get(owner);
    if(!state){state={id:"session-"+(sessions.length+1),traceId:owner.traceId,types:Array.from({length:4},metric),calls:0,totalNs:0,maxNs:0,histogram:new Array(limits.length).fill(0)};owners.set(owner,state);sessions.push(state)}
    return state;
  }
  function reset(m){m.cpuNs=0;m.fragments=0;m.copies=0;m.partial=false}
  const wrappedReassemble=function(dataType,body) {
    if(depth>=pool.length)return originalReassemble.call(this,dataType,body);
    const state=session(this),m=state.types[dataType]??(state.types[dataType]=metric()),pending=this.pendingByDataType?.get(dataType);
    if(m.fragments===0 && pending)m.partial=true;
    m.fragments++;if(pending)m.copies+=pending.buf.length+body.length;
    const q=pool[depth++];q.owner=this;q.state=state;q.metric=m;q.type=dataType;q.bodyBytes=body.length;q.sequence=this.lastSeqByType?.get(dataType);q.assemblyNs=0;q.segmentStarted=now();
    try{return originalReassemble.call(this,dataType,body)}finally{
      const tail=now()-q.segmentStarted;m.cpuNs+=tail;
      const elapsed=q.assemblyNs+tail;state.calls++;state.totalNs+=elapsed;state.maxNs=Math.max(state.maxNs,elapsed);
      let bin=0;while(elapsed>limits[bin])bin++;state.histogram[bin]++;
      if(!this.pendingByDataType?.has(dataType))reset(m);
      depth--;
    }
  };
  const wrappedHandle=function(header,payload,dataType) {
    const q=depth>0?pool[depth-1]:undefined;
    if(!q || q.owner!==this)return originalHandle.call(this,header,payload,dataType);
    const at=now(),segment=at-q.segmentStarted,m=q.metric;m.cpuNs+=segment;q.assemblyNs+=segment;
    let row;
    if(header.commandId===1300){
      if(rows.length>=maxRows)overflow++;
      else {
        row={session:q.state.id,traceId:q.state.traceId,monoMs:at/1e6,channel:header.channel,logicalBytes:header.bytesToRead,declaredVideoBytes:payload.length>=4?payload.readUInt32LE(0):null,keyframe:payload.length>=5?Boolean(payload[4]&1):null,datagramsObserved:m.fragments,estimatedCopiedBytes:m.copies,partialAtCaptureStart:m.partial,assemblySyncMs:m.cpuNs/1e6,finalDatagramBeforeDispatchMs:segment/1e6,finalDatagramBytes:q.bodyBytes,finalSequence:q.sequence};
        rows.push(row);
      }
    }
    reset(m);m.fragments=1;
    const dispatchAt=now();
    try{return originalHandle.call(this,header,payload,dataType)}finally{
      const returned=now();if(row)row.handlerAndConsumersSyncMs=(returned-dispatchAt)/1e6;
      // Exclude the original handler AND observer bookkeeping from assembly timing.
      q.segmentStarted=returned;
    }
  };
  function cleanup(){if(cleaned)return;cleaned=true;if(proto.reassemble===wrappedReassemble)proto.reassemble=originalReassemble;if(proto.handleFrame===wrappedHandle)proto.handleFrame=originalHandle;if(globalThis.__ewbReassemblyCleanup===cleanup)delete globalThis.__ewbReassemblyCleanup}
  globalThis.__ewbReassemblyCleanup=cleanup;proto.reassemble=wrappedReassemble;proto.handleFrame=wrappedHandle;
  const guard=setTimeout(cleanup,(seconds+5)*1000);guard.unref?.();
  try{
    await new Promise(done=>setTimeout(done,seconds*1000));
    if(cleaned)throw Error("Assembly capture canceled");
    const ended=Date.now(),cpu=process.cpuUsage(cpuStart);cleanup();clearTimeout(guard);
    const cache={id,rows};cache.guard=setTimeout(()=>{if(globalThis.__ewbGapCapture===cache)delete globalThis.__ewbGapCapture},120000);cache.guard.unref?.();globalThis.__ewbGapCapture=cache;
    return{captureId:id,rowCount:rows.length,started,ended,overflow,scope:"logical-frame assembly synchronous elapsed time excluding handler/consumer dispatch; includes OS preemption and observer timer-read overhead; no UDP rows or payload bytes",wholeProcessCpuMs:(cpu.user+cpu.system)/1000,histogramUpperBoundsMs:limits.map(x=>Number.isFinite(x)?x/1e6:null),sessions:sessions.map(s=>({id:s.id,traceId:s.traceId,reassemblyCalls:s.calls,totalAssemblySyncMs:s.totalNs/1e6,maxCallAssemblySyncMs:s.maxNs/1e6,histogram:s.histogram}))};
  }finally{cleanup();clearTimeout(guard)}
}

async function main(){
  const args=process.argv.slice(2),option=(name,fallback)=>{const at=args.indexOf(name);return at<0?fallback:args[at+1]};
  if(args.includes("--help") || !args.length){console.log("Usage: node server/scripts/capture-sdk-reassembly.mjs --inspector http://127.0.0.1:PORT --out FILE [--seconds 60] [--package /opt/eufy-wall-bridge/server/package.json]");return}
  const endpoint=new URL(option("--inspector")),seconds=Number(option("--seconds","60"));
  if(endpoint.protocol!=="http:" || !["127.0.0.1","localhost","[::1]"].includes(endpoint.hostname) || !Number.isInteger(seconds) || seconds<1 || seconds>60)throw Error("Explicit loopback inspector and 1..60 seconds required");
  const WebSocket=createRequire(new URL("../package.json",import.meta.url))("ws"),targets=await(await fetch(new URL("/json/list",endpoint),{signal:AbortSignal.timeout(5000)})).json();
  if(targets.length!==1)throw Error("Expected one inspector target");
  const url=new URL(targets[0].webSocketDebuggerUrl);url.hostname=endpoint.hostname;url.port=endpoint.port;
  const ws=new WebSocket(url);await new Promise((done,fail)=>{ws.once("open",done);ws.once("error",fail)});
  let next=0;const pending=new Map();
  ws.on("message",bytes=>{const reply=JSON.parse(bytes),p=pending.get(reply.id);if(!p)return;pending.delete(reply.id);clearTimeout(p.timer);if(reply.error || reply.result?.exceptionDetails)p.reject(Error(reply.error?.message??reply.result.exceptionDetails.text));else p.resolve(reply.result?.result?.value)});
  function evaluate(expression,awaitPromise=false,timeout=5000){return new Promise((resolve,reject)=>{const id=++next,timer=setTimeout(()=>{pending.delete(id);reject(Error("Inspector timeout"))},timeout);pending.set(id,{resolve,reject,timer});ws.send(JSON.stringify({id,method:"Runtime.evaluate",params:{expression,awaitPromise,returnByValue:true}}))})}
  const id=randomUUID();let capture;
  try{
    const sdk=`process.getBuiltinModule('module').createRequire(${JSON.stringify(option("--package","/opt/eufy-wall-bridge/server/package.json"))})('@mega-yfue/eufy-sdk')`;
    const header=await evaluate(`(${captureReassembly.toString()})(${sdk},${seconds},${JSON.stringify(id)})`,true,(seconds+10)*1000);
    capture=await exportCaptureRows(evaluate,header,{batchSize:100});
  }finally{await evaluate("globalThis.__ewbReassemblyCleanup?.();true").catch(()=>{});await clearCaptureCache(evaluate,id).catch(()=>{});ws.close()}
  const out=resolve(option("--out"));mkdirSync(dirname(out),{recursive:true,mode:0o700});writeFileSync(out,JSON.stringify(capture)+"\n",{mode:0o600});console.log(JSON.stringify({out,started:capture.started,ended:capture.ended,rowCount:capture.rowCount,overflow:capture.overflow,sessions:capture.sessions}));
}
if(process.argv[1] && import.meta.url===pathToFileURL(resolve(process.argv[1])).href)await main();
