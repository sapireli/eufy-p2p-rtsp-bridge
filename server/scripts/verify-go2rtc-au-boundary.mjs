#!/usr/bin/env node
// Isolated, finite fixture observer: one coded AU, source HTTP stays OPEN.
import {createServer} from 'node:http';
import {connect} from 'node:net';
import {spawn} from 'node:child_process';
import {readFileSync,writeFileSync,mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createHash} from 'node:crypto';
import {newVideoFlvMuxer} from '../src/video-flv.mjs';
import {newVideoMpegTsMuxer} from '../src/video-mpegts.mjs';
import {annexBNals} from '../src/video-matroska.mjs';
import {stampVideoFrame,parseFrameStamp} from '../src/frame-stamp.mjs';
const args=process.argv.slice(2),opt=(n,d)=>args.includes(n)?args[args.indexOf(n)+1]:d;
if(args.includes('--help')||!args.length){console.log('Usage: node verify-go2rtc-au-boundary.mjs --go2rtc PATH --fixtures DIR [--http-port 29930] [--rtsp-port 29554] [--api-port 29084]\nDIR: small.h264, garage-real-large.h264, small.h265, large.h265. All must be exactly one real coded access unit. No production streams, encoder, EOF, future AU or empty PES is used.');process.exit(0);}
const httpPort=Number(opt('--http-port','29930')),rtspPort=Number(opt('--rtsp-port','29554')),apiPort=Number(opt('--api-port','29084'));
for(const p of [httpPort,rtspPort,apiPort])if(!Number.isInteger(p)||p<1024||p>65535)throw Error('Invalid isolated port');
const cases=[];
function type(n,codec){return codec==='h264'?n[0]&31:(n[0]>>1)&63;}
function pictureHash(data,codec){const h=createHash('sha256');let count=0;for(const n of annexBNals(data)){const t=type(n,codec);if(codec==='h264'?t>=1&&t<=5:t<=31){const len=Buffer.alloc(4);len.writeUInt32BE(n.length);h.update(len);h.update(n);count++;}}return {hash:h.digest('hex'),nals:count};}
for(const [name,codec,file]of [['small264','h264','small.h264'],['large264','h264','garage-real-large.h264'],['small265','h265','small.h265'],['large265','h265','large.h265']]){
 const original=readFileSync(join(opt('--fixtures'),file));
 // Archived live AUs can carry their original bridge stamp. Remove ONLY its
 // custom SEI NAL before stamping the isolated fixture; picture bytes stay exact.
 const raw=Buffer.concat(annexBNals(original).filter(n=>!parseFrameStamp(Buffer.concat([Buffer.from([0,0,0,1]),n]),codec)).flatMap(n=>[Buffer.from([0,0,0,1]),n]));
 const record={serverEpoch:Buffer.from('0102030405060708','hex'),sourceTimestamp:123456789,ingressMonoUs:987654321n,frameId:BigInt(cases.length+1),maxAgeMs:500};
 const data=stampVideoFrame(raw,codec,record),frame={codec,data,sourceTimestamp:123456789,keyframe:true};
 for(const format of ['mpegts','flv']){
  let prefix,unit;
  if(format==='flv'){
   const b=newVideoFlvMuxer({codec}).push(frame);let at=13,last;
   while(at<b.length){if(at+11>b.length)throw Error('Bad FLV fixture tag');const size=b.readUIntBE(at+1,3),end=at+11+size+4;if(end>b.length)throw Error('Truncated FLV fixture');last=at;at=end;}
   prefix=b.subarray(0,last);unit=b.subarray(last);
  }else{
   const params=annexBNals(raw).filter(n=>codec==='h264'?[7,8].includes(type(n,codec)):[32,33,34].includes(type(n,codec)));
   const mux=newVideoMpegTsMuxer({codec});prefix=mux.push({...frame,data:Buffer.concat(params.flatMap(n=>[Buffer.from([0,0,0,1]),n]))});unit=mux.push(frame);
  }
  cases.push({name:name+'_'+format,codec,format,rawBytes:raw.length,record,prefix,unit,expected:pictureHash(raw,codec),responses:[],emissions:0});
 }
}
const sources=new Map(cases.map(c=>[c.name,c]));
const http=createServer((req,res)=>{
 const [path,query]=req.url.slice(1).split('?'),c=sources.get(path);
 if(!c){res.writeHead(404);return res.end();}
 if(query==='release'){c.release?.();res.end('ok');return;}
 res.writeHead(200,{'Content-Type':c.format==='flv'?'video/x-flv':'video/mp2t'});res.flushHeaders();res.write(c.prefix);c.responses.push(res);
 let sent=false;c.release=()=>{if(sent)return;sent=true;clearTimeout(c.fallback);c.emittedAt=performance.now();c.emissions++;res.write(c.unit);};
 // TS may need the large PES start to expose its preceding codec configuration.
 // The timer emits the SAME sole coded AU, never a second frame or terminator.
 c.fallback=setTimeout(c.release,600);res.on('close',()=>clearTimeout(c.fallback));
});
await new Promise((r,j)=>{http.once('error',j);http.listen(httpPort,'127.0.0.1',r);});
const dir=mkdtempSync(join(tmpdir(),'ewb-au-boundary-')),cfg=join(dir,'go2rtc.yaml');
writeFileSync(cfg,`api:\n  listen: 127.0.0.1:${apiPort}\nrtsp:\n  listen: 127.0.0.1:${rtspPort}\nwebrtc:\n  listen: ''\nstreams:\n`+cases.map(c=>`  ${c.name}: http://127.0.0.1:${httpPort}/${c.name}\n`).join(''));
const child=spawn(opt('--go2rtc'),['-config',cfg],{stdio:['ignore','pipe','pipe']});let logs='';for(const s of [child.stdout,child.stderr])s.on('data',b=>{logs=(logs+b).slice(-32768);});
let ready=false;
for(let attempt=0;attempt<100&&!ready;attempt++){
 if(child.exitCode!==null)throw Error('Isolated go2rtc exited before ready: '+logs);
 ready=await new Promise(r=>{const probe=connect(rtspPort,'127.0.0.1');probe.once('connect',()=>{probe.destroy();r(true);});probe.once('error',()=>r(false));});
 if(!ready)await new Promise(r=>setTimeout(r,100));
}
if(!ready){child.kill('SIGTERM');http.close();throw Error('Isolated go2rtc readiness timeout: '+logs);}
function depayload(codec,body,state){
 const t=type(body,codec);const nals=[];
 if(codec==='h264'){
  if(t===24){for(let at=1;at<body.length;){if(at+2>body.length)throw Error('Truncated STAP');const len=body.readUInt16BE(at);at+=2;if(!len||at+len>body.length)throw Error('Bad STAP');nals.push(body.subarray(at,at+len));at+=len;}}
  else if(t===28){const fu=body[1],head=Buffer.from([(body[0]&0xe0)|(fu&31)]);if(fu&128)state.fu=[head];if(!state.fu)throw Error('Orphan FU');state.fu.push(body.subarray(2));if(fu&64){nals.push(Buffer.concat(state.fu));state.fu=null;}}
  else nals.push(body);
 }else{
  if(t===48){for(let at=2;at<body.length;){const len=body.readUInt16BE(at);at+=2;if(!len||at+len>body.length)throw Error('Bad AP');nals.push(body.subarray(at,at+len));at+=len;}}
  else if(t===49){const fu=body[2];if(fu&128)state.fu=[Buffer.from([(body[0]&0x81)|((fu&63)<<1),body[1]])];if(!state.fu)throw Error('Orphan HEVC FU');state.fu.push(body.subarray(3));if(fu&64){nals.push(Buffer.concat(state.fu));state.fu=null;}}
  else nals.push(body);
 }
 return nals;
}
async function observe(c){
 const sock=connect(rtspPort,'127.0.0.1');let buffer=Buffer.alloc(0),closed=false,error,seq=0,session;let changed;
 sock.on('data',b=>{buffer=Buffer.concat([buffer,b]);if(buffer.length>8*1024*1024){error=Error('RTSP receive bound');sock.destroy();}changed?.();});sock.on('error',e=>{error=e;changed?.();});sock.on('close',()=>{closed=true;changed?.();});
 const wait=()=>new Promise(r=>{const timer=setTimeout(()=>{changed=null;r();},20);changed=()=>{clearTimeout(timer);changed=null;r();};});
 const deadline=performance.now()+7000;
 async function request(method,target,headers=''){
  sock.write(`${method} ${target} RTSP/1.0\r\nCSeq: ${++seq}\r\n${headers}${session?'Session: '+session+'\r\n':''}\r\n`);
  while(performance.now()<deadline){if(error)throw error;if(closed)throw Error('RTSP closed');const at=buffer.indexOf('\r\n\r\n');if(at>=0){const lines=buffer.subarray(0,at).toString().split('\r\n');const fields=Object.fromEntries(lines.slice(1).map(l=>{const x=l.indexOf(':');return[l.slice(0,x).toLowerCase(),l.slice(x+1).trim()];}));const size=Number(fields['content-length']??0);if(buffer.length>=at+4+size){const body=buffer.subarray(at+4,at+4+size);buffer=buffer.subarray(at+4+size);if(!lines[0].startsWith('RTSP/1.0 200 '))throw Error(lines[0]);if(fields.session)session=fields.session.split(';')[0];return{body,fields};}}await wait();}throw Error('RTSP setup timeout');
 }
 let complete,packets=0,configurationMarkers=0,setup=false;
 try{
  const url=`rtsp://127.0.0.1:${rtspPort}/${c.name}`,sdp=await request('DESCRIBE',url,'Accept: application/sdp\r\n');let video=false,control;
  for(const line of sdp.body.toString().split(/\r?\n/)){if(line.startsWith('m='))video=line.startsWith('m=video ');if(video&&line.startsWith('a=control:'))control=line.slice(10);}
  if(!control)throw Error('Missing video track');const track=control.startsWith('rtsp:')?control:(sdp.fields['content-base']??url+'/').replace(/\/$/,'')+'/'+control;
  await request('SETUP',track,'Transport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n');await request('PLAY',url);setup=true;await fetch(`http://127.0.0.1:${httpPort}/${c.name}?release`);
  const state={nals:[],fu:null,ts:null,lastSeq:null};
  while(performance.now()<deadline&&!complete){
   while(buffer.length>=4&&buffer[0]===36){const channel=buffer[1],size=buffer.readUInt16BE(2);if(buffer.length<4+size)break;const packet=buffer.subarray(4,4+size);buffer=buffer.subarray(4+size);if(channel!==0)continue;packets++;
    if(packet.length<12)throw Error('Short RTP');const nseq=packet.readUInt16BE(2),ts=packet.readUInt32BE(4);if(state.lastSeq!==null&&((nseq-state.lastSeq)&65535)!==1)throw Error('RTP loss');if(state.ts!==null&&state.ts!==ts)throw Error('Unexpected second timestamp');state.lastSeq=nseq;state.ts=ts;
    let at=12+(packet[0]&15)*4;if(packet[0]&16)at+=4+packet.readUInt16BE(at+2)*4;let end=packet.length;if(packet[0]&32)end-=packet.at(-1);state.nals.push(...depayload(c.codec,packet.subarray(at,end),state));
    if(packet[1]&128){if(state.fu)throw Error('Incomplete FU at marker');const data=Buffer.concat(state.nals.flatMap(n=>[Buffer.from([0,0,0,1]),n]));if(!pictureHash(data,c.codec).nals){configurationMarkers++;state.nals=[];state.ts=null;continue;}complete={data,receivedAt:performance.now(),pts:ts};break;}
   }
   if(!complete)await wait();
  }
  const picture=complete?pictureHash(complete.data,c.codec):null,stamp=complete?parseFrameStamp(complete.data,c.codec):null;
  return{name:c.name,codec:c.codec,format:c.format,rawBytes:c.rawBytes,setup,emissions:c.emissions,sourceHTTPStillOpen:c.responses.length>0&&c.responses.every(r=>!r.writableEnded&&!r.destroyed&&!r.socket?.destroyed),completeAU:!!complete,rtpPackets:packets,configurationMarkers,emissionToMarkerMs:complete?complete.receivedAt-c.emittedAt:null,pictureIdentical:picture?.hash===c.expected.hash&&picture?.nals===c.expected.nals,expectedPicture:c.expected,actualPicture:picture,stampExact:!!stamp&&stamp.frameId===c.record.frameId&&stamp.ingressMonoUs===c.record.ingressMonoUs&&stamp.sourceTimestamp===c.record.sourceTimestamp&&stamp.serverEpoch.equals(c.record.serverEpoch),stamp};
 }catch(e){return{name:c.name,codec:c.codec,format:c.format,rawBytes:c.rawBytes,setup,emissions:c.emissions,sourceHTTPStillOpen:c.responses.length>0&&c.responses.every(r=>!r.writableEnded&&!r.destroyed&&!r.socket?.destroyed),completeAU:false,error:e.message};}
 finally{sock.destroy();for(const res of c.responses)res.destroy();}
}
try{for(const c of cases)console.log(JSON.stringify(await observe(c),(_,v)=>typeof v==='bigint'?v.toString():v));}
finally{child.kill('SIGTERM');for(const c of cases){clearTimeout(c.fallback);for(const r of c.responses)r.destroy();}http.closeAllConnections();http.close();rmSync(dir,{recursive:true,force:true});if(child.exitCode!==null&&child.exitCode!==0)console.error(logs);}
