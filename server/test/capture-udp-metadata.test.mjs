import test from "node:test";
import assert from "node:assert/strict";
import {spawnSync} from "node:child_process";
import {fileURLToPath} from "node:url";
test("packet tap parses RX/TX metadata, ACK rollover, and rejects unrelated/malformed packets", () => {
  const script = fileURLToPath(new URL("../scripts/capture-udp-metadata.py", import.meta.url));
  const result = spawnSync("python3", ["-c", `
import importlib.util,struct,socket,sys
sp=importlib.util.spec_from_file_location('tap',sys.argv[1]);m=importlib.util.module_from_spec(sp);sp.loader.exec_module(m)
peer=socket.inet_aton('192.168.23.233');local=socket.inet_aton('192.168.23.158');peers={peer:'peer-1'}
def packet(body,out=False):
 ip=bytearray(20);ip[0]=0x45;ip[9]=17;struct.pack_into('!H',ip,2,28+len(body));ip[12:16]=local if out else peer;ip[16:20]=peer if out else local
 return bytes(ip)+struct.pack('!HHHH',36637 if out else 22557,22557 if out else 36637,8+len(body),0)+body
rx=packet(bytes.fromhex('f1d000000001ffff'))
assert m.parse_udp_metadata(rx,('eth0',0x0800,0),peers)['sequence']==65535
ack=packet(bytes.fromhex('f1d1000000010002ffff0000'),True)
r=m.parse_udp_metadata(ack,('eth0',0x0800,4),peers);assert r['direction']=='out' and r['ackSequences']==[65535,0] and r['localPort']==36637
assert not any(isinstance(x,bytes) for x in r.values())
assert m.parse_udp_metadata(rx,('eth0',0x0806,0),peers) is None
bad=bytearray(rx);bad[6]=0x20;assert m.parse_udp_metadata(bad,('eth0',0x0800,0),peers) is None
assert m.parse_udp_metadata(packet(bytes.fromhex('f1d1000000010002ffff'),True),('eth0',0x0800,4),peers) is None
bad=bytearray(rx);struct.pack_into('!H',bad,2,65535);assert m.parse_udp_metadata(bad,('eth0',0x0800,0),peers) is None
`,script], {encoding:"utf8"});
  assert.equal(result.status, 0, result.stderr);
});

test("optional command chronology retains fixed outbound header integers and no encrypted bytes", () => {
  const script = fileURLToPath(new URL("../scripts/capture-udp-metadata.py", import.meta.url));
  const result = spawnSync("python3", ["-c", `
import importlib.util,struct,socket,json,sys
sp=importlib.util.spec_from_file_location('tap',sys.argv[1]);m=importlib.util.module_from_spec(sp);sp.loader.exec_module(m)
peer=socket.inet_aton('192.0.2.1');local=socket.inet_aton('192.0.2.2');peers={peer:'peer-1'}
def packet(payload,out=True,datatype=0):
 body=b'\\xf1\\xd0\\x00\\x00'+bytes([0,datatype])+struct.pack('!H',65535)+payload
 ip=bytearray(20);ip[0]=0x45;ip[9]=17;struct.pack_into('!H',ip,2,28+len(body));ip[12:16]=local if out else peer;ip[16:20]=peer if out else local
 return bytes(ip)+struct.pack('!HHHH',10001,10002,8+len(body),0)+body
header=b'XZYH'+struct.pack('<HI',1350,4096)+bytes([8,0,2,8,11,0])
secret=b'ENCRYPTED_PRIVATE_BODY_'+bytes(range(256))*16
r=m.parse_udp_metadata(packet(header+secret),('eth0',0x0800,4),peers,True)
assert (r['commandId'],r['commandChannel'],r['commandSignCode'],r['commandType'])==(1350,2,8,11)
assert all(not isinstance(v,(bytes,bytearray,memoryview)) for v in r.values())
assert 'PRIVATE' not in json.dumps(r) and 'XZYH' not in json.dumps(r)
# Body contents cannot affect fixed-header fields; same-size encrypted bodies yield identical rows.
assert r==m.parse_udp_metadata(packet(header+b'Z'*len(secret)),('eth0',0x0800,4),peers,True)
# END envelope length is an integer; the actual/declared payload can be distinguished without bytes.
for body in [bytes.fromhex('f1f00000'), bytes.fromhex('f1f00001'), bytes.fromhex('f1f0000107')]:
 raw=bytearray(packet(b''));struct.pack_into('!H',raw,2,28+len(body));struct.pack_into('!H',raw,24,8+len(body));raw=bytes(raw[:28])+body
 end=m.parse_udp_metadata(raw,('eth0',0x0800,4),peers,True)
 assert end['declaredPayloadBytes']==int.from_bytes(body[2:4],'big')
 assert all(not isinstance(v,(bytes,bytearray,memoryview)) for v in end.values())
 assert 'declaredPayloadBytes' not in m.parse_udp_metadata(raw,('eth0',0x0800,4),peers)

for payload,out,kind,enabled in [(header+secret,True,0,False),(header+secret,False,0,True),(header+secret,True,1,True),(header[:15],True,0,True),(b'continuation'+header+secret,True,0,True)]:
 row=m.parse_udp_metadata(packet(payload,out,kind),('eth0',0x0800,4 if out else 0),peers,enabled)
 assert 'commandId' not in row
`, script], {encoding: "utf8"});
  assert.equal(result.status, 0, result.stderr);
});
