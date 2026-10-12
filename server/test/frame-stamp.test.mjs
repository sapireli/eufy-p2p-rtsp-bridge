import test from "node:test";
import assert from "node:assert/strict";
import { stampVideoFrame, parseFrameStamp } from "../src/frame-stamp.mjs";

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
