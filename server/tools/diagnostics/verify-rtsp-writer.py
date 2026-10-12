"""Bounded H.264 writer diagnostic. Persists metadata/VCL digests, never pixels.
Usage: URL {main|healthy|paused} CLOCK_URL. Paused imposes test-only backpressure.
Run only against an isolated sidecar for the paused role.
"""
import importlib.util,json,socket,time,sys,pathlib,hashlib,struct
root=pathlib.Path(__file__).resolve().parents[3]
spec=importlib.util.spec_from_file_location('fresh',root/'server/scripts/measure-frame-freshness.py')
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class H264Only:
 def __init__(self): raise ValueError('Writer digest observer requires H264')
m.H265Frames=H264Only
selftest=sys.argv[1:]==['--selftest']
if selftest:url,role,clock_url='rtsp://127.0.0.1/test','main','http://127.0.0.1/api/clock'
elif len(sys.argv)==4:url,role,clock_url=sys.argv[1:4]
else:raise SystemExit('usage: URL {main|healthy|paused} CLOCK_URL; or --selftest')
if role not in ('main','healthy','paused'): raise SystemExit('role must be main, healthy, or paused')
from urllib.parse import urlsplit
paused_port=urlsplit(url).port or 554
events=[];phase=[];original=socket.create_connection
original_push=m.H264Frames.push
original_stamp=m.parse_stamp
hashes=[]
def stamp_reader(nal,*args):
 if 1 <= (nal[0]&31) <= 5: hashes.append(hashlib.sha256(nal).digest())
 return original_stamp(nal,*args)
m.parse_stamp=stamp_reader
def push(self,packet):
 hashes.clear();frame=original_push(self,packet)
 if frame:
  stamp=frame.get('stamp');event=dict(wallNs=time.time_ns(),receiptMonoNs=time.monotonic_ns(),rtpTimestamp=frame['rtpTimestamp'],transportValid=frame['transportValid'],vclDigest=hashlib.sha256(b''.join(hashes)).hexdigest())
  if stamp:event.update(epoch=stamp['serverEpoch'],frameId=stamp['frameId'],ingressMonoUs=stamp['ingressMonoUs'])
  events.append(event)
 return frame
m.H264Frames.push=push
if selftest:
 assembler=m.H264Frames()
 for seq,nal in enumerate((b'\x61\x01\x02\x03',b'\x65\x04\x05\x06'),1):
  packet=struct.pack('!BBHII',0x80,0xe0,seq,seq*6000,1)+nal
  output=assembler.push(packet)
  assert output['transportValid'] and events[-1]['rtpTimestamp']==seq*6000
  assert events[-1]['vclDigest']==hashlib.sha256(hashlib.sha256(nal).digest()).hexdigest()
 assert len(events)==2 and events[0]['vclDigest']!=events[1]['vclDigest']
 print('offline complete-AU VCL digest/RTP identity recording passed')
 raise SystemExit(0)
class Reader:
 def __init__(self,sock):
  self.sock=sock;self.born=time.monotonic();self.paused=False;sock.setsockopt(socket.SOL_SOCKET,socket.SO_RCVBUF,16384)
  phase.append(dict(phase='connect',wallNs=time.time_ns(),effectiveReceiveBuffer=sock.getsockopt(socket.SOL_SOCKET,socket.SO_RCVBUF)))
 def __getattr__(self,name):return getattr(self.sock,name)
 def __enter__(self):return self
 def __exit__(self,*a):return self.sock.__exit__(*a)
 def recv(self,*a):
  if not self.paused and time.monotonic()-self.born>=6:
   self.paused=True;phase.append(dict(phase='pause',wallNs=time.time_ns(),seconds=16));time.sleep(16);phase.append(dict(phase='resume',wallNs=time.time_ns()))
  return self.sock.recv(*a)
def create(address,*a,**kw):
 s=original(address,*a,**kw)
 return Reader(s) if role=='paused' and address[1]==paused_port else s
socket.create_connection=create
result=dict(role=role,startNs=time.time_ns())
try:
 measured=m.measure(url,clock_url,40 if role!='paused' else 30,0,1000)
 result['measurement']={k:v for k,v in measured.items() if k in ('frames','stableFrames','stampedFrames','transportInvalidFrames','abandonedAccessUnits','missingOrInvalidStampFrames','error')}
except Exception as e:
 result['failure']=dict(wallNs=time.time_ns(),errorType=type(e).__name__)
 if role=='paused':
  socket.create_connection=original
  try:
   before=len(events);measured=m.measure(url,clock_url,8,0,1000)
   result['redial']=dict(frames=len(events)-before,error=measured.get('error'))
  except Exception as e:result['redial']=dict(errorType=type(e).__name__)
finally:
 socket.create_connection=original;result.update(endNs=time.time_ns(),phases=phase,events=events);print(json.dumps(result))
