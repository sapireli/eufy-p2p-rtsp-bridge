import test from "node:test";
import assert from "node:assert/strict";
import {spawnSync} from "node:child_process";
import {fileURLToPath} from "node:url";

const script = fileURLToPath(new URL("../scripts/capture-udp-metadata.py", import.meta.url));
const setup = String.raw`
import importlib.util,struct,socket,json,sys
sp=importlib.util.spec_from_file_location('tap',sys.argv[1]);m=importlib.util.module_from_spec(sp);sp.loader.exec_module(m)
peer=socket.inet_aton('192.0.2.1');local=socket.inet_aton('192.0.2.2');peers={peer:'peer-1'}
def header(length,command=1300,channel=2):
 return b'XZYH'+struct.pack('<HI',command,length)+bytes([1,0,channel,1,0,0])
def packet(payload,sequence=0,kind=1,out=False,port=10001,message=0xd0):
 body=b'\xf1'+bytes([message])+struct.pack('!H',4+len(payload))+bytes([0xd1,kind])+struct.pack('!H',sequence)+payload if message==0xd0 else b'\xf1'+bytes([message])+struct.pack('!H',len(payload))+payload
 ip=bytearray(20);ip[0]=0x45;ip[9]=17;struct.pack_into('!H',ip,2,28+len(body));ip[12:16]=local if out else peer;ip[16:20]=peer if out else local
 return bytes(ip)+struct.pack('!HHHH',10002 if out else port,port if out else 10002,8+len(body),0)+body
def parse(tracker,payload,sequence=0,**kwargs):
 out=kwargs.get('out',False)
 return m.parse_udp_metadata(packet(payload,sequence,**kwargs),('eth0',0x0800,4 if out else 0),peers,logical_tracker=tracker,observed_ns=123)['incomingLogical']
def no_body(value):
 if isinstance(value,dict):
  for item in value.values(): no_body(item)
 elif isinstance(value,(list,tuple)): 
  for item in value: no_body(item)
 else: assert not isinstance(value,(bytes,bytearray,memoryview)),repr(value)
`;
function check(name, code) {
  test(name, () => {
    const result = spawnSync("python3", ["-c", setup + code, script], {encoding:"utf8"});
    assert.equal(result.status, 0, result.stderr);
  });
}

check("incoming unit accounting skips opaque body bytes even when a continuation mimics a complete header", String.raw`
a=m.IncomingLogicalTracker();b=m.IncomingLogicalTracker()
for tracker,opaque in [(a,b'PRIVATE_ENCRYPTED'),(b,b'Z'*17)]:
 # A full unit establishes only a tentative start; a calculated second header confirms the chain.
 first=parse(tracker,header(17)+opaque,65534)
 assert first['boundary']=='unknown' and first['events'][0]['confidence']=='tentative'
 second=parse(tracker,header(64)+b'A'*8,65535)
 assert second['boundary']=='unit' and second['remainingPayloadBytes']==56 and second['confidence']=='chain'
 assert second['commandId']==1300 and second['channel']==2
 # The apparent header below is known payload and MUST NOT cause a unit-start.
 continuation=parse(tracker,header(500)+b'B'*16,0)
 assert continuation['remainingPayloadBytes']==24 and 'events' not in continuation
 assert continuation['expectedSequence']==1
 finish=parse(tracker,b'C'*24,1)
 assert finish['boundary']=='header' and finish['events']==[{'event':'unit-complete','unitId':2}]
 no_body(first);no_body(second);no_body(continuation);no_body(finish)
assert a.streams==b.streams
# Ordinary output and transient pending metadata contain no opaque body bytes.
text=json.dumps([first,second,continuation,finish]);assert 'PRIVATE' not in text and 'XZYH' not in text
assert m.parse_udp_metadata(packet(header(4)+b'KEY!'),('eth0',0x0800,0),peers).get('incomingLogical') is None
`);

