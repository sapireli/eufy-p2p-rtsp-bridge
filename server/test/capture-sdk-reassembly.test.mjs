import test from "node:test";
import assert from "node:assert/strict";
import {captureReassembly} from "../scripts/capture-sdk-reassembly.mjs";

function fixture(){
  let ns=0n;
  class Session {
    pendingByDataType=new Map();lastSeqByType=new Map();traceId="synthetic-session";
    reassemble(type,body){
      ns+=10n;const prior=this.pendingByDataType.get(type);
      const all=prior?Buffer.concat([prior.buf,body]):body;
      if(all.length<8){this.pendingByDataType.set(type,{buf:all});return "pending"}
      this.pendingByDataType.delete(type);
      const result=this.handleFrame({commandId:1300,channel:1,bytesToRead:8},all,type);ns+=2n;return result;
    }
    handleFrame(){ns+=1000n;return 123}
  }
  return{sdk:{P2PSession:Session},Session,clock:()=>ns};
}
function discardCache(){clearTimeout(globalThis.__ewbGapCapture?.guard);delete globalThis.__ewbGapCapture}

test("assembly trace excludes handler/consumer time and restores exact original methods",async()=>{
  const f=fixture(),session=new f.Session(),original=f.Session.prototype.reassemble,handle=f.Session.prototype.handleFrame;
  try{
    const finished=captureReassembly(f.sdk,.01,"test-capture",10,f.clock);
    assert.equal(session.reassemble(1,Buffer.alloc(4)),"pending");
    session.lastSeqByType.set(1,12);
    assert.equal(session.reassemble(1,Buffer.alloc(4)),123);
    const header=await finished,rows=globalThis.__ewbGapCapture.rows;
    assert.equal(header.rowCount,1);assert.equal(header.overflow,0);
    assert.equal(rows[0].assemblySyncMs,20/1e6);
    assert.equal(rows[0].finalDatagramBeforeDispatchMs,10/1e6);
    assert.equal(rows[0].handlerAndConsumersSyncMs,1000/1e6);
    assert.equal(rows[0].estimatedCopiedBytes,8);assert.equal(rows[0].datagramsObserved,2);assert.equal(rows[0].finalSequence,12);
    assert.equal(rows[0].partialAtCaptureStart,false);
    assert.equal(f.Session.prototype.reassemble,original);assert.equal(f.Session.prototype.handleFrame,handle);
  }finally{globalThis.__ewbReassemblyCleanup?.();discardCache()}
});

test("assembly records are bounded and an already-partial first frame is identified",async()=>{
  const f=fixture(),session=new f.Session();session.pendingByDataType.set(1,{buf:Buffer.alloc(4)});
  try{
    const finished=captureReassembly(f.sdk,.01,"bounded-capture",1,f.clock);
    session.reassemble(1,Buffer.alloc(4));session.reassemble(1,Buffer.alloc(8));
    const header=await finished;assert.equal(header.rowCount,1);assert.equal(header.overflow,1);
    assert.equal(globalThis.__ewbGapCapture.rows[0].partialAtCaptureStart,true);
  }finally{globalThis.__ewbReassemblyCleanup?.();discardCache()}
});


test("assembly observer accounts for one final chunk-list copy and excludes incomplete append bytes", async()=>{
 let ns=0n;
 class Session {
  pendingByDataType=new Map();lastSeqByType=new Map();
  reassemble(type,body){
   ns+=10n;
   const p=this.pendingByDataType.get(type)??{header:{commandId:1300,channel:1,bytesToRead:8},chunks:[],byteLength:0};
   p.chunks.push(body);p.byteLength+=body.length;
   if(p.byteLength<8){this.pendingByDataType.set(type,p);return;}
   this.pendingByDataType.delete(type);this.handleFrame(p.header,Buffer.concat(p.chunks,p.byteLength),type);
  }
  handleFrame(){ns+=1000n;}
 }
 const session=new Session();
 try{
  const done=captureReassembly({P2PSession:Session},.01,"linear-copy",10,()=>ns);
  session.reassemble(1,Buffer.alloc(2));session.reassemble(1,Buffer.alloc(2));session.reassemble(1,Buffer.alloc(4));
  await done;const row=globalThis.__ewbGapCapture.rows[0];
  assert.equal(row.datagramsObserved,3);assert.equal(row.estimatedCopiedBytes,8);assert.equal(row.copyAccountingMode,"chunk-list");
  assert.equal(row.assemblySyncMs,30/1e6);
 }finally{globalThis.__ewbReassemblyCleanup?.();discardCache();}
});
