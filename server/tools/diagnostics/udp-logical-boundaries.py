"""Classify captured UDP quiet boundaries without storing/reconstructing payloads.

Only explicit logical headers establish a unit. Missing packets, unobserved
interior headers, and future-arriving reorder dependencies remain unknown.
"""
import collections,json,pathlib,struct,sys

def classify(records):
    grouped=collections.defaultdict(list)
    for r in records:
        if not r['out'] and r['message']==208 and r['type']==1:
            grouped[r['port']].append(r)
    output=[]
    for port,arrivals in sorted(grouped.items()):
        origin=arrivals[0]['seq'];unique={};duplicates=0
        for r in arrivals:
            ordinal=(r['seq']-origin)&65535
            if ordinal>=32768:continue # earlier-than-window retransmission
            if ordinal in unique:duplicates+=1
            else:unique[ordinal]=r
        state=None;lastOrdinal=None;states={};missing=0;interior=0
        for ordinal,r in sorted(unique.items()):
            if lastOrdinal is not None and ordinal!=lastOrdinal+1:
                state=None;missing+=1
            lastOrdinal=ordinal;body=r['bytes']-8
            if r['header']:
                remaining=r['declared']-(body-16)
                state=dict(command=r['command'],channel=r['channel'],declared=r['declared'],remaining=remaining,
                           headerSeq=r['seq'],maxArrivalNs=r['wall'],boundaryReason='explicit-header')
            elif state is not None:
                state={**state,'remaining':state['remaining']-body,'maxArrivalNs':max(state['maxArrivalNs'],r['wall'])}
            if state is not None and state['remaining']<0:
                # More bytes than this unit needs may contain another unseen header.
                state=None;interior+=1
            states[r['seq']]=dict(state) if state else None
        gaps=[]
        for before,after in zip(arrivals,arrivals[1:]):
            gap=after['wall']-before['wall']
            if gap<=500_000_000:continue
            state=states.get(before['seq'])
            if not state:label='unknown'
            elif state['maxArrivalNs']>before['wall']:label='unknown-future-reorder-dependency'
            elif state['remaining']>0:label='within-video-unit' if state['command']==1300 else 'within-other-unit'
            else:label='after-complete-video-unit' if state['command']==1300 else 'after-complete-other-unit'
            gaps.append(dict(gapMs=gap/1e6,startWallNs=before['wall'],endWallNs=after['wall'],
                             beforeSeq=before['seq'],afterSeq=after['seq'],sequenceAdvance=(after['seq']-before['seq'])&65535,
                             classification=label,previousUnit=state,nextExplicitCommand=after['command'] if after['header'] else None))
        output.append(dict(localPort=port,records=len(arrivals),duplicates=duplicates,missingSequenceIntervals=missing,
                           unobservedInteriorHeaderIntervals=interior,gaps=gaps))
    return output

def selftest():
    def r(seq,wall,body=10,command=1300,declared=0,header=False):
        return dict(seq=seq,wall=wall,bytes=body+8,port=100,out=False,message=208,type=1,command=command,channel=2,declared=declared,header=header)
    # First header contains 10/30 body bytes; pause interrupts 20 remaining.
    a=r(1,0,26,declared=30,header=True);b=r(2,600_000_001,20)
    assert classify([a,b])[0]['gaps'][0]['classification']=='within-video-unit'
    # A duplicated datagram cannot consume the remaining bytes twice.
    c=r(3,1_200_000_002,26,declared=10,header=True)
    assert classify([a,b,{**b,'wall':601_000_001},c])[0]['gaps'][1]['classification']=='after-complete-video-unit'
    # Missing data and unseen interior headers cannot establish a completed unit.
    assert classify([a,r(3,1,1),r(4,700_000_000,1)])[0]['gaps'][0]['classification']=='unknown'
    assert classify([r(1,0,26,declared=9,header=True),b])[0]['gaps'][0]['classification']=='unknown'
    # An early-arriving tail depends on a middle fragment arriving after the boundary.
    x=r(1,0,26,declared=30,header=True);tail=r(3,1,10);middle=r(2,700_000_001,10)
    assert classify([x,tail,middle])[0]['gaps'][0]['classification']=='unknown-future-reorder-dependency'
    print('offline declared-size/retransmission/missing/interior/reorder boundary tests passed')

if __name__=='__main__':
    if sys.argv[1:]==['--selftest']:selftest();raise SystemExit(0)
    if len(sys.argv)!=2:raise SystemExit('usage: CAPTURE_DIRECTORY; or --selftest')
    base=pathlib.Path(sys.argv[1]);stats=json.loads((base/'capture-stats.json').read_text());width=stats['recordBytes']
    raw=(base/'capture.bin').read_bytes();assert width>=40 and len(raw)%width==0
    records=[]
    for at in range(0,len(raw),width):
        r=raw[at:at+width];records.append(dict(wall=struct.unpack_from('<Q',r)[0],port=struct.unpack_from('<H',r,16)[0],
            bytes=struct.unpack_from('<H',r,20)[0],seq=struct.unpack_from('<H',r,22)[0],command=struct.unpack_from('<H',r,24)[0],
            out=bool(r[26]),message=r[27],type=r[28],channel=r[29],header=bool(r[31]),declared=struct.unpack_from('<I',r,32)[0]))
    output=classify(records);(base/'logical-boundaries.json').write_text(json.dumps(output,indent=2));print(json.dumps(output,indent=2))
