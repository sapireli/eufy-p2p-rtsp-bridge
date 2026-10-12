import test from "node:test";
import assert from "node:assert/strict";
import {mkdtempSync,writeFileSync,readFileSync,rmSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {fileURLToPath} from "node:url";
import {spawnSync} from "node:child_process";

test("verified writer repair is default; failed builds preserve binary and updates transfer its cache",()=>{
 const dir=mkdtempSync(join(tmpdir(),"go2rtc-routing-"));
 const log=join(dir,"calls");const binary=join(dir,"go2rtc");
 const helper=fileURLToPath(new URL("../../deploy/install-go2rtc.sh",import.meta.url));
 const updater=fileURLToPath(new URL("../../deploy/update-debian.sh",import.meta.url));
 const env={...process.env,PATH:dir+":"+process.env.PATH,MOCK_LOG:log,GO2RTC_VERSION:"1.9.14"};
 const script=(name,code)=>writeFileSync(join(dir,name),"#!/bin/bash\n"+code,{mode:0o755});
 script("python3",String.raw`echo BUILDER >> "$MOCK_LOG"
[[ "$MOCK_BUILD_FAIL" == 1 ]] && exit 8
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
  assert.equal(readFileSync(binary,"utf8"),"PATCHED_BINARY");assert.equal(calls(),"BUILDER\n");
  writeFileSync(binary,"KEEP");r=run(helper,[binary,"amd64"],{MOCK_BUILD_FAIL:"1"});assert.notEqual(r.status,0);
  assert.equal(readFileSync(binary,"utf8"),"KEEP");
  r=run(helper,[binary,"amd64"],{GO2RTC_VERSION:"1.9.15"});assert.notEqual(r.status,0);assert.equal(readFileSync(binary,"utf8"),"KEEP");
  writeFileSync(log,"");r=run(updater,["root@example.invalid"]);assert.equal(r.status,0,r.stderr);
  assert.ok(calls().includes("BUILDER"));assert.ok(calls().includes("uname -m"));
  assert.ok(calls().includes("bash '/root/eufy-wall-source/deploy/install-server.sh'"));
  assert.ok(!calls().includes("--exclude .go2rtc-cache"));
  writeFileSync(log,"");r=run(updater,["root@example.invalid"],{MOCK_BUILD_FAIL:"1"});assert.notEqual(r.status,0);
  assert.ok(!calls().includes("RSYNC"));assert.ok(!calls().includes("install-server.sh"));
 } finally {rmSync(dir,{recursive:true,force:true});}
});
