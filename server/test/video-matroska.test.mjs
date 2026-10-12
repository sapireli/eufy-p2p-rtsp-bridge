import test from "node:test";
import assert from "node:assert/strict";
import {spawnSync,spawn} from "node:child_process";
import {mkdtempSync,readFileSync,writeFileSync,rmSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {newVideoMatroskaMuxer} from "../src/video-matroska.mjs";
import {newVideoMpegTsMuxer} from "../src/video-mpegts.mjs";
import {EncodedFrameReader} from "../src/encoded-frame-reader.mjs";

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
for(const codec of ["h264","h265"])test(`complete Matroska ${codec} packets preserve source wrap timing, codec configuration and decoded pictures`,(t)=>{
  if(spawnSync("ffmpeg",["-version"]).status!==0)return t.skip("ffmpeg unavailable");
  const dir=mkdtempSync(join(tmpdir(),"ewb-matroska-"));
  try {
    const rows=fixture(codec,dir),source=[0xfffffff0,51,217];
    const mux=newVideoMatroskaMuxer({codec});
    const chunks=rows.map((row,i)=>mux.push({...row,sourceTimestamp:source[i]}));
    assert.equal(mux.pts90k,233*90);
    const mkv=join(dir,"video.mkv"),raw=join(dir,"video.raw");writeFileSync(mkv,Buffer.concat(chunks));writeFileSync(raw,Buffer.concat(rows.map(row=>row.data)));
    const probe=spawnSync("ffprobe",["-v","error","-show_packets","-select_streams","v","-show_entries","packet=pts,size","-of","json",mkv],{encoding:"utf8"});
    assert.equal(probe.status,0,probe.stderr);
    assert.deepEqual(JSON.parse(probe.stdout).packets.map(packet=>packet.pts),[0,67,233]);
    assert.deepEqual(hashes(mkv,"matroska"),hashes(raw,codec==="h264"?"h264":"hevc"));
    assert.throws(()=>mux.push({...rows[1],sourceTimestamp:216}),/discontinuity/);
    assert.throws(()=>newVideoMatroskaMuxer({codec,maxFrameBytes:1}).push({...rows[0],sourceTimestamp:0}),/size bound/);
    assert.throws(()=>newVideoMatroskaMuxer({codec}).push({...rows[1],sourceTimestamp:0}),/parameter sets/);
    // In-band resolution change remains valid; length size and chroma/bit depth
    // stay constant, and the decoder sees the actual updated SPS/VPS/PPS.
    const resized=fixture(codec,dir,160,120)[0];
    const resizedBytes=mux.push({...resized,sourceTimestamp:284});
    writeFileSync(mkv,Buffer.concat([...chunks,resizedBytes]));
    const dimensions=spawnSync("ffprobe",["-v","error","-show_frames","-select_streams","v","-show_entries","frame=width,height","-of","json",mkv],{encoding:"utf8"});
    assert.equal(dimensions.status,0,dimensions.stderr);
    assert.deepEqual(JSON.parse(dimensions.stdout).frames.map(frame=>[frame.width,frame.height]),[[128,96],[128,96],[128,96],[160,120]]);
  } finally {rmSync(dir,{recursive:true,force:true});}
});

test("Matroska finite complete AU releases the last decoded picture before a later AU or EOF",async(t)=>{
  if(spawnSync("ffmpeg",["-version"]).status!==0)return t.skip("ffmpeg unavailable");
  const dir=mkdtempSync(join(tmpdir(),"ewb-matroska-live-"));
  async function run(rows,format,mux) {
    const child=spawn("ffmpeg",["-hide_banner","-v","error","-copyts","-thread_type:v","slice","-threads","1","-f",format,"-analyzeduration","100000","-probesize","262144","-i","pipe:0","-frames:v","3","-fps_mode:v","passthrough","-threads:v","1","-f","rawvideo","pipe:1"],{stdio:["pipe","pipe","pipe"]});
    let bytes=0,error="";child.stdout.on("data",b=>bytes+=b.length);child.stderr.on("data",b=>error+=b.toString());child.stdin.on("error",()=>{});
    try {
      for(let i=0;i<rows.length;i++) {child.stdin.write(mux.push({...rows[i],sourceTimestamp:1000+i*67}));await new Promise(done=>setTimeout(done,80));}
      await new Promise(done=>setTimeout(done,300));
      return {frames:bytes/(128*96*3/2),inputEnded:child.stdin.writableEnded,error};
    } finally {child.kill("SIGKILL");child.stdin.destroy();}
  }
  try {
    const rows=fixture("h264",dir);
    rows[0].data=Buffer.concat([Buffer.from([0,0,0,1,12]),Buffer.alloc(300000,255),Buffer.from([128]),rows[0].data]);
    const ts=await run(rows,"mpegts",newVideoMpegTsMuxer({codec:"h264",maxPesPayloadBytes:63000}));
    const mkv=await run(rows,"matroska",newVideoMatroskaMuxer({codec:"h264"}));
    assert.equal(ts.inputEnded,false);assert.equal(mkv.inputEnded,false);
    assert.equal(ts.frames,2,`full Annex-B parser retains the final unit: ${ts.error}`);
    assert.equal(mkv.frames,3,`complete packet has no next-AU hold: ${mkv.error}`);
  } finally {rmSync(dir,{recursive:true,force:true});}
});
