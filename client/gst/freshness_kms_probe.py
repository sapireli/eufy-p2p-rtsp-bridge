#!/usr/bin/env python3
"""Bounded native expiry/recovery check; does not send CEC or change service enablement.
Requires the already-installed strict client and an active service. Stops only its Go
parent while GStreamer children keep decoding; always resumes the parent in finally.
"""
import argparse,json,os,pathlib,re,signal,subprocess,time
p=argparse.ArgumentParser();p.add_argument("--service",default="eufy-wall.service");p.add_argument("--planes",default="98,109");p.add_argument("--primary",type=int,default=86);p.add_argument("--log",default="/tmp/eufy-freshness-kms.json");a=p.parse_args();planes=[int(v) for v in a.planes.split(",")];records=[]
def snapshot(phase):
 text=pathlib.Path("/sys/kernel/debug/dri/0/state").read_text();values={}
 for section in re.split(r"(?=plane\[)",text):
  m=re.search(r"plane\[(\d+)\]:.*?\n\s+fb=(\d+)",section,re.S)
  if m:values[int(m[1])]=int(m[2])
 records.append({"phase":phase,"monotonic":time.monotonic(),"planes":values});return values
def advancing(phase,timeout):
 end=time.monotonic()+timeout;seen={v:set() for v in planes}
 while time.monotonic()<end:
  s=snapshot(phase)
  for plane in planes:
   if s.get(plane):seen[plane].add(s[plane])
  if all(len(v)>=3 for v in seen.values()):return s
  time.sleep(.1)
 raise RuntimeError("video planes did not advance: "+str(seen))
pid=int(subprocess.check_output(["systemctl","show",a.service,"--property=MainPID","--value"],text=True).strip());assert pid>1
paused=False
try:
 before=advancing("fresh",30);assert before.get(a.primary,0)>0;primary=before[a.primary]
 os.kill(pid,signal.SIGSTOP);paused=True;start=time.monotonic();expired=None
 while time.monotonic()-start<7:
  s=snapshot("clock_loss")
  if all(s.get(plane)==0 for plane in planes):expired=s;break
  time.sleep(.03)
 assert expired is not None,"video overlay remained visible beyond clock validity and age deadline"
 assert expired.get(a.primary)==primary,"owned background was removed with expired video"
 elapsed=time.monotonic()-start;os.kill(pid,signal.SIGCONT);paused=False;after=advancing("recovery",30)
 print(json.dumps({"status":"passed","expireAfterPauseSeconds":round(elapsed,3),"before":before,"expired":expired,"after":after,"captureClockValidated":False,"scope":"bridge-ingress","log":a.log}))
finally:
 if paused:
  try:os.kill(pid,signal.SIGCONT)
  except ProcessLookupError:pass
 pathlib.Path(a.log).write_text(json.dumps(records,indent=2))
