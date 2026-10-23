import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync,readFileSync,writeFileSync,rmSync,statSync,readdirSync,realpathSync,symlinkSync,mkdirSync,chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn } from "node:child_process";
import { loadBridgeIdentity } from "../src/bridge-identity.mjs";
function fixture(t) {const dir=realpathSync(mkdtempSync(join(tmpdir(),"wall-id-")));t.after(()=>rmSync(dir,{recursive:true,force:true}));return dir;}
test("bridge identity persists independently of credentials and remains private",t=>{
 const dir=fixture(t),id=loadBridgeIdentity(dir),path=join(dir,"bridge-id");
 assert.match(id,/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
 assert.equal(readFileSync(path,"utf8"),id+"\n");assert.equal(loadBridgeIdentity(dir),id);
 assert.equal(statSync(path).mode&0o777,0o600);assert.deepEqual(readdirSync(dir),["bridge-id"]);
 const other=join(dir,"other");assert.notEqual(loadBridgeIdentity(other),id);
});
test("concurrent processes exclusively publish one complete UUID without replacing it",async t=>{
 const dir=fixture(t),moduleUrl=new URL("../src/bridge-identity.mjs",import.meta.url).href;
 const run=()=>new Promise((resolve,reject)=>{
  const child=spawn(process.execPath,["--input-type=module","-e",`import {loadBridgeIdentity} from ${JSON.stringify(moduleUrl)};console.log(loadBridgeIdentity(process.argv[1]));`,dir]);
  let out="",err="";child.stdout.on("data",b=>out+=b);child.stderr.on("data",b=>err+=b);
  child.on("error",reject);child.on("close",code=>code===0?resolve(out.trim()):reject(new Error(err)));
 });
 const values=await Promise.all(Array.from({length:8},run));assert.equal(new Set(values).size,1);
 assert.equal(readFileSync(join(dir,"bridge-id"),"utf8"),values[0]+"\n");assert.deepEqual(readdirSync(dir),["bridge-id"]);
});
test("invalid existing UUID is refused without overwriting evidence",t=>{
 const dir=fixture(t),path=join(dir,"bridge-id");
 for(const value of ["invalid-private-content", "", "00000000-0000-0000-0000-000000000000", "x".repeat(200)]) {
  writeFileSync(path,value);assert.throws(()=>loadBridgeIdentity(dir),/Invalid bridge identity/);assert.equal(readFileSync(path,"utf8"),value);
 }
});
test("symlink files, broken links and symlink directories are refused",t=>{
 const dir=fixture(t),path=join(dir,"bridge-id"),target=join(dir,"private-id");
 writeFileSync(target,"do-not-change");symlinkSync(target,path);assert.throws(()=>loadBridgeIdentity(dir),/symlink/);assert.equal(readFileSync(target,"utf8"),"do-not-change");
 rmSync(path);symlinkSync(join(dir,"missing"),path);assert.throws(()=>loadBridgeIdentity(dir),/symlink/);rmSync(path);
 const alias=join(dir,"alias"),actual=join(dir,"actual");mkdirSync(actual);symlinkSync(actual,alias,"dir");assert.throws(()=>loadBridgeIdentity(alias),/symlink/);assert.deepEqual(readdirSync(actual),[]);
});
test("nonregular identity path fails and existing permissions are tightened",t=>{
 const dir=fixture(t),path=join(dir,"bridge-id");mkdirSync(path);assert.throws(()=>loadBridgeIdentity(dir),/Invalid bridge identity file/);rmSync(path,{recursive:true});
 const id=loadBridgeIdentity(dir);chmodSync(path,0o644);assert.equal(loadBridgeIdentity(dir),id);assert.equal(statSync(path).mode&0o777,0o600);
});
