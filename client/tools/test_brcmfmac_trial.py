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
                      "/sys/module/brcmfmac_wcc": "", "/sys/module/brcmutil": "",
                      "/sys/module/cfg80211": "", "/sys/class/net/wlan0": "", "/sys/class/net/wlan0/operstate": "up"}
        self.calls = []; self.fail = None; self.freezeWrites = []
        self.now = 0; self.freezeAt = 0; self.noAck = False; self.route = True; self.scopeFail = False; self.noRoute = False; self.absentScope = False; self.unsafeMonitor = False; self.monitorChecks = 0; self.unsafeAfter = 10000
    def monitor_safe(self):
        self.monitorChecks += 1
        return not self.unsafeMonitor and self.monitorChecks <= self.unsafeAfter
    def scope_absent(self, scope): return self.absentScope
    def network_ready(self, address): return self.route
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
        if "--scope" in args and self.scopeFail: raise RuntimeError("scoped ifup timeout")
        if self.fail == "ifup" and "--scope" in args: raise RuntimeError("injected ifup failure")
        if "--scope" in args and not self.noRoute: self.route = True
        if self.fail == args[0]:
            if required: raise RuntimeError("injected failure")
            return False
        if args[:2] == ["modprobe", "-r"]:
            self.files.pop("/sys/module/brcmfmac/srcversion", None)
            self.files.pop("/sys/module/brcmfmac_wcc", None)
            self.route = False
            self.files.pop("/sys/class/net/wlan0", None)
            self.files.pop("/sys/module/brcmutil", None)
            self.files.pop("/sys/module/cfg80211", None)
        if args[0] == "insmod":
            if not all("/sys/module/" + dep in self.files for dep in ("brcmutil", "cfg80211")):
                raise RuntimeError("insmod unresolved dependency")
            self.files["/sys/module/brcmfmac/srcversion"] = "candidate"
            self.files["/sys/class/net/wlan0"] = ""
        if args in (["modprobe", "brcmutil"], ["modprobe", "cfg80211"]):
            self.files["/sys/module/" + args[1]] = ""
        if args == ["modprobe", "brcmfmac"]:
            self.files["/sys/module/brcmfmac/srcversion"] = "stock"
            self.files["/sys/class/net/wlan0"] = ""
            self.files["/sys/module/brcmutil"] = self.files["/sys/module/cfg80211"] = ""
        if args == ["modprobe", "brcmfmac_wcc"]: self.files["/sys/module/brcmfmac_wcc"] = ""
        return True

