import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdtempSync,rmSync,statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SettingsStore } from "../src/settings-store.mjs";
import { createSettingsHttp,loadAdminToken } from "../src/settings-http.mjs";
async function fixture(t) {
  const dir=mkdtempSync(join(tmpdir(),"wall-http-"));
  const token=loadAdminToken(dir);
  const store=new SettingsStore({dataDir:dir,legacyRaw:{defaults:{hold_seconds:60},eufy:{password:"secret-login"}},logger:null});
  const ctx={settingsAdminToken:token,settingsStore:store,settings:{snapshot:()=>store.load(),patch:input=>store.patch(input),import:input=>store.import(input)}};
  const handler=createSettingsHttp(ctx);
  const server=createServer((req,res)=>void handler(req,res,new URL(req.url,"http://localhost"),(res,status,body)=>{res.writeHead(status,{"content-type":"application/json"});res.end(JSON.stringify(body));}));
  await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));
  t.after(async()=>{await new Promise(resolve=>server.close(resolve));rmSync(dir,{recursive:true,force:true});});
  const request=(path="/api/settings",options={})=>fetch(`http://127.0.0.1:${server.address().port}${path}`,{...options,headers:{authorization:`Bearer ${token}`,...options.headers}});
  return {dir,token,ctx,store,request};
}
test("admin token persists privately and settings reads/writes require it",async t=>{
  const {dir,token,store,request}=await fixture(t);
  assert.equal(loadAdminToken(dir),token);assert.equal(statSync(join(dir,"settings-admin-token")).mode&0o777,0o600);
  for(const path of ["/api/settings","/api/settings/export","/api/settings/import"]) {
    const response=await request(path,{headers:{authorization:"Bearer wrong"}});
    assert.equal(response.status,401);assert.equal(response.headers.get("cache-control"),"no-store");await response.text();
  }
  const response=await request();assert.equal(response.status,200);assert.equal((await response.json()).revision,1);
  assert.equal(store.load().revision,1);
});
test("invalid JSON and oversized bodies do not mutate settings",async t=>{
  const {request,store}=await fixture(t);
  for(const [body,status] of [["{bad",400],["null",400],[JSON.stringify({pad:"x".repeat(1024*1024)}),413]]) {
    const response=await request("/api/settings",{method:"PATCH",body});assert.equal(response.status,status);await response.text();
  }
  assert.equal(store.load().revision,1);
});
test("HTTP revision conflict preserves winner and authenticated backup round trips",async t=>{
  const {request,store}=await fixture(t);
  let response=await request("/api/settings/export");const backup=await response.json();
  assert.doesNotMatch(JSON.stringify(backup),/secret-login/);
  assert.match(response.headers.get("content-disposition"),/attachment/);
  response=await request("/api/settings",{method:"PATCH",body:JSON.stringify({revision:1,settings:{defaults:{hold_seconds:90}}})});assert.equal(response.status,200);await response.json();
  response=await request("/api/settings",{method:"PATCH",body:JSON.stringify({revision:1,settings:{defaults:{hold_seconds:120}}})});assert.equal(response.status,409);await response.text();
  response=await request("/api/settings/import",{method:"POST",body:JSON.stringify({revision:2,backup})});assert.equal(response.status,200);await response.json();
  assert.equal(store.load().settings.defaults.hold_seconds,60);assert.equal(store.load().revision,3);
});
test("restart rejects stale requests or unsupervised processes",async t=>{
  const {request}=await fixture(t);
  for(const revision of [0,1]) { const response=await request("/api/settings/restart",{method:"POST",body:JSON.stringify({revision})});assert.equal(response.status,409);await response.text(); }
});
