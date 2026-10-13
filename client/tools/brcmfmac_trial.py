#!/usr/bin/env python3
"""Local bounded driver trial. Never replaces installed modules or client policy.

Requires a root-only manifest and state directory; see rollback preflight evidence.
Successful trial deliberately leaves its independent 90-second rollback armed.
"""
import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import signal
import sys
import tempfile
import time

UNIT = "eufy-brcmfmac-trial-rollback"
FREEZER = "/sys/fs/cgroup/system.slice/dietpi-wifi-monitor.service/cgroup.freeze"

class Host:
    def clock(self): return time.monotonic()
    def sleep(self, seconds): time.sleep(seconds)
    def read(self, path):
        return Path(path).read_text().strip()
    def exists(self, path):
        return Path(path).exists()
    def write(self, path, text):
        Path(path).write_text(text)
    def digest(self, path):
        return hashlib.sha256(Path(path).read_bytes()).hexdigest()
    def run(self, args, timeout=10, required=True):
        # Never export command output (WPA hooks may print sensitive fields).
        process = subprocess.Popen(args, stdout=subprocess.DEVNULL,
                                   stderr=subprocess.DEVNULL, start_new_session=True)
        try:
            ok = process.wait(timeout=timeout) == 0
        except BaseException as error:
            # Kill hook descendants too; ifupdown may have spawned WPA/DHCP hooks.
            # No unbounded wait even if a kernel task cannot be killed immediately.
            try: os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError: pass
            try: process.wait(timeout=1)
            except subprocess.TimeoutExpired: pass
            if not isinstance(error, subprocess.TimeoutExpired): raise
            ok = False
        if required and not ok:
            raise RuntimeError("bounded step failed: " + args[0])
        return ok

