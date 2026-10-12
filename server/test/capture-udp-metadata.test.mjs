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
