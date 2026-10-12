import test from "node:test";
import assert from "node:assert/strict";
import {mkdtempSync,writeFileSync,readFileSync,rmSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {fileURLToPath} from "node:url";
import {spawnSync} from "node:child_process";

test("stock is default and failed downloads preserve binary; patched install/update needs explicit opt-in",()=>{
 const dir=mkdtempSync(join(tmpdir(),"go2rtc-routing-"));
 const log=join(dir,"calls");const binary=join(dir,"go2rtc");
 const helper=fileURLToPath(new URL("../../deploy/install-go2rtc.sh",import.meta.url));
 const updater=fileURLToPath(new URL("../../deploy/update-debian.sh",import.meta.url));
 const {GO2RTC_WRITE_FIX,...cleanEnv}=process.env;
 const env={...cleanEnv,PATH:dir+":"+process.env.PATH,MOCK_LOG:log};
 const script=(name,code)=>writeFileSync(join(dir,name),"#!/bin/bash\n"+code,{mode:0o755});
 script("curl",String.raw`echo STOCK >> "$MOCK_LOG"
[[ "$MOCK_DOWNLOAD_FAIL" == 1 ]] && exit 8
while [[ $# -gt 0 ]]; do
 if [[ $1 == -o ]]; then shift; printf STOCK_BINARY > "$1"; exit 0; fi
 shift
done
exit 1
`);
 script("python3",String.raw`echo BUILDER >> "$MOCK_LOG"
while [[ $# -gt 0 ]]; do
 if [[ $1 == --install ]]; then shift; printf PATCHED_BINARY > "$1"; fi
 shift
done
`);
 script("ssh",String.raw`printf 'SSH %s\n' "$*" >> "$MOCK_LOG"
[[ $* == *"uname -m"* ]] && echo x86_64
exit 0
`);
 script("rsync",String.raw`printf 'RSYNC %s\n' "$*" >> "$MOCK_LOG"`);
 const run=(path,args=[],extra={})=>spawnSync("bash",[path,...args],{env:{...env,...extra},encoding:"utf8"});
 const calls=()=>readFileSync(log,"utf8");
 try {
  writeFileSync(binary,"OLD");let r=run(helper,[binary,"amd64"]);assert.equal(r.status,0,r.stderr);
  assert.equal(readFileSync(binary,"utf8"),"STOCK_BINARY");assert.equal(calls(),"STOCK\n");
  writeFileSync(log,"");r=run(helper,[binary,"amd64"],{GO2RTC_WRITE_FIX:"1"});assert.equal(r.status,0,r.stderr);
  assert.equal(readFileSync(binary,"utf8"),"PATCHED_BINARY");assert.equal(calls(),"BUILDER\n");
  writeFileSync(binary,"KEEP");r=run(helper,[binary,"amd64"],{MOCK_DOWNLOAD_FAIL:"1"});assert.notEqual(r.status,0);
  assert.equal(readFileSync(binary,"utf8"),"KEEP");
  r=run(helper,[binary,"amd64"],{GO2RTC_WRITE_FIX:"invalid"});assert.notEqual(r.status,0);assert.equal(readFileSync(binary,"utf8"),"KEEP");
  writeFileSync(log,"");r=run(updater,["root@example.invalid"]);assert.equal(r.status,0,r.stderr);
  assert.ok(!calls().includes("BUILDER"));assert.ok(calls().includes("GO2RTC_WRITE_FIX=0 bash"));
  assert.ok(calls().includes("--exclude .go2rtc-cache"));
  writeFileSync(log,"");r=run(updater,["root@example.invalid"],{GO2RTC_WRITE_FIX:"1"});assert.equal(r.status,0,r.stderr);
  assert.ok(calls().includes("BUILDER"));assert.ok(calls().includes("GO2RTC_WRITE_FIX=1 bash"));
  assert.ok(!calls().includes("--exclude .go2rtc-cache"));
 } finally {rmSync(dir,{recursive:true,force:true});}
});