check("incoming header parsing handles calculated fragments and rejects malformed or unsupported boundary candidates", String.raw`
t=m.IncomingLogicalTracker()
parse(t,header(0),10)
next_header=header(8,1301)
a=parse(t,next_header[:3],11)
assert a['boundary']=='unknown' and len(next(iter(t.streams.values()))['header'])==3
b=parse(t,next_header[3:]+b'12345678',12)
assert b['boundary']=='header' and b['events'][0]['commandId']==1301
# A new stream starting with only a partial prefix is deliberately not guessed.
u=m.IncomingLogicalTracker();a=parse(u,header(0)[:8]);b=parse(u,header(0)[8:],1)
assert a['boundary']==b['boundary']=='unknown'
# Bad fixed fields, unrecognized command and oversized length never establish unit metadata.
for offset,value in [(0,0),(10,255),(11,1),(12,100),(13,255),(15,1)]:
 raw=bytearray(header(4));raw[offset]=value
 r=parse(m.IncomingLogicalTracker(),bytes(raw)+b'ABCD');assert 'unitId' not in r and r['boundary']=='unknown'
for raw in [header(4,65535),header(m.IncomingLogicalTracker.MAX_UNIT+1)]:
 assert 'unitId' not in parse(m.IncomingLogicalTracker(),raw)
# A partial header after a confirmed chain is distinct from an empty header boundary.
r=parse(t,header(4)[:4],13);assert r['boundary']=='partial-header' and r['partialHeaderBytes']==4
# A malformed calculated next header invalidates an established chain.
r=parse(t,b'WRONG_BOUNDARY!!!',14);assert r['boundary']=='unknown' and r['events']==[{'event':'invalid-header'}]
# Wire length/data-type errors are never classified as logical units.
for kind in [3,255]:
 assert parse(m.IncomingLogicalTracker(),header(0),kind=kind)['sequenceState']=='unsupported-or-malformed'
raw=bytearray(packet(header(0)));raw[30:32]=b'\x00\x00'
r=m.parse_udp_metadata(bytes(raw),('eth0',0x0800,0),peers,logical_tracker=m.IncomingLogicalTracker())
assert r['incomingLogical']['sequenceState']=='unsupported-or-malformed'
`);

check("metadata-only reordering preserves known continuations but refuses unseen boundaries, duplicates and overflow", String.raw`
t=m.IncomingLogicalTracker();parse(t,header(0),65533);parse(t,header(40)+b'A'*4,65534)
late=parse(t,b'BODY_REORDERED'*1+b'XXX',0)
assert late['boundary']=='unknown' and late['pendingPackets']==1
state=next(iter(t.streams.values()));no_body(state['pending'])
assert state['pending'][0]=={'payloadBytes':17,'observedNs':123}
old=parse(t,b'IGNORE',65534);assert old['sequenceState']=='duplicate-or-old'
r=parse(t,b'B'*4,65535)
assert r['boundary']=='unit' and r['remainingPayloadBytes']==15 and r['expectedSequence']==1
# An unseen header cannot be parsed when only its length was retained.
parse(t,header(0),2)
r=parse(t,b'C'*15,1)
assert r['boundary']=='unknown' and r['events'][-1]['event']=='reordered-boundary-unknown'
# A very large sequence jump resets instead of retaining an unbounded packet map.
r=parse(t,header(0),1000)
assert r['sequenceState']=='reorder-limit-reset' and r['pendingPackets']==0 and r['boundary']=='unknown'
# More endpoints evict old passive state; actual transport remains untouched.
for port in range(20000,20100): parse(t,header(0),port=port)
assert len(t.streams)==t.MAX_STREAMS
# Empty units are bounded even in pathological packed datagrams.
r=parse(m.IncomingLogicalTracker(),header(0)*130)
assert r['boundary']=='unknown' and r['events'][-1]['event']=='packed-unit-limit'
`);

check("well-framed endpoint lifecycle resets prevent reused sequence zero inheriting old unit state", String.raw`
t=m.IncomingLogicalTracker();parse(t,header(0),10);parse(t,header(100),11)
parse(t,header(0),10,port=10003)
assert len(t.streams)==2
# Malformed END cannot reset diagnostic state.
raw=bytearray(packet(b'',message=0xf0));raw[31]=1
m.parse_udp_metadata(bytes(raw),('eth0',0x0800,0),peers,logical_tracker=t)
assert len(t.streams)==2
m.parse_udp_metadata(packet(b'',message=0xf0),('eth0',0x0800,0),peers,logical_tracker=t)
assert len(t.streams)==1
r=parse(t,header(0),0);assert r['sequenceState']=='ordered' and r['boundary']=='unknown'
# Outgoing CAM_ID also resets only this endpoint, including an opaque valid body.
m.parse_udp_metadata(packet(b'PRIVATE!',out=True,message=0x42),('eth0',0x0800,4),peers,logical_tracker=t)
assert len(t.streams)==1
r=parse(t,header(0),0);assert r['sequenceState']=='ordered'
# Output from a valid unit contains only numeric metadata and diagnostic labels.
no_body(r)
`);
