import importlib.util
from pathlib import Path
import tempfile
import unittest
import sys
import time
from unittest.mock import patch, Mock

spec = importlib.util.spec_from_file_location("trial", Path(__file__).with_name("brcmfmac_trial.py"))
trial = importlib.util.module_from_spec(spec); spec.loader.exec_module(trial)

class Fake(trial.Host):
    def __init__(self, frozen="0"):
        self.files = {"/proc/sys/kernel/osrelease": "exact", trial.FREEZER: frozen,
                      "/sys/module/brcmfmac/srcversion": "stock",
                      "/sys/module/brcmfmac_wcc": "", "/sys/class/net/wlan0/operstate": "up"}
        self.calls = []; self.fail = None; self.freezeWrites = []
        self.now = 0; self.freezeAt = 0; self.noAck = False
    def clock(self): return self.now
    def sleep(self, seconds): self.now += seconds
    def read(self, p):
        if p.endswith("cgroup.events"):
            val = self.files[trial.FREEZER]
            if val == "1" and (self.noAck or self.now < self.freezeAt): val = "0"
            return "populated 1\nfrozen " + val
        return self.files[p]
    def exists(self, p): return p in self.files
    def write(self, p, v):
        self.files[p] = v; self.freezeWrites.append(v); self.freezeAt = self.now + 0.04
    def digest(self, p): return "hash"
    def run(self, args, timeout=10, required=True):
        self.calls.append(args)
        if self.fail == args[0]:
            if required: raise RuntimeError("injected failure")
            return False
        if args[:2] == ["modprobe", "-r"]:
            self.files.pop("/sys/module/brcmfmac/srcversion", None)
            self.files.pop("/sys/module/brcmfmac_wcc", None)
        if args[0] == "insmod": self.files["/sys/module/brcmfmac/srcversion"] = "candidate"
        if args == ["modprobe", "brcmfmac"]: self.files["/sys/module/brcmfmac/srcversion"] = "stock"
        if args == ["modprobe", "brcmfmac_wcc"]: self.files["/sys/module/brcmfmac_wcc"] = ""
        return True

class Tests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.h = Fake()
        self.m = {"kernel": "exact", "interface": "wlan0", "manifestPath": "/manifest",
                  "stockSrcversion": "stock", "stock": [{"installed": "/stock", "backup": "/backup", "sha256": "hash"}],
                  "candidate": {"path": "/candidate", "sha256": "hash", "srcversion": "candidate"}}
        self.path = Path(self.tmp.name) / "state.json"
        self.w = trial.Worker(self.h, self.m, self.path, "/worker")
    def test_independent_restore_and_idempotency(self):
        self.w.trial()
        self.assertEqual(self.h.calls[1][0], "systemd-run")
        self.assertEqual(self.h.files[trial.FREEZER], "1")
        # Separate worker invocation shares durable freeze ownership.
        trial.Worker(self.h, self.m, self.path, "/worker").rollback()
        self.assertEqual(self.h.files[trial.FREEZER], "0")
        self.assertEqual(self.w.loaded(), "stock")
        before = list(self.h.calls); self.w.rollback(); self.assertEqual(before, self.h.calls)
        self.assertEqual([a for a in self.h.calls if a[:2] == ["modprobe", "-r"]],
                         [["modprobe", "-r", "brcmfmac_wcc", "brcmfmac"]] * 2)
        self.assertFalse(any("eufy-wall.service" in a and a[1] != "is-active" for a in self.h.calls))
    def test_does_not_thaw_external_freeze(self):
        self.h.files[trial.FREEZER] = "1"
        self.w.trial(); self.w.rollback()
        self.assertEqual(self.h.freezeWrites, [])
    def test_load_failure_restores_stock(self):
        self.h.fail = "insmod"
        with self.assertRaises(RuntimeError): self.w.trial()
        self.assertEqual(self.w.loaded(), "stock")
        self.assertEqual(self.w.load()["phase"], "restored")
        self.assertEqual(self.h.files[trial.FREEZER], "0")
    def test_failed_recovery_keeps_guard_retryable(self):
        self.w.trial(); self.h.fail = "ifup"
        with self.assertRaises(RuntimeError): self.w.rollback()
        self.assertEqual(self.w.load()["phase"], "restoring")
        self.assertEqual(self.h.files[trial.FREEZER], "0")
        self.assertFalse(any(a == ["systemctl", "stop", trial.UNIT + ".timer"] for a in self.h.calls))
        self.h.fail = None; self.w.rollback()
        self.assertEqual(self.w.load()["phase"], "restored")
    def test_bad_digest_never_arms_or_changes_network(self):
        self.m["stock"][0]["sha256"] = "wrong"
        with self.assertRaises(RuntimeError): self.w.trial()
        self.assertEqual(self.h.calls, [])
    def test_unfinished_trial_cannot_replace_state(self):
        self.w.trial(); before = self.path.read_bytes()
        with self.assertRaises(RuntimeError): self.w.trial()
        self.assertEqual(before, self.path.read_bytes())
    def test_freeze_acknowledged_before_interface_down(self):
        original = self.h.run
        def check(args, **kw):
            if args[0] == "ifdown":
                self.assertGreaterEqual(self.h.now, self.h.freezeAt)
            return original(args, **kw)
        self.h.run = check
        self.w.trial()
    def test_freeze_ack_failure_restores_without_unload(self):
        self.h.noAck = True
        with self.assertRaises(RuntimeError): self.w.trial()
        self.assertFalse(any(a[:2] == ["modprobe", "-r"] for a in self.h.calls))
        self.assertEqual(self.h.files[trial.FREEZER], "0")
        self.assertEqual(self.w.load()["phase"], "restored")
    def test_interrupt_kills_and_reaps_owned_group(self):
        proc = Mock(pid=43210)
        proc.wait.side_effect = [KeyboardInterrupt(), 0]
        with patch.object(trial.subprocess, "Popen", return_value=proc), patch.object(trial.os, "killpg") as kill:
            with self.assertRaises(KeyboardInterrupt): trial.Host().run(["owned-hook"])
        kill.assert_called_once_with(43210, trial.signal.SIGKILL)
        self.assertEqual(proc.wait.call_count, 2)
    def test_timeout_kills_hook_descendants(self):
        marker = str(Path(self.tmp.name) / "late")
        child = "import time,pathlib;time.sleep(0.25);pathlib.Path(" + repr(marker) + ").write_text('late')"
        parent = "import subprocess,sys,time;subprocess.Popen([sys.executable,'-c'," + repr(child) + "]);time.sleep(10)"
        with self.assertRaises(RuntimeError):
            trial.Host().run([sys.executable, "-c", parent], timeout=0.1)
        time.sleep(0.3)
        self.assertFalse(Path(marker).exists())
    def test_lock_excludes_independent_workers(self):
        with trial.lock(self.path):
            with self.assertRaises(BlockingIOError):
                with trial.lock(self.path): pass

if __name__ == "__main__": unittest.main()
