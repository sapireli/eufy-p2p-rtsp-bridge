import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { stampVideoFrame } from "../src/frame-stamp.mjs";

test("freshness observer depayloads real stamps, rejects loss and bounds clock/receipt timing", () => {
  const vcl = Buffer.from([0,0,0,1,0x65,0x88]);
  const record = {serverEpoch:Buffer.from("0000000100000002","hex"),sourceTimestamp:0xfffffff0,ingressMonoUs:123456789n,frameId:9n,maxAgeMs:500};
  const encoded = stampVideoFrame(vcl,"h264",record);
  const sei = encoded.subarray(4,encoded.length-vcl.length);
  const fixture = {sei:sei.toString("hex"),epoch:record.serverEpoch.toString("hex"),script:fileURLToPath(new URL("../scripts/measure-frame-freshness.py",import.meta.url))};
  const result = spawnSync("python3",["-c",String.raw`
import json,sys,importlib.util,struct,time,socket,threading
from http.server import ThreadingHTTPServer,BaseHTTPRequestHandler
x=json.load(sys.stdin)
spec=importlib.util.spec_from_file_location("freshness",x["script"]);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
sei=bytes.fromhex(x["sei"])
assert m.parse_stamp(sei)==dict(serverEpoch=x["epoch"],sourceTimestamp=0xfffffff0,ingressMonoUs=123456789,frameId=9,maxAgeMs=500)
assert m.parse_stamp(sei[:20]) is None
def packet(seq,ts,data,marker=False,extra=False):
    if extra:
        return bytes([0xb1,96|int(marker)*128])+struct.pack("!HII",seq,ts,55)+struct.pack("!I",42)+struct.pack("!HHI",1,1,0x1234)+data+bytes([0,0,0,4])
    return bytes([0x80,96|int(marker)*128])+struct.pack("!HII",seq,ts,55)+data
vcl=b"\x65\x88"
a=m.H264Frames();assert a.push(packet(1,100,sei)) is None
out=a.push(packet(2,100,vcl,True,True));assert out["stamp"]["frameId"]==9 and out["transportValid"]
stap=b"\x78"+struct.pack("!H",len(sei))+sei+struct.pack("!H",len(vcl))+vcl
out=m.H264Frames().push(packet(65535,200,stap,True));assert out["stamp"]["frameId"]==9
# SEI reconstructed from FU-A; exact original byte sequence including escapes.
a=m.H264Frames();parts=[sei[1:20],sei[20:40],sei[40:]]
for i,part in enumerate(parts):
    flags=(128 if i==0 else 64 if i==2 else 0)|6
    assert a.push(packet(i,300,bytes([0x7c,flags])+part)) is None
assert a.push(packet(3,300,vcl,True))["stamp"]["frameId"]==9
# A missing fragment must NEVER permit a freshness stamp for that picture.
a=m.H264Frames();a.push(packet(1,400,sei));out=a.push(packet(3,400,vcl,True))
assert not out["transportValid"] and out["stamp"] is None
a=m.H264Frames();a.push(packet(1,400,bytes([0x7c,0x86])+sei[1:20]));out=a.push(packet(2,400,vcl,True))
assert out["stamp"] is None
assert m.H264Frames().push(packet(1,400,b"\x78\x00\xff\x06",True))["stamp"] is None
try:m.H264Frames(2).push(packet(1,10,sei,True));raise AssertionError("unbounded AU accepted")
except ValueError:pass
# Clock interval must include true offset, not just midpoint.
stamp=m.parse_stamp(sei);clock=dict(epoch=x["epoch"],offsetLowUs=1000,offsetHighUs=3000,sampleLocalUs=123550000)
low,high=m.age_bounds(stamp,123550000,clock,0)
assert abs(low-94.211)<0.001 and abs(high-96.211)<0.001
try:m.age_bounds(dict(stamp,serverEpoch="ffffffffffffffff"),123550000,clock);raise AssertionError("epoch mismatch accepted")
except ValueError:pass

class ClockHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        body=json.dumps(dict(serverEpoch=x["epoch"],monotonicUs=time.perf_counter_ns()//1000+1000000)).encode()
        self.send_response(200);self.send_header("Content-Length",str(len(body)));self.end_headers();self.wfile.write(body)
    def log_message(self,*args):pass
http=ThreadingHTTPServer(("127.0.0.1",0),ClockHandler);threading.Thread(target=http.serve_forever,daemon=True).start()
clockurl=f"http://127.0.0.1:{http.server_port}/api/clock"
sample=m.sample_clock(clockurl);assert sample["offsetLowUs"]<=1000000<=sample["offsetHighUs"]

# Actual RTSP handshake/interleaved parsing with response+media fragmentation.
listener=socket.socket();listener.bind(("127.0.0.1",0));listener.listen();port=listener.getsockname()[1]
def escape(payload):
    out=bytearray();zeros=0
    for b in payload:
        if zeros>=2 and b<=3:out.append(3);zeros=0
        out.append(b);zeros=zeros+1 if b==0 else 0
    return bytes(out)
def live_sei(i):
    payload=m.UUID+bytes([1,1])+struct.pack("!H",500)+bytes.fromhex(x["epoch"])+struct.pack("!IQQ",1000+i*67,time.perf_counter_ns()//1000+1000000-10000,i)
    return b"\x06"+escape(b"\x05\x30"+payload+b"\x80")
def rtsp():
    conn,_=listener.accept();pending=b""
    with conn:
        for _ in range(3):
            while b"\r\n\r\n" not in pending:pending+=conn.recv(65536)
            raw,pending=pending.split(b"\r\n\r\n",1);method=raw.split(b" ")[0]
            cseq=next(line.split(b":",1)[1].strip() for line in raw.split(b"\r\n") if line.lower().startswith(b"cseq:"))
            body=b"v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=control:trackID=0\r\n" if method==b"DESCRIBE" else b""
            response=b"RTSP/1.0 200 OK\r\nCSeq: "+cseq+b"\r\nSession: test\r\nContent-Length: "+str(len(body)).encode()+b"\r\n\r\n"+body
            conn.sendall(response[:7]);conn.sendall(response[7:])
        for i in range(1,4):
            s=live_sei(i);body=b"\x78"+struct.pack("!H",len(s))+s+struct.pack("!H",len(vcl))+vcl
            p=packet(i,6000*i,body,True);wrapped=b"$\x00"+struct.pack("!H",len(p))+p
            for at in range(0,len(wrapped),11):conn.sendall(wrapped[at:at+11])
            time.sleep(.01)
        time.sleep(.5)
threading.Thread(target=rtsp,daemon=True).start()
result=m.measure(f"rtsp://127.0.0.1:{port}/fixture",clockurl,.15,0,100)
assert result["stampedFrames"]==3 and result["frameIdDuplicates"]==0 and result["frameIdRegressions"]==0,result
assert result["ageUpperMs"]["maximum"]<200,result
assert result["presentationMeasured"] is False and result["scope"]=="bridge-ingress-to-observer-receipt"
listener.close();http.shutdown()
print("observer clock, SEI, FU-A, STAP-A, packet-loss, limits and actual RTSP tests passed")
`],{input:JSON.stringify(fixture),encoding:"utf8",timeout:10000});
  assert.equal(result.status,0,result.stderr || result.error?.message);
  assert.match(result.stdout,/tests passed/);
});