class Tests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.h = Fake()
        self.m = {"kernel": "exact", "interface": "wlan0", "bridgeProbeAddress": "192.168.23.158", "manifestPath": "/manifest",
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
    def test_unsafe_monitor_descendants_rejected_before_arm(self):
        self.h.unsafeMonitor = True
        with self.assertRaises(RuntimeError): self.w.trial()
        self.assertEqual(self.h.calls, [])
        self.assertEqual(self.h.freezeWrites, [])
    def test_new_unsafe_monitor_child_after_freeze_rejected_and_thawed(self):
        self.h.unsafeAfter = 1
        with self.assertRaises(RuntimeError): self.w.trial()
        self.assertFalse(any(a[0] == "ifdown" for a in self.h.calls))
        self.assertFalse(any(a[:2] == ["modprobe", "-r"] for a in self.h.calls))
        self.assertEqual(self.h.files[trial.FREEZER], "0")
    def test_scope_retains_daemons_after_success_and_uses_unique_names(self):
        self.w.trial(); first = self.w.load()["networkScope"]
        self.w.rollback(); second = self.w.load()["networkScope"]
        self.assertNotEqual(first, second)
        self.assertIn(["systemctl", "stop", first], self.h.calls)
        self.assertNotIn(["systemctl", "stop", second], self.h.calls)
        self.assertEqual(self.w.load()["phase"], "restored")
    def test_scoped_ifup_failure_cleans_owned_scope(self):
        state = {"phase": "restoring", "ownsFreeze": False, "hadWcc": True}
        self.w.save(state); self.h.scopeFail = True
        with self.assertRaises(RuntimeError): self.w.scoped_ifup(state)
        args = next(a for a in self.h.calls if "--scope" in a)
        unit = next(a[7:] for a in args if a.startswith("--unit="))
        self.assertIn(["systemctl", "stop", unit], self.h.calls)
        self.assertIsNone(self.w.load()["networkScope"])
    def test_scope_not_created_is_safely_retired(self):
        state = {"phase": "restoring", "networkScope": "eufy-brcmfmac-ifup-uncreated.scope"}
        self.w.save(state); self.h.fail = "systemctl"; self.h.absentScope = True
        self.w.close_network_scope(state)
        self.assertIsNone(self.w.load()["networkScope"])
    def test_live_scope_cleanup_failure_prevents_duplicate_ifup(self):
        state = {"phase": "restoring", "networkScope": "eufy-brcmfmac-ifup-live.scope"}
        self.w.save(state); self.h.fail = "systemctl"
        with self.assertRaises(RuntimeError): self.w.scoped_ifup(state)
        self.assertFalse(any("--scope" in a for a in self.h.calls))
        self.assertEqual(self.w.load()["networkScope"], state["networkScope"])
    def test_stock_identity_without_netdev_forces_reload(self):
        state = {"phase": "trial", "ownsFreeze": False, "hadWcc": True}
        self.w.save(state); self.h.files.pop("/sys/class/net/wlan0")
        self.w.rollback()
        self.assertIn(["modprobe", "-r", "brcmfmac_wcc", "brcmfmac"], self.h.calls)
        self.assertIn(["modprobe", "brcmfmac"], self.h.calls)
        self.assertEqual(self.w.load()["phase"], "restored")
    def test_candidate_resolves_dependencies_after_recursive_unload(self):
        self.w.trial()
        start = self.h.calls.index(["modprobe", "-r", "brcmfmac_wcc", "brcmfmac"])
        self.assertEqual(self.h.calls[start+1:start+4],
                         [["modprobe", "brcmutil"], ["modprobe", "cfg80211"], ["insmod", "/candidate"]])
        self.assertEqual(self.w.loaded(), "candidate")
        self.w.rollback()
        self.assertEqual(self.w.loaded(), "stock")
    def test_stock_control_same_guard_unload_and_network_path(self):
        # Explicit control requires no candidate artifact; ordinary trial still does.
        self.m.pop("candidate")
        self.w.trial(stock_control=True)
        self.assertEqual(self.w.load()["mode"], "stock-control")
        self.assertEqual(self.w.loaded(), "stock")
        self.assertTrue(any(a[0] == "systemd-run" for a in self.h.calls))
        self.assertIn(["modprobe", "-r", "brcmfmac_wcc", "brcmfmac"], self.h.calls)
        self.assertIn(["modprobe", "brcmfmac"], self.h.calls)
        self.assertIn(["ifdown", "--force", "wlan0"], self.h.calls)
        self.assertTrue(any("--scope" in a and a[-3:] == ["ifup", "--force", "wlan0"] for a in self.h.calls))
        self.assertFalse(any(a[0] == "insmod" for a in self.h.calls))
        self.assertEqual(self.h.files[trial.FREEZER], "1")
        self.w.rollback()
        self.assertEqual(self.h.files[trial.FREEZER], "0")
        self.assertEqual(self.w.load()["phase"], "restored")
    def test_ifup_success_without_ipv4_route_keeps_guard(self):
        self.w.trial(); self.h.route = False; self.h.noRoute = True
        with self.assertRaises(RuntimeError): self.w.rollback()
        self.assertEqual(self.w.load()["phase"], "restoring")
        self.assertFalse(any(a == ["systemctl", "stop", trial.UNIT + ".timer"] for a in self.h.calls))
        self.h.route = True; self.h.noRoute = False; self.w.rollback()
        self.assertEqual(self.w.load()["phase"], "restored")
    def test_initial_missing_ipv4_route_never_arms(self):
        self.h.route = False
        with self.assertRaises(RuntimeError): self.w.trial()
        self.assertEqual(self.h.calls, [])
    def test_candidate_trial_does_not_bypass_candidate_validation(self):
        self.m["candidate"]["sha256"] = "bad"
        with self.assertRaises(RuntimeError): self.w.trial()
        self.assertEqual(self.h.calls, [])
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
    def test_resumed_owned_freeze_waits_for_actual_ack(self):
        state = {"phase": "restoring", "ownsFreeze": True, "hadWcc": True}
        self.w.save(state)
        self.h.files[trial.FREEZER] = "1"
        self.h.freezeAt = 0.06
        self.h.files["/sys/module/brcmfmac/srcversion"] = "candidate"
        original = self.h.run
        def check(args, **kw):
            if args[0] == "ifdown": self.assertGreaterEqual(self.h.now, 0.06)
            return original(args, **kw)
        self.h.run = check
        self.w.rollback()
        self.assertEqual(self.h.files[trial.FREEZER], "0")
    def test_guard_has_real_runtime_bound(self):
        self.w.trial()
        args = next(a for a in self.h.calls if a[0] == "systemd-run")
        for prop in ("--property=Type=exec", "--property=RuntimeMaxSec=90s", "--property=TimeoutStopSec=5s"):
            self.assertIn(prop, args)
        self.assertFalse(any("TimeoutStartSec" in a for a in args))
    def test_freeze_ack_failure_restores_without_unload(self):
        self.h.noAck = True
        with self.assertRaises(RuntimeError): self.w.trial()
        self.assertFalse(any(a[:2] == ["modprobe", "-r"] for a in self.h.calls))
        self.assertEqual(self.h.files[trial.FREEZER], "0")
        self.assertEqual(self.w.load()["phase"], "restoring")
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
