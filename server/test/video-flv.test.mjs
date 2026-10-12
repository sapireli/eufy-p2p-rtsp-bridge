import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { newVideoFlvMuxer } from "../src/video-flv.mjs";
import { annexBNals } from "../src/video-matroska.mjs";
import { EncodedFrameReader } from "../src/encoded-frame-reader.mjs";
import { stampVideoFrame, parseFrameStamp } from "../src/frame-stamp.mjs";
function fixture(codec, dir, width=128, height=96) {
  const stats=join(dir,`${codec}-${width}-stats.txt`);
  const args=["-hide_banner","-v","error","-f","lavfi","-i",`testsrc2=size=${width}x${height}:rate=15`,"-frames:v","3","-c:v",codec==="h264"?"libx264":"libx265"];
  if(codec==="h265")args.push("-x265-params","pools=1:frame-threads=1:bframes=0:keyint=3:log-level=error");
  args.push("-bf","0","-g","3","-fps_mode:v","passthrough","-enc_time_base:v","1:15","-map","0:v:0","-f","data","-stats_mux_pre",stats,"-stats_mux_pre_fmt","{n} {pts} {tb} {size}","pipe:1");
  const result=spawnSync("ffmpeg",args,{maxBuffer:4*1024*1024});
  assert.equal(result.status,0,result.stderr.toString());
  const rows=[];const parser=new EncodedFrameReader({onFrame:row=>rows.push(row)});parser.pushData(result.stdout);parser.pushStats(readFileSync(stats));parser.end();
  return rows.map(row=>({data:row.data,codec,width,height}));
}
function hashes(path, format) {
  const result=spawnSync("ffmpeg",["-hide_banner","-v","error","-threads","1","-f",format,"-i",path,"-fps_mode:v","passthrough","-f","framemd5","pipe:1"],{encoding:"utf8",maxBuffer:4*1024*1024});
  assert.equal(result.status,0,result.stderr);
  return result.stdout.split("\n").filter(line=>line&&!line.startsWith("#")).map(line=>line.split(",").at(-1).trim());
}

function tags(bytes) {
 const out=[];
 let at=bytes.subarray(0,3).toString()==="FLV"?13:0;
 while(at<bytes.length){const size=bytes.readUIntBE(at+1,3);assert.ok(at+11+size+4<=bytes.length);out.push({type:bytes[at],timestamp:bytes.readUIntBE(at+4,3)+bytes[at+7]*2**24,data:bytes.subarray(at+11,at+11+size)});assert.equal(bytes.readUInt32BE(at+11+size),size+11);at+=11+size+4;}
 return out;
}
for(const codec of ["h264","h265"])test(`FLV ${codec} preserves complete pictures, SEI, wrap timing and changed codec configuration`,t=>{
 if(spawnSync("ffmpeg",["-version"]).status!==0)return t.skip("ffmpeg unavailable");
 const dir=mkdtempSync(join(tmpdir(),"ewb-flv-"));
 try{
  const rows=fixture(codec,dir),source=[0xfffffff0,51,217],mux=newVideoFlvMuxer({codec});
  if(codec === "h265") for(let irap=16;irap<=21;irap++) {
    const nals=annexBNals(rows[0].data).map(nal=>{
      const copy=Buffer.from(nal),kind=(copy[0]>>1)&63;
      if(kind>=16&&kind<=21)copy[0]=(irap<<1)|(copy[0]&1);
      return Buffer.concat([Buffer.from([0,0,0,1]),copy]);
    });
    const records=tags(newVideoFlvMuxer({codec}).push({data:Buffer.concat(nals),sourceTimestamp:0}));
    assert.equal(records.at(-1).data[0],0x93,`HEVC IRAP ${irap} is a random-access FLV picture`);
  }
  const stamps=rows.map((row,i)=>({serverEpoch:Buffer.alloc(8,1),sourceTimestamp:source[i],ingressMonoUs:100000n+BigInt(i),frameId:BigInt(i+1),maxAgeMs:500}));
  const chunks=rows.map((row,i)=>mux.push({...row,data:stampVideoFrame(row.data,codec,stamps[i]),sourceTimestamp:source[i]}));
  assert.equal(mux.pts90k,233*90);
  const records=tags(Buffer.concat(chunks)),video=records.filter(r=>r.type===9);
  assert.equal(records.filter(r=>r.type===18).length,1);
  assert.ok(records[0].data.includes(Buffer.from("videocodecid")));
  assert.equal(records[0].data.includes(Buffer.from("audiocodecid")),false);
  assert.deepEqual(video.slice(1).map(r=>r.timestamp),[0,67,233]);
  for(const [i,r] of video.slice(1).entries()){
   const nals=[];for(let at=5;at<r.data.length;){const n=r.data.readUInt32BE(at);at+=4;nals.push(Buffer.from([0,0,0,1]),r.data.subarray(at,at+n));at+=n;}
   const decoded=parseFrameStamp(Buffer.concat(nals),codec);assert.equal(decoded.frameId,BigInt(i+1));assert.equal(decoded.sourceTimestamp,source[i]);
  }
  const path=join(dir,"video.flv"),raw=join(dir,"video.raw");writeFileSync(path,Buffer.concat(chunks));writeFileSync(raw,Buffer.concat(rows.map(r=>r.data)));
  assert.deepEqual(hashes(path,"flv"),hashes(raw,codec==="h264"?"h264":"hevc"));
  const probe=spawnSync("ffprobe",["-v","error","-show_packets","-select_streams","v","-show_entries","packet=pts","-of","json",path],{encoding:"utf8"});
  assert.equal(probe.status,0,probe.stderr);assert.deepEqual(JSON.parse(probe.stdout).packets.map(r=>r.pts),[0,67,233]);
  assert.throws(()=>mux.push({...rows[1],sourceTimestamp:216}),/discontinuity/);
  const resized=fixture(codec,dir,160,120)[0],resize=mux.push({...resized,sourceTimestamp:284});
  const changed=tags(resize);assert.equal(changed.length,2);assert.equal(changed[0].timestamp,284-0xfffffff0+2**32);
  writeFileSync(path,Buffer.concat([...chunks,resize]));
  const dims=spawnSync("ffprobe",["-v","error","-show_frames","-select_streams","v","-show_entries","frame=width,height","-of","json",path],{encoding:"utf8"});assert.equal(dims.status,0,dims.stderr);assert.deepEqual(JSON.parse(dims.stdout).frames.map(f=>[f.width,f.height]),[[128,96],[128,96],[128,96],[160,120]]);
  assert.throws(()=>newVideoFlvMuxer({codec,maxFrameBytes:1}).push({...rows[0],sourceTimestamp:0}),/size bound/);
  assert.throws(()=>newVideoFlvMuxer({codec}).push({...rows[1],sourceTimestamp:0}),/parameter sets/);
 }finally{rmSync(dir,{recursive:true,force:true});}
});