class Worker:
    def __init__(self, host, manifest, state_path, executable):
        self.h, self.m = host, manifest
        self.path, self.executable = Path(state_path), executable
    def save(self, state):
        # Atomic durable state precedes any ownership-changing action.
        fd, name = tempfile.mkstemp(dir=self.path.parent)
        try:
            os.fchmod(fd, 0o600)
            with os.fdopen(fd, "w") as out:
                json.dump(state, out); out.flush(); os.fsync(out.fileno())
            os.replace(name, self.path)
            dfd = os.open(self.path.parent, os.O_RDONLY)
            try: os.fsync(dfd)
            finally: os.close(dfd)
        finally:
            if os.path.exists(name): os.unlink(name)
    def load(self):
        return json.loads(self.path.read_text()) if self.path.exists() else None
    def validate(self, candidate=False):
        if self.h.read("/proc/sys/kernel/osrelease") != self.m["kernel"]:
            raise RuntimeError("kernel mismatch")
        for module in self.m["stock"]:
            for key in ("installed", "backup"):
                if self.h.digest(module[key]) != module["sha256"]:
                    raise RuntimeError("stock digest mismatch")
        if candidate:
            c = self.m["candidate"]
            if c["srcversion"] == self.m["stockSrcversion"]:
                raise RuntimeError("candidate identity must differ from stock")
            if self.h.digest(c["path"]) != c["sha256"]:
                raise RuntimeError("candidate digest mismatch")
        if self.m.get("interface") != "wlan0":
            raise RuntimeError("only verified wlan0 layout supported")
    def loaded(self):
        p = "/sys/module/brcmfmac/srcversion"
        return self.h.read(p) if self.h.exists(p) else None
    def wait_frozen(self, value):
        deadline = self.h.clock() + 3
        events = str(Path(FREEZER).with_name("cgroup.events"))
        while self.h.clock() < deadline:
            fields = dict(line.split() for line in self.h.read(events).splitlines())
            if fields.get("frozen") == value: return
            self.h.sleep(0.02)
        raise RuntimeError("monitor freezer acknowledgment timeout")
    def freeze(self, state):
        if self.h.exists(FREEZER) and self.h.read(FREEZER) == "0":
            state["ownsFreeze"] = True
            self.save(state)
            self.h.write(FREEZER, "1")
        if self.h.exists(FREEZER):
            self.wait_frozen("1")
    def thaw(self, state):
        if state.get("ownsFreeze"):
            if self.h.exists(FREEZER):
                self.h.write(FREEZER, "0")
                self.wait_frozen("0")
            state["ownsFreeze"] = False
            self.save(state)
    def trial(self):
        self.validate(True)
        previous = self.load()
        if previous and previous["phase"] != "restored":
            raise RuntimeError("unfinished trial; rollback first")
        if self.loaded() != self.m["stockSrcversion"]:
            raise RuntimeError("trial requires loaded stock identity")
        if self.h.read("/sys/class/net/wlan0/operstate") != "up":
            raise RuntimeError("trial requires existing active interface")
        state = {"phase": "arming", "ownsFreeze": False,
                 "hadWcc": self.h.exists("/sys/module/brcmfmac_wcc"),
                 "clientActive": self.h.run(["systemctl", "is-active", "--quiet", "eufy-wall.service"], required=False)}
        self.save(state)
        # Explicit Python path and argument vector; no shell or remote dependency.
        self.h.run(["systemd-run", "--unit=" + UNIT, "--on-active=90s",
                    "--timer-property=AccuracySec=1s", "--property=Type=exec",
                    "--property=RuntimeMaxSec=90s", "--property=TimeoutStopSec=5s",
                    "--property=Restart=on-failure", "--property=RestartSec=2s",
                    "--property=StartLimitIntervalSec=300s", "--property=StartLimitBurst=3",
                    sys.executable, self.executable, "rollback", "--manifest", self.m["manifestPath"],
                    "--state", str(self.path)])
        state["phase"] = "armed"; self.save(state)
        try:
            self.freeze(state)
            self.h.run(["ifdown", "--force", "wlan0"], timeout=15)
            self.h.run(["modprobe", "-r", "brcmfmac_wcc", "brcmfmac"])
            self.h.run(["insmod", self.m["candidate"]["path"]])
            if state["hadWcc"]: self.h.run(["modprobe", "brcmfmac_wcc"])
            if self.loaded() != self.m["candidate"]["srcversion"]:
                raise RuntimeError("candidate loaded identity mismatch")
            self.h.run(["ifup", "--force", "wlan0"], timeout=20)
            state["phase"] = "trial"; self.save(state)
        except BaseException:
            self.rollback()
            raise
    def rollback(self):
        state = self.load()
        if state is None or state["phase"] == "restored": return
        state["phase"] = "restoring"; self.save(state)
        try:
            self.validate()
            self.freeze(state)
            # Skip repeated unload once a prior invocation restored stock.
            if self.loaded() != self.m["stockSrcversion"]:
                self.h.run(["ifdown", "--force", "wlan0"], timeout=15, required=False)
                self.h.run(["modprobe", "-r", "brcmfmac_wcc", "brcmfmac"])
                self.h.run(["modprobe", "brcmfmac"])
            if state["hadWcc"] and not self.h.exists("/sys/module/brcmfmac_wcc"):
                self.h.run(["modprobe", "brcmfmac_wcc"])
            if self.loaded() != self.m["stockSrcversion"]:
                raise RuntimeError("stock loaded identity mismatch")
            self.h.run(["ifup", "--force", "wlan0"], timeout=20)
            if self.h.read("/sys/class/net/wlan0/operstate") != "up":
                raise RuntimeError("interface recovery unverified")
        finally:
            self.thaw(state)
        state["phase"] = "restored"; self.save(state)
        # Stop timer only after restoration; never cancel the running guard service.
        self.h.run(["systemctl", "stop", UNIT + ".timer"], required=False)

@contextlib.contextmanager
def lock(path):
    with open(str(path) + ".lock", "a") as out:
        os.chmod(out.name, 0o600)
        # Nonblocking: guard does not wait forever for a wedged trial process.
        fcntl.flock(out, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield

def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("action", choices=["trial", "rollback"])
    ap.add_argument("--manifest", required=True)
    ap.add_argument("--state", required=True)
    a = ap.parse_args()
    if os.geteuid() != 0: raise SystemExit("root required")
    state = Path(a.state).resolve(); manifest = Path(a.manifest).resolve()
    for p in (state.parent, manifest):
        st = p.stat()
        if st.st_uid != 0 or st.st_mode & 0o022: raise SystemExit("root-owned non-writable paths required")
    m = json.loads(manifest.read_text()); m["manifestPath"] = str(manifest)
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt("termination requested")
    signal.signal(signal.SIGTERM, interrupted)
    with lock(state):
        worker = Worker(Host(), m, state, str(Path(__file__).resolve()))
        getattr(worker, a.action)()

if __name__ == "__main__": main()
