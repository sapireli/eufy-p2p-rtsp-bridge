import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { stampVideoFrame, parseFrameStamp, firstVclNal } from "../src/frame-stamp.mjs";

const record = { serverEpoch: Buffer.from("0000000100000002", "hex"), sourceTimestamp: 0xfffffffe, ingressMonoUs: 0x100000002n, frameId: (1n << 64n) - 1n, maxAgeMs: 500 };
const video = Buffer.from([0, 0, 0, 1, 0x65, 0x88, 0x80]);
for (const codec of ["h264", "h265"]) {
  test(`${codec} stamp survives emulation prevention and preserves original encoded data`, () => {
    const input = codec === "h264" ? video : Buffer.from([0, 0, 0, 1, 38, 1, 0x80]);
    const stamped = stampVideoFrame(input, codec, record);
    assert.deepEqual(stamped.subarray(-input.length), input);
    assert.deepEqual(parseFrameStamp(stamped, codec), { version: 1, scope: 1, ...record });
    assert.ok(stamped.includes(Buffer.from([0, 0, 3, 0])), "zero-filled clock fields are escaped");
    assert.deepEqual(parseFrameStamp(stamped.subarray(4, stamped.length - input.length), codec), { version: 1, scope: 1, ...record });
    assert.equal(parseFrameStamp(stamped.subarray(0, 30), codec), null, "truncated SEI cannot confer freshness");
    const wrongUuid = Buffer.from(stamped);
    wrongUuid[codec === "h264" ? 7 : 8] ^= 1;
    assert.equal(parseFrameStamp(wrongUuid, codec), null);
  });
  test(`${codec} stamp follows encoder prefix SEIs and precedes the first VCL`, () => {
    const prefix = codec === "h264" ? Buffer.from([0,0,0,1,9,0xf0,0,0,0,1,7,0x88,0,0,0,1,6,5,1,0x44,0x80]) : Buffer.from([0,0,0,1,70,1,0x50,0,0,0,1,64,1,0x88,0,0,0,1,78,1,5,1,0x44,0x80]);
    const vcl = codec === "h264" ? video : Buffer.from([0,0,0,1,38,1,0x80]);
    const input = Buffer.concat([prefix, vcl]);
    const stamped = stampVideoFrame(input, codec, record);
    assert.deepEqual(stamped.subarray(0, prefix.length), prefix);
    assert.deepEqual(stamped.subarray(-vcl.length), vcl);
    const added = stamped.subarray(prefix.length, stamped.length-vcl.length);
    assert.deepEqual(parseFrameStamp(added, codec), {version:1,scope:1,...record});
    assert.deepEqual(Buffer.concat([stamped.subarray(0,prefix.length),stamped.subarray(-vcl.length)]),input);
  });
}
test("unrelated/malformed SEI and unsupported scope/version do not confer freshness", () => {
  assert.equal(parseFrameStamp(video, "h264"), null);
  assert.equal(parseFrameStamp(Buffer.from([6, 5, 48, 0, 0, 3]), "h264"), null);
  const noEscapes = { ...record, serverEpoch: Buffer.alloc(8, 0x44), sourceTimestamp: 0x44444444, ingressMonoUs: 0x4444444444444444n, frameId: 0x4444444444444444n };
  const stamped = stampVideoFrame(video, "h264", noEscapes);
  for (const index of [23, 24]) { // prefix + NAL + type + size + UUID, then version/scope
    const unsupported = Buffer.from(stamped); unsupported[index] = 2;
    assert.equal(parseFrameStamp(unsupported, "h264"), null);
  }
});
test("stamp validates clocks, age, epoch, codec and payload", () => {
  for (const bad of [{ frameId: -1n }, { ingressMonoUs: 1 }, { frameId: 1n << 64n }, { sourceTimestamp: -1 }, { sourceTimestamp: 2 ** 32 }, { maxAgeMs: 0 }, { maxAgeMs: 65536 }, { serverEpoch: Buffer.alloc(7) }]) {
    assert.throws(() => stampVideoFrame(video, "h264", { ...record, ...bad }));
  }
  assert.throws(() => stampVideoFrame(video, "vp9", record));
  assert.throws(() => stampVideoFrame(Buffer.alloc(0), "h264", record));
});

