import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

test("slow clock refresh cannot hold received RTSP pictures in the observer", (t) => {
  const script = process.env.OBSERVER_SCRIPT ?? fileURLToPath(new URL("../scripts/measure-frame-freshness.py", import.meta.url));
  const result = spawnSync("python3", ["-c", String.raw`
import importlib.util,json,socket,struct,sys,threading,time
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
spec=importlib.util.spec_from_file_location("observer",sys.argv[1]);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
epoch="0000000100000002"; offset=1000000; slow_seen=threading.Event()
class Clock(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.server.armed:
            slow_seen.set();time.sleep(1)
        body=json.dumps(dict(serverEpoch=epoch,monotonicUs=time.perf_counter_ns()//1000+offset)).encode()
        self.send_response(200);self.send_header("Content-Length",str(len(body)));self.end_headers();self.wfile.write(body)
    def log_message(self,*args):pass
http=ThreadingHTTPServer(("127.0.0.1",0),Clock);http.armed=False
threading.Thread(target=http.serve_forever,daemon=True).start()
listener=socket.socket();listener.bind(("127.0.0.1",0));listener.listen()
stop=threading.Event()
def escape(data):
    result=bytearray();zeros=0
    for b in data:
        if zeros>=2 and b<=3:result.append(3);zeros=0
        result.append(b);zeros=zeros+1 if b==0 else 0
    return bytes(result)
def serve():
    conn,_=listener.accept();pending=b""
    try:
        with conn:
            for _ in range(3):
                while b"\r\n\r\n" not in pending:pending+=conn.recv(65536)
                req,pending=pending.split(b"\r\n\r\n",1)
                seq=next(v.split(b":",1)[1].strip() for v in req.split(b"\r\n") if v.lower().startswith(b"cseq:"))
                body=b"v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=control:trackID=0\r\n" if req.startswith(b"DESCRIBE") else b""
                conn.sendall(b"RTSP/1.0 200 OK\r\nCSeq: "+seq+b"\r\nSession: fixture\r\nContent-Length: "+str(len(body)).encode()+b"\r\n\r\n"+body)
            http.armed=True
            i=0;next_frame=time.perf_counter()
            while not stop.is_set():
                i+=1;arrival=time.perf_counter_ns()//1000+offset
                metadata=m.UUID+bytes([1,1])+struct.pack("!H",500)+bytes.fromhex(epoch)+struct.pack("!IQQ",i*20,arrival,i)
                sei=b"\x06"+escape(b"\x05\x30"+metadata+b"\x80")
                payload=b"\x78"+struct.pack("!H",len(sei))+sei+b"\x00\x02\x65\x88"
                packet=bytes([0x80,0xe0])+struct.pack("!HII",i,i*1800,1)+payload
                conn.sendall(b"$\x00"+struct.pack("!H",len(packet))+packet)
                # Avoid accumulating OS sleep overshoot; keep the intended 50 fps fixture.
                next_frame+=.02;time.sleep(max(0,next_frame-time.perf_counter()))
    except (BrokenPipeError,ConnectionResetError):pass
thread=threading.Thread(target=serve,daemon=True);thread.start()
try:
    result=m.measure(f"rtsp://127.0.0.1:{listener.getsockname()[1]}/fixture",f"http://127.0.0.1:{http.server_port}/clock",6.2,0,100)
    assert slow_seen.is_set(),"refresh stall was not exercised"
    # Known fixture offset avoids mistaking a wide HTTP interval for frame age.
    ages=[f["receiptMonotonicUs"]+offset-f["stamp"]["ingressMonoUs"] for f in result["samples"]]
    assert len(ages)>250 and result["transportInvalidFrames"]==0,result
    assert max(ages)<200000,("observer held packets",max(ages),len(ages))
    print(json.dumps({"frames":len(ages),"maximumTrueReceiptAgeMs":max(ages)/1000,"clockRefreshStallMs":1000}))
finally:
    stop.set();listener.close();http.shutdown();thread.join(1)
`, script], { encoding: "utf8", timeout: 15000 });
  assert.equal(result.status, 0, result.stderr || result.error?.message);
  t.diagnostic(result.stdout.trim());
});
