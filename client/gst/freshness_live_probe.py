#!/usr/bin/env python3
"""Headless strict-freshness V4L2 validation; never touches a DRM plane."""
import argparse,json,os,pathlib,struct,subprocess,tempfile,threading,time,urllib.request
p=argparse.ArgumentParser();p.add_argument("stream");p.add_argument("--clock-url",default="http://homebridge.lan:3000/api/clock");p.add_argument("--seconds",type=int,default=30);p.add_argument("--settle",type=int,default=5);p.add_argument("--min-render-ratio",type=float,default=.9);p.add_argument("--plugin-dir",default="/tmp/eufy-freshness-plugin");p.add_argument("--probe",default="/tmp/eufy-pacing-probe-next");p.add_argument("--log",default="/tmp/eufy-freshness-live.log");a=p.parse_args()
samples=[]
stop=threading.Event();clock=tempfile.NamedTemporaryFile(prefix="eufy-live-clock-",delete=False);clock.close();path=pathlib.Path(clock.name)
def sample():
 t0=time.monotonic_ns()
 with urllib.request.urlopen(a.clock_url,timeout=1) as response:v=json.load(response)
 t1=time.monotonic_ns();epoch=bytes.fromhex(v["serverEpoch"])
 if len(epoch)!=8 or v["scope"]!="bridge-ingress" or v["captureClockValidated"] or t1-t0>1_000_000_000:raise ValueError("bad clock sample")
 global samples
 record=(epoch,int(v["monotonicUs"]),t0,t1,t1+5_000_000_000)
 samples=[old for old in samples if old[0]==epoch and old[4]>t1]+[record];samples=samples[-16:]
 reserved=[old for old in samples if old[4]-t1>=2_100_000_000]
 selected=min(reserved or samples,key=lambda old:old[3]-old[2]+2*((max(0,t1-old[3])+999)//1000))
 epoch,server,t0,t1,lease=selected
 data=struct.pack(">8s8sQQQQ",b"EWCLK001",epoch,server,t0,t1,lease)
 tmp=path.with_suffix(".new");tmp.write_bytes(data);os.replace(tmp,path)
def poll():
 while not stop.wait(1):
  try:sample()
  except Exception as err:print("clock sample failure:",err,flush=True)
try:
 sample();thread=threading.Thread(target=poll,daemon=True);thread.start()
 pipeline=f"rtspsrc location={a.stream} latency=0 protocols=tcp buffer-mode=none ! rtph264depay ! h264parse ! video/x-h264,stream-format=byte-stream,alignment=au ! identity name=encoded ! eufystamp channel=1 ! v4l2h264dec name=decoder ! queue max-size-buffers=1 max-size-bytes=0 max-size-time=0 leaky=downstream ! eufypacing name=pacer channel=1 clock-file={path} max-age-ms=500 ! fakesink name=output sync=false processing-deadline=0 signal-handoffs=true"
 env=dict(os.environ,GST_PLUGIN_PATH=a.plugin_dir,EUFY_PROBE_SECONDS=str(a.seconds),EUFY_PROBE_KMS_ALLOCATION="1",GST_DEBUG="eufypacing:4")
 with open(a.log,"w") as log:r=subprocess.run([a.probe,pipeline],env=env,stdout=log,stderr=subprocess.STDOUT,timeout=a.seconds+15)
 counts={key:0 for key in ["encoded","decoded","pacer-arrival","render"]};steady={key:0 for key in counts};first=None
 for line in pathlib.Path(a.log).read_text(errors="replace").splitlines():
  fields=line.split(",");key=fields[0]
  if key in counts and len(fields)==3:
   stamp=int(fields[1]);first=stamp if first is None else min(first,stamp);counts[key]+=1
   if stamp-first>=a.settle*1_000_000_000:steady[key]+=1
 ratio=steady["render"]/max(1,steady["pacer-arrival"])
 print(json.dumps({"status":r.returncode,"counts":counts,"steadyCounts":steady,"steadyRenderRatio":ratio,"continuousPlaybackGate":ratio>=a.min_render_ratio,"log":a.log,"hardwareDecoder":"v4l2h264dec","scope":"bridge-ingress","captureClockValidated":False}))
 if r.returncode or steady["render"]<10 or ratio<a.min_render_ratio:raise SystemExit(1)

finally:
 stop.set();path.unlink(missing_ok=True)
