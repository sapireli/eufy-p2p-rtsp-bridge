"""Extract command metadata only; never print or persist raw decompressed buffers."""
import base64,hashlib,json,pathlib,re,struct,sys,zlib
p=pathlib.Path(sys.argv[1]);lines=p.read_text().splitlines();out={"dumpSHA256":hashlib.sha256(p.read_bytes()).hexdigest(),"sections":[]}
for i,line in enumerate(lines):
 if " --- " not in line or i+1>=len(lines) or not lines[i+1].startswith(":"):continue
 packed=base64.a85decode(lines[i+1][1:]);packed=b"".join(packed[j:j+4][::-1] for j in range(0,len(packed),4));raw=zlib.decompress(packed)
 meta={"section":line,"bytes":len(raw)}
 if " --- batch = " in line:
  words=struct.unpack("<"+"I"*(len(raw)//4),raw);modes=[];chains=[]
  for j,v in enumerate(words):
   if v==0x73900011:
    d1,d2,d5=words[j+1],words[j+2],words[j+5];cb=1<<((d2&3)+3)
    meta.setdefault("pictureState",[]).append({"byteOffset":j*4,"dimensionWord":hex(d1),"codingBlockWord":hex(d2),"depthWord":hex(d5),"codedWidth":((d1&65535)+1)*cb,"codedHeight":((d1>>16)+1)*cb,"lumaBitDepth":((d5>>27)&7)+8,"chromaBitDepth":((d5>>24)&7)+8})
   if (v&0xffff0000) in (0x73800000,0x70000000):modes.append({"byteOffset":j*4,"command":hex(v),"modeWord":hex(words[j+1])})
   if (v&0xff800000)==0x18800000 and j+2<len(words):chains.append({"byteOffset":j*4,"address":hex((words[j+2]<<32)|(words[j+1]&~3))})
  meta.update(pipeModeCandidates=modes,batchStartCandidates=chains,wordAtReportedBBOffset0x848=hex(words[0x848//4]))
 out["sections"].append(meta)
print(json.dumps(out,indent=2))
