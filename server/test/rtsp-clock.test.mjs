import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

test("RTSP clock observer handles fragmented keepalive responses mixed with RTP", () => {
  const script = fileURLToPath(new URL("../scripts/measure-rtsp-clock.py", import.meta.url));
  const python = String.raw`
import importlib.util,sys,struct
spec=importlib.util.spec_from_file_location('clock',sys.argv[1]);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
rtp=b'\x80\xe0'+b'\x00'*10
packet=b'$'+b'\x00'+struct.pack('!H',len(rtp))+rtp
response=b'RTSP/1.0 200 OK\r\nCSeq: 7\r\nContent-Length: 4\r\n\r\n$xxx'
for cut in range(len(response)):
 assert m.play_message(response[:cut]) is None
channel,body,left=m.play_message(response+packet)
assert channel is None and body==b'$xxx' and left==packet
for cut in range(len(packet)):
 assert m.play_message(packet[:cut]) is None
channel,body,left=m.play_message(packet+response)
assert channel==0 and body==rtp and left==response
assert m.timestamp_delta(5,0xfffffffe)==7
assert m.timestamp_delta(100,101)==-1
for bad in [b'junk',b'RTSP/1.0 454 Session Not Found\r\n\r\n',b'RTSP/1.0 200 OK\r\nContent-Length: -1\r\n\r\n']:
 try: m.play_message(bad)
 except ValueError: pass
 else: raise AssertionError('accepted invalid RTSP response')
`;
  const r = spawnSync("python3", ["-c", python, script], { encoding: "utf8" });
  assert.equal(r.status, 0, r.stderr);
});
