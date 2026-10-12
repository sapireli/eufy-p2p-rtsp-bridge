import test from "node:test";
import assert from "node:assert/strict";
import { hevcProfileTierLevel } from "../src/codec-capability.mjs";
const actual = Buffer.from("QgEBIUAAAAMAkAAAAwAAAwCWoAKAgC0WeluRKFzcBAQEEAAAAwAQAAADAZCA", "base64");
test("actual native Balcony SPS profile is Main, level5.0, high tier", () => {
  assert.deepEqual(hevcProfileTierLevel(actual), {profile:1,level:150,tier:1});
});
test("bounded HEVC PTL decoding removes emulation prevention and retains high tier", () => {
  const rbsp=Buffer.from([1,0x22,0,0,0,0,0,0,0,0,0,0,123]);
  const escaped=[];let zeros=0;
  for(const byte of rbsp){if(zeros>=2&&byte<=3){escaped.push(3);zeros=0;}escaped.push(byte);zeros=byte===0?zeros+1:0;}
  assert.deepEqual(hevcProfileTierLevel(Buffer.from([0x42,1,...escaped])),{profile:2,level:123,tier:1});
  assert.deepEqual(hevcProfileTierLevel(Buffer.concat([actual,Buffer.alloc(60000)])),{profile:1,level:150,tier:1});
});
test("unknown, truncated, malformed, wrong NAL and unbounded SPS do not claim capability", () => {
  assert.equal(hevcProfileTierLevel(actual.subarray(0,14)),undefined);
  assert.equal(hevcProfileTierLevel(Buffer.from([0x42,1,1,1,0,0,3,4,...Array(20).fill(0)])),undefined);
  for(const offset of [0,1,3]){const copy=Buffer.from(actual);copy[offset]=offset===0?0x40:offset===1?0:0xC1;assert.equal(hevcProfileTierLevel(copy),undefined);}
  assert.equal(hevcProfileTierLevel(Buffer.alloc(65536)),undefined);
});