for (const codec of ["h264", "h265"]) {
  test(`${codec} prefix reader stops before a large coded slice and preserves mixed start codes`, () => {
    const prefix = codec === "h264" ? Buffer.from([0,0,1,9,0xf0,0,0,0,1,6,5,1,0x44,0x80]) : Buffer.from([0,0,1,70,1,0x50,0,0,0,1,78,1,5,1,0x44,0x80]);
    const vcl = codec === "h264" ? Buffer.from([0,0,1,0x65]) : Buffer.from([0,0,1,38,1]);
    const input = Buffer.concat([prefix,vcl,Buffer.alloc(590000,0x7f)]);
    const originalIndexOf = input.indexOf;
    const offsets=[];
    input.indexOf=function(value,offset) { offsets.push(offset); assert.ok(offset<=prefix.length,"no search begins inside the coded slice"); return originalIndexOf.call(this,value,offset); };
    const nal=firstVclNal(input,codec);
    assert.equal(nal.from,prefix.length);assert.equal(nal.type,codec==="h264"?5:19);assert.equal(nal.keyframe,true);
    assert.equal(offsets.length,3);
    const stamped=stampVideoFrame(input,codec,record);
    assert.deepEqual(stamped.subarray(0,prefix.length),prefix);
    assert.deepEqual(stamped.subarray(-input.length+prefix.length),input.subarray(prefix.length));
  });
}
test("keyframe detection uses actual VCL type rather than parameter-set presence", () => {
  const unit=type=>Buffer.from([0,0,0,1,type,0x80]);
  assert.equal(firstVclNal(Buffer.concat([unit(7),unit(1)]),"h264").keyframe,false);
  assert.equal(firstVclNal(unit(5),"h264").keyframe,true);
  for(let type=16;type<=21;type++)assert.equal(firstVclNal(Buffer.from([0,0,1,type<<1,1]),"h265").keyframe,true);
  assert.equal(firstVclNal(Buffer.from([0,0,1,2,1]),"h265").keyframe,false);
  assert.equal(firstVclNal(Buffer.from([0,0,1,6]),"h264"),null);
  assert.equal(firstVclNal(Buffer.from([0,0,1,38]),"h265"),null);
});

for (const [codec,encoder] of [["h264","libx264"],["h265","libx265"]]) {
  test(`${codec} prefix stamping preserves real FFmpeg decoded pixels`, t => {
    if(spawnSync("ffmpeg",["-version"]).status!==0)return t.skip("ffmpeg unavailable");
    const format=codec==="h264"?"h264":"hevc";
    const extra=codec==="h265"?["-x265-params","pools=none:frame-threads=1:log-level=error"]:[];
    const encoded=spawnSync("ffmpeg",["-hide_banner","-v","error","-f","lavfi","-i","testsrc2=size=128x96:rate=1","-frames:v","1","-c:v",encoder,"-preset","ultrafast",...extra,"-f",format,"pipe:1"],{maxBuffer:1024*1024});
    assert.equal(encoded.status,0,encoded.stderr?.toString());
    assert.equal(firstVclNal(encoded.stdout,codec).keyframe,true);
    const stamped=stampVideoFrame(encoded.stdout,codec,record);
    const pixels=input=>{
      const decoded=spawnSync("ffmpeg",["-hide_banner","-v","error","-threads","1","-f",format,"-i","pipe:0","-frames:v","1","-f","framemd5","pipe:1"],{input,maxBuffer:1024*1024});
      assert.equal(decoded.status,0,decoded.stderr?.toString());
      return decoded.stdout.toString().split("\n").filter(line=>line && !line.startsWith("#")).map(line=>line.split(",").at(-1).trim());
    };
    assert.deepEqual(pixels(stamped),pixels(encoded.stdout));
  });
}
