#!/usr/bin/env python3
"""Build/cache verified patched go2rtc; never replace an installed binary on failure."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request

VERSION = "1.9.14"
SOURCE_SHA256 = "e3d59e553dfd0085889a2956281cfc0fd78bb7b4d6269d1c90c217d2ffcf2c7b"
SOURCE_URL = "https://codeload.github.com/AlexxIT/go2rtc/tar.gz/refs/tags/v" + VERSION
ROOT = Path(__file__).resolve().parents[1]
PATCH = ROOT / "server/patches/go2rtc-v1.9.14/fail-closed-write.patch"
TEST = ROOT / "server/patches/go2rtc-v1.9.14/interleaved_write_failure_test.go"
LIFECYCLE_TEST = ROOT / "server/patches/go2rtc-v1.9.14/consumer_lifecycle_test.go"
TARGETS = {"amd64":("amd64",None), "arm64":("arm64",None), "arm":("arm","7"), "armv6":("arm","6")}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def profile(target):
    return {"schemaVersion":1,"upstreamVersion":VERSION,"sourceSha256":SOURCE_SHA256,
            "patchSha256":digest(PATCH.read_bytes()),"testSha256":digest(TEST.read_bytes()),"lifecycleTestSha256":digest(LIFECYCLE_TEST.read_bytes()),"target":"linux-"+target,"buildFlags":["-trimpath"],"cgoEnabled":False}


def atomic_write(path, data, mode=0o644):
    path = Path(path)
    path.parent.mkdir(parents=True,exist_ok=True)
    fd,name = tempfile.mkstemp(prefix="."+path.name+"-",dir=path.parent)
    try:
        with os.fdopen(fd,"wb") as out:
            out.write(data)
            out.flush()
            os.fsync(out.fileno())
        os.chmod(name,mode)
        os.replace(name,path)
    finally:
        if os.path.exists(name):os.unlink(name)


def verified_cache(cache,target):
    binary=cache/("go2rtc_linux_"+target)
    manifest=cache/(binary.name+".json")
    try:
        meta=json.loads(manifest.read_text())
        if not isinstance(meta,dict):return None
        if any(meta.get(k)!=v for k,v in profile(target).items()):return None
        data=binary.read_bytes()
        if not data or digest(data)!=meta.get("binarySha256") or not meta.get("goVersion"):return None
        return data,meta
    except (OSError,ValueError):
        return None


def source_bytes(cache, supplied):
    archive=Path(supplied) if supplied else cache/("source-v"+VERSION+".tar.gz")
    if archive.exists():data=archive.read_bytes()
    elif supplied:raise RuntimeError("supplied source archive is missing")
    else:
        with urllib.request.urlopen(SOURCE_URL,timeout=60) as response:data=response.read()
        if digest(data)!=SOURCE_SHA256:raise RuntimeError("source archive SHA-256 mismatch")
        atomic_write(archive,data)
    if digest(data)!=SOURCE_SHA256:raise RuntimeError("source archive SHA-256 mismatch")
    return data


def build(cache,target,supplied=None):
    data=source_bytes(cache,supplied)
    if not shutil.which("go"):raise RuntimeError("verified cache unavailable: Go is required to build; upstream go.mod declares go 1.24.0. Seed the cache with update-debian.sh or install a supported Go toolchain")
    with tempfile.TemporaryDirectory(prefix="go2rtc-build-") as temporary:
        work=Path(temporary)
        archive=work/"source.tar.gz";archive.write_bytes(data)
        with tarfile.open(archive,"r:gz") as source:
            for entry in source.getmembers():
                parts=Path(entry.name).parts
                if not parts or parts[0]!="go2rtc-"+VERSION or ".." in parts or Path(entry.name).is_absolute() or not (entry.isfile() or entry.isdir()):
                    raise RuntimeError("unsafe source archive entry")
            source.extractall(work)
        tree=work/("go2rtc-"+VERSION)
        subprocess.run(["patch","--batch","-p1","-i",str(PATCH)],cwd=tree,check=True)
        shutil.copyfile(TEST,tree/"pkg/rtsp/interleaved_write_failure_test.go")
        environment=os.environ.copy()
        for key in ("GOOS","GOARCH","GOARM","CGO_ENABLED"):
            environment.pop(key,None)
        environment.update(GOFLAGS="",GOWORK="off")
        go_version=subprocess.check_output(["go","version"],cwd=tree,env=environment,text=True).strip()
        subprocess.run(["go","test","./pkg/rtsp","./pkg/tcp","./pkg/core","-count=1"],cwd=tree,env=environment,check=True)
        lifecycle_directory=tree/"internal/streams"
        shutil.copyfile(LIFECYCLE_TEST,lifecycle_directory/"consumer_lifecycle_test.go")
        lifecycle_sources=sorted(p.name for p in lifecycle_directory.glob("*.go") if not p.name.endswith("_test.go"))
        subprocess.run(["go","test","-count=1",*lifecycle_sources,"consumer_lifecycle_test.go"],cwd=lifecycle_directory,env=environment,check=True)
        architecture,arm=TARGETS[target]
        environment.update(GOOS="linux",GOARCH=architecture,CGO_ENABLED="0")
        if arm:environment["GOARM"]=arm
        output=work/"go2rtc"
        subprocess.run(["go","build","-trimpath","-o",str(output)],cwd=tree,env=environment,check=True)
        binary=output.read_bytes()
        meta={**profile(target),"goVersion":go_version,"binarySha256":digest(binary),"verification":"rtsp,tcp,core packages and focused streams lifecycle regression"}
        atomic_write(cache/("go2rtc_linux_"+target),binary,0o755)
        atomic_write(cache/("go2rtc_linux_"+target+".json"),(json.dumps(meta,sort_keys=True,indent=2)+"\n").encode())
        return binary,meta


def install(binary,meta,target):
    target=Path(target)
    # Immutable provenance is staged before the final binary rename.
    provenance=target.with_name(target.name+".provenance-"+meta["binarySha256"]+".json")
    atomic_write(provenance,(json.dumps(meta,sort_keys=True,indent=2)+"\n").encode())
    atomic_write(target,binary,0o755)
    return str(provenance)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target",choices=TARGETS,required=True)
    parser.add_argument("--cache",type=Path,default=ROOT/"deploy/.go2rtc-cache")
    parser.add_argument("--source-archive",help="Offline archive; still verified against the pinned upstream SHA")
    parser.add_argument("--install",type=Path)
    parser.add_argument("--verify-only",action="store_true",help="Reject invalid cache without rebuilding or downloading")
    args=parser.parse_args()
    args.cache.mkdir(parents=True,exist_ok=True)
    with (args.cache/".lock").open("a") as lock:
        fcntl.flock(lock,fcntl.LOCK_EX)
        artifact=verified_cache(args.cache,args.target)
        if artifact is None:
            if args.verify_only:raise RuntimeError("cached patched binary/provenance mismatch")
            artifact=build(args.cache,args.target,args.source_archive)
        binary,meta=artifact
        provenance=install(binary,meta,args.install) if args.install else None
    print(json.dumps({"binary":str(args.cache/("go2rtc_linux_"+args.target)),"binarySha256":meta["binarySha256"],"installedProvenance":provenance}))


if __name__=="__main__":
    try:main()
    except (RuntimeError,OSError,subprocess.SubprocessError,tarfile.TarError) as error:
        raise SystemExit("go2rtc build/install failed; existing binary preserved: "+str(error))
