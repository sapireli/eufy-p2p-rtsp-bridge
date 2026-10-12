import collections,datetime,json,pathlib,struct,sys
base=pathlib.Path(sys.argv[1]) if len(sys.argv)==2 else None
if base is None: raise SystemExit("usage: analyze-udp-headers.py CAPTURE_DIRECTORY")
data=(base/'capture.bin').read_bytes()
assert len(data)%40==0
groups=collections.defaultdict(list)
headers=collections.Counter()
for at in range(0,len(data),40):
 r=data[at:at+40]
 wall,processed=struct.unpack_from('<QQ',r)
 local,peer,size,seq,cmd=struct.unpack_from('<HHHHH',r,16)
 direction,msg,kind,channel,sign,header=r[26:32]
 declared=struct.unpack_from('<I',r,32)[0]
 groups[local,direction,msg,kind].append((wall,seq,processed,size))
 if header:headers[local,channel,cmd,sign]+=1
result={'stats':json.loads((base/'capture-stats.json').read_text()),'groups':[],'logicalHeaders':[dict(localPort=k[0],channel=k[1],command=k[2],sign=k[3],count=v)for k,v in sorted(headers.items())]}
for key,rows in sorted(groups.items()):
 gaps=sorted(((b[0]-a[0])/1e6,a,b)for a,b in zip(rows,rows[1:]))
 large=[dict(gapMs=g,startWallNs=a[0],endWallNs=b[0],startSequence=a[1],endSequence=b[1],sequenceAdvance=(b[1]-a[1])&65535)for g,a,b in gaps if g>=500]
 result['groups'].append(dict(localPort=key[0],direction='out'if key[1]else'in',messageType=key[2],dataType=key[3],count=len(rows),firstWallNs=rows[0][0],lastWallNs=rows[-1][0],maxGapMs=gaps[-1][0]if gaps else 0,gapsOver500ms=large))
(base/'summary.json').write_text(json.dumps(result,indent=2))
print(json.dumps({"stats":result["stats"],"videoGroups":[g for g in result["groups"] if g["direction"]=="in" and g["messageType"]==208 and g["dataType"]==1],"logicalHeaders":result["logicalHeaders"]},indent=2))
