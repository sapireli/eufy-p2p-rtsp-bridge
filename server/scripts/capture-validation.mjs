#!/usr/bin/env node
// Explicit diagnostic observer. Does not enable an inspector or start camera streams.
import { createRequire } from "node:module";
import { chmodSync, mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { spawnSync } from "node:child_process";

const args = process.argv.slice(2);
if (args.includes("--help") || !args.length) {
  console.log(`Usage: node server/scripts/capture-validation.mjs --inspector http://127.0.0.1:PORT --out DIRECTORY [--seconds 30] [--per-stream 3] [--package /opt/eufy-wall-bridge/server/package.json] [--decode] [--ocr]

Use an already enabled loopback Node inspector, optionally forwarded over SSH.
Capture finite independently decodable SDK keyframes, raw source timestamps and
receipt times. --decode saves PNGs offline using local ffmpeg. --ocr also records
unvalidated tesseract text. It never claims that an OSD/source clock is capture time.
Output contains private camera pictures. The directory and files are owner-only.`);
  process.exit(0);
}
function option(name, fallback) {
  const at = args.indexOf(name);
  if (at < 0) return fallback;
  if (!args[at + 1] || args[at + 1].startsWith("--")) throw Error(`Missing ${name} value`);
  return args[at + 1];
}
const endpoint = new URL(option("--inspector"));
if (endpoint.protocol !== "http:" || !["127.0.0.1", "localhost", "[::1]"].includes(endpoint.hostname)) {
  throw Error("Inspector must be an explicitly supplied loopback HTTP endpoint");
}
const seconds = Number(option("--seconds", "30"));
const perStream = Number(option("--per-stream", "3"));
if (!Number.isInteger(seconds) || seconds < 1 || seconds > 120) throw Error("seconds must be 1..120");
if (!Number.isInteger(perStream) || perStream < 1 || perStream > 10) throw Error("per-stream must be 1..10");
const out = resolve(option("--out"));
const targetPackage = option("--package", "/opt/eufy-wall-bridge/server/package.json");
mkdirSync(out, { recursive: true, mode: 0o700 });
chmodSync(out, 0o700);
const require = createRequire(new URL("../package.json", import.meta.url));
const WebSocket = require("ws");
const list = await (await fetch(new URL("/json/list", endpoint), { signal: AbortSignal.timeout(5000) })).json();
if (list.length !== 1) throw Error("Expected exactly one inspector target");
const socketUrl = new URL(list[0].webSocketDebuggerUrl);
socketUrl.hostname = endpoint.hostname;
socketUrl.port = endpoint.port;
const ws = new WebSocket(socketUrl);
await new Promise((done, fail) => { ws.once("open", done); ws.once("error", fail); });
let nextId = 0;
const pending = new Map();
ws.on("message", (bytes) => {
  const response = JSON.parse(bytes);
  const call = pending.get(response.id);
  if (!call) return;
  pending.delete(response.id);
  clearTimeout(call.timer);
  if (response.error) call.fail(Error(response.error.message));
  else if (response.result?.exceptionDetails) call.fail(Error(response.result.exceptionDetails.text));
  else call.done(response.result?.result?.value);
});
ws.on("close", () => { for (const p of pending.values()) { clearTimeout(p.timer); p.fail(Error("Inspector disconnected")); } pending.clear(); });
function evaluate(expression, awaitPromise = false, timeoutMs = 5000) {
  return new Promise((done, fail) => {
    const id = ++nextId;
    const timer = setTimeout(() => { pending.delete(id); fail(Error("Inspector request timed out")); }, timeoutMs);
    pending.set(id, { done, fail, timer });
    ws.send(JSON.stringify({ id, method: "Runtime.evaluate", params: { expression, awaitPromise, returnByValue: true } }));
  });
}
const expression = `(async()=>{
  if(globalThis.__ewbCaptureValidationCleanup)throw Error('A capture observer is already running');
  const sdk=process.getBuiltinModule('module').createRequire(${JSON.stringify(targetPackage)})('@mega-yfue/eufy-sdk');
  const proto=sdk.LiveStream.prototype, own=Object.getOwnPropertyDescriptor(proto,'emit'), original=proto.emit;
  const streams=new Map(), rows=[], started=Date.now(), mono=process.hrtime.bigint();
  let rejected=0, observed=0, savedBytes=0;
  function types(data,codec){const result=new Set();for(let i=0;i<data.length-4;i++){if(data[i]===0&&data[i+1]===0&&(data[i+2]===1||(data[i+2]===0&&data[i+3]===1))){const at=i+(data[i+2]===1?3:4);result.add(codec==='h265'?(data[at]>>1)&63:data[at]&31);i=at;}}return result;}
  const wrapper=function(event,...args){
    if(event==='video'){
      const f=args[0];observed++;
      if(f?.keyframe&&Buffer.isBuffer(f.data)){
        const key=this.session?.traceId+':'+this.channel;
        if(!streams.has(key)&&streams.size<16)streams.set(key,{id:'stream-'+(streams.size+1),count:0});
        const stream=streams.get(key);
        if(stream&&stream.count<${perStream}&&rows.length<80){
          const ingressUnixMs=Date.now(), ingressMonotonicMs=Number(process.hrtime.bigint()-mono)/1e6;
          const n=types(f.data,f.codec);
          const independent=f.codec==='h264'?n.has(5)&&n.has(7)&&n.has(8):f.codec==='h265'&&[19,20,21].some(t=>n.has(t))&&[32,33,34].every(t=>n.has(t));
          if(independent&&f.data.length<=2097152&&savedBytes+f.data.length<=16777216){stream.count++;savedBytes+=f.data.length;rows.push({stream:stream.id,sourceTimestamp:f.sourceTimestamp,ingressUnixMs,ingressMonotonicMs,codec:f.codec,width:f.width,height:f.height,size:f.data.length,nalTypes:[...n],bytes:f.data.toString('base64')});}else rejected++;
        }
      }
    }
    return original.call(this,event,...args);
  };
  proto.emit=wrapper;
  let cleaned=false;
  function cleanup(){if(cleaned)return;cleaned=true;if(proto.emit===wrapper){if(own)Object.defineProperty(proto,'emit',own);else delete proto.emit;}if(globalThis.__ewbCaptureValidationCleanup===cleanup)delete globalThis.__ewbCaptureValidationCleanup;}
  globalThis.__ewbCaptureValidationCleanup=cleanup;
  const guard=setTimeout(cleanup,${(seconds + 10) * 1000});
  try{await new Promise(done=>setTimeout(done,${seconds * 1000}));return {started,ended:Date.now(),observed,rejected,rows};}finally{cleanup();clearTimeout(guard);}
})()`;
let capture;
try { capture = await evaluate(expression, true, (seconds + 15) * 1000); }
finally { await evaluate("globalThis.__ewbCaptureValidationCleanup?.();true").catch(() => {}); ws.close(); }
const manifest = { ...capture, scope: "SDK-emission receipt, not verified sensor capture", captureClockValidated: false };
manifest.rows = capture.rows.map((row, index) => {
  const { bytes, ...metadata } = row;
  const stem = `${row.stream}-${String(index + 1).padStart(3, "0")}`;
  const videoFile = `${stem}.${row.codec === "h265" ? "hevc" : "h264"}`;
  const pngFile = `${stem}.png`;
  writeFileSync(resolve(out, videoFile), Buffer.from(bytes, "base64"), { mode: 0o600 });
  const result = { ...metadata, videoFile };
  if (args.includes("--decode") || args.includes("--ocr")) {
    const decoded = spawnSync("ffmpeg", ["-hide_banner", "-v", "error", "-threads", "1", "-f", row.codec === "h265" ? "hevc" : "h264", "-i", resolve(out, videoFile), "-frames:v", "1", "-threads", "1", "-y", resolve(out, pngFile)], { encoding: "utf8", timeout: 30000 });
    result.decodeOk = decoded.status === 0;
    if (!result.decodeOk) result.decodeError = decoded.error?.message ?? decoded.stderr.trim().slice(0, 1000);
    else {
      chmodSync(resolve(out, pngFile), 0o600);
      result.pngFile = pngFile;
      if (args.includes("--ocr")) {
        const ocr = spawnSync("tesseract", [resolve(out, pngFile), "stdout"], { encoding: "utf8", timeout: 30000 });
        result.unvalidatedOcrText = ocr.status === 0 ? ocr.stdout.trim() : null;
        result.ocrOk = ocr.status === 0;
      }
    }
  }
  return result;
});
writeFileSync(resolve(out, "manifest.json"), JSON.stringify(manifest, null, 2) + "\n", { mode: 0o600 });
console.log(JSON.stringify({ out, frames: manifest.rows.length, observed: manifest.observed, rejected: manifest.rejected, captureClockValidated: false }));
