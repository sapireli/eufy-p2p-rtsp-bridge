#!/usr/bin/env python3
"""Exercise controller decisions without CEC hardware or systemd mutations."""
import concurrent.futures
import json
import os
import signal
from pathlib import Path
import subprocess
import tempfile
import time
import unittest

SCRIPT = Path(__file__).with_name("tv-power.sh")
MOCK = r'''#!/usr/bin/env python3
import fcntl,json,os,pathlib,sys,time
name=pathlib.Path(sys.argv[0]).name
args=sys.argv[1:]
if name=='id': print('1000'); sys.exit()
if name=='sudo': os.execvp(args[1],args[1:])
with open(os.environ['MOCK_DATA'],'r+') as f:
 fcntl.flock(f,fcntl.LOCK_EX)
 d=json.load(f); rc=0; out=''; systemctl_delay=d.get('systemctl_delay',0)
 if name=='readlink': out=d.get('executable','/usr/local/bin/eufy-wall')
 elif name=='cec-client':
  command=sys.stdin.read().strip()
  if command=='pow 0':
   out='power status: '+d['tv']; delay=d.get('delay',0)
  else:
   d['tv']='on' if command=='on 0' else 'standby'; delay=d.get('cec_command_delay',0); d['cec_command_started']=True
 elif name=='pgrep':
  rc=0 if d.get('orphan',False) else 1
  if rc==0: out='321'
 elif name=='ps': out='wall'
 elif name=='cat': out='/usr/local/bin/eufy-wall\0-config\0/etc/eufy-wall.yaml\0'
 elif name=='kill': d['orphan']=False
 elif name=='logger': d.setdefault('logs',[]).append(args[-1])
 elif name=='find': out='123' if d.get('cgroup_tasks',False) else ''
 elif name=='systemd-run':
  unit=next(x.split('=',1)[1] for x in args if x.startswith('--unit='))+'.service'
  d['units'].append(unit); d['queued']+=1
 elif name=='systemctl':
  if args[0]=='show' and args[1]!='eufy-wall.service':
   state=d.get('worker_state','active') if args[1] in d['units'] else 'inactive'
   out='ActiveState='+state+'\nJob='+str(d.get('worker_job',0))
  elif args[0]=='show':
   s=d['client']; out='ActiveState='+('active' if s=='on' else ('activating' if s=='starting' else 'inactive'))+'\nSubState='+('running' if s=='on' else 'dead')+'\nMainPID='+('123' if s=='on' else '0')+'\nControlGroup='+d.get('group','')
  elif args[0]=='is-active': rc=0 if args[-1] in d['units'] else 3
  elif args[0]=='list-units': out='\n'.join(d['units'])
  elif args[0]=='kill':
   d['orphan']=False; d['cgroup_tasks']=False
  elif args[0]=='--no-block':
   action,unit=args[1:3]
   if unit=='eufy-wall.service':
    d['actions'].append(action)
    if action=='start' and d.get('orphan'): d['started_with_orphan']=True
    if not d.get('hold_service',False):
     d['client']='on' if action in ('start','restart') else 'off'
     if action=='restart': d['executable']='/usr/local/bin/eufy-wall'
   elif unit in d['units']: d['units'].remove(unit)
 f.seek(0); json.dump(d,f); f.truncate()
if name=='cec-client' and delay: time.sleep(delay)
if name=='systemctl' and systemctl_delay: time.sleep(systemctl_delay)
if out: print(out)
sys.exit(rc)
'''


class Controller(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        root = Path(self.tmp.name)
        tools = root / "bin"
        tools.mkdir()
        for cmd in ("id", "sudo", "readlink", "pgrep", "find", "ps", "cat", "kill", "logger", "systemctl", "systemd-run", "cec-client"):
            p = tools / cmd
            p.write_text(MOCK)
            p.chmod(0o755)
        self.data = root / "data.json"
        self.runtime = root / "runtime"
        self.runtime.mkdir()
        self.env = dict(os.environ, PATH=f"{tools}:{os.environ['PATH']}",
                        MOCK_DATA=str(self.data), TV_POWER_RUNTIME=str(self.runtime))
        self.seed()

    def tearDown(self):
        self.tmp.cleanup()

    def seed(self, **kw):
        d = dict(tv="standby", client="off", units=[], queued=0, actions=[])
        d.update(kw)
        self.data.write_text(json.dumps(d))

    def run_script(self, *args, timeout=12):
        return subprocess.run([str(SCRIPT), *args], env=self.env, capture_output=True,
                              text=True, timeout=timeout)

    def unknown(self, result):
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def pending(self, target, unit="tv-power-cec-test.service"):
        d = json.loads(self.data.read_text())
        d['units'].append(unit)
        self.data.write_text(json.dumps(d))
        (self.runtime / 'pending').write_text(f'{target} {unit}\n')
        return unit

    def test_parity_is_required(self):
        self.assertEqual(self.run_script('state').stdout, 'off\n')
        self.seed(tv='on', client='on')
        self.assertEqual(self.run_script('state').stdout, 'on\n')

    def test_tv_on_service_crash_queues_recovery_once(self):
        self.seed(tv='on')
        self.unknown(self.run_script('state'))
        self.unknown(self.run_script('state'))
        self.assertEqual(json.loads(self.data.read_text())['queued'], 1)

    def test_tv_off_running_client_queues_stop(self):
        self.seed(client='on')
        self.unknown(self.run_script('state'))
        self.assertTrue((self.runtime / 'pending').read_text().startswith('off '))

    def test_pending_on_does_not_get_overridden_by_old_standby(self):
        self.pending('on')
        self.unknown(self.run_script('state'))
        self.assertEqual(json.loads(self.data.read_text())['queued'], 0)

    def test_starting_worker_still_preserves_explicit_intent(self):
        self.seed(worker_state='activating')
        self.pending('on')
        self.unknown(self.run_script('state'))
        self.assertEqual(json.loads(self.data.read_text())['queued'], 0)

    def test_queued_inactive_worker_preserves_opposite_explicit_intent(self):
        self.seed(tv='on', client='on', worker_state='inactive', worker_job=876)
        self.pending('off')
        self.unknown(self.run_script('state'))
        self.assertTrue((self.runtime / 'pending').read_text().startswith('off '))
        self.assertEqual(json.loads(self.data.read_text())['queued'], 0)

    def test_queue_logs_distinguish_state_reconcile_and_explicit_request(self):
        self.seed(tv='on')
        self.unknown(self.run_script('state'))
        logs=json.loads(self.data.read_text())['logs']
        self.assertTrue(any('origin=state-reconcile target=on' in x and 'tv=on client=off' in x for x in logs))
        result=self.run_script('off')
        self.assertEqual(result.returncode,0)
        logs=json.loads(self.data.read_text())['logs']
        self.assertTrue(any('origin=explicit target=off' in x for x in logs))

    def test_unknown_and_transitional_tv_never_mutate_service(self):
        for tv in ('unknown', 'in transition from standby to on'):
            self.seed(tv=tv, client='on')
            self.unknown(self.run_script('state'))
            self.assertEqual(json.loads(self.data.read_text())['queued'], 0)

    def test_service_transition_and_orphan_are_not_off(self):
        for state in ({'client': 'starting'}, {'orphan': True}):
            self.seed(**state)
            self.unknown(self.run_script('state'))

    def test_systemd_active_with_wrong_process_is_not_on(self):
        self.seed(tv='on', client='on', executable='/usr/bin/sleep')
        self.unknown(self.run_script('state'))

    def test_leftover_pipeline_cgroup_tasks_are_not_off(self):
        self.seed(group='/', cgroup_tasks=True)
        self.unknown(self.run_script('state'))

    def test_dead_worker_does_not_leave_stuck_intent(self):
        (self.runtime / 'pending').write_text('on missing.service\n')
        self.assertEqual(self.run_script('state').stdout, 'off\n')

    def test_explicit_latest_request_supersedes_pending(self):
        self.pending('on')
        result = self.run_script('off')
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, '')
        self.assertTrue((self.runtime / 'pending').read_text().startswith('off '))

    def test_old_worker_cannot_act_on_same_target_new_generation(self):
        self.seed(tv='on')
        self.pending('on')
        result = self.run_script('--worker-on', 'old.service')
        self.assertEqual(result.returncode, 0)
        self.assertEqual(json.loads(self.data.read_text())['actions'], [])

    def test_worker_checks_client_and_cec_before_completion(self):
        self.seed(tv='on')
        unit = self.pending('on')
        result = self.run_script('--worker-on', unit)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(json.loads(self.data.read_text())['actions'], ['start'])
        self.assertFalse((self.runtime / 'pending').exists())

    def test_worker_repairs_invalid_running_service(self):
        self.seed(tv='on', client='on', executable='/usr/bin/sleep')
        unit = self.pending('on')
        result = self.run_script('--worker-on', unit)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(json.loads(self.data.read_text())['actions'], ['restart'])
        self.assertFalse((self.runtime / 'pending').exists())

    def test_atomic_binary_update_keeps_valid_running_process(self):
        self.seed(tv='on', client='on', executable='/usr/local/bin/eufy-wall (deleted)')
        self.assertEqual(self.run_script('state').stdout, 'on\n')

    def test_worker_can_wake_tv_without_initial_power_reply(self):
        self.seed(tv='unknown')
        unit = self.pending('on')
        result = self.run_script('--worker-on', unit)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(json.loads(self.data.read_text())['tv'], 'on')
        self.assertFalse((self.runtime / 'pending').exists())

    def test_off_worker_reaps_remaining_role_and_pipeline(self):
        self.seed(orphan=True, group='/', cgroup_tasks=True)
        unit = self.pending('off')
        result = self.run_script('--worker-off', unit, timeout=25)
        self.assertEqual(result.returncode, 0)
        d = json.loads(self.data.read_text())
        self.assertFalse(d['orphan'])
        self.assertFalse(d['cgroup_tasks'])
        self.assertFalse((self.runtime / 'pending').exists())

    def test_on_worker_cleans_orphan_before_managed_start(self):
        self.seed(tv='on', orphan=True)
        unit = self.pending('on')
        result = self.run_script('--worker-on', unit, timeout=25)
        self.assertEqual(result.returncode, 0)
        d = json.loads(self.data.read_text())
        self.assertFalse(d['orphan'])
        self.assertEqual(d['actions'], ['start'])
        self.assertFalse(d.get('started_with_orphan', False))
        self.assertFalse((self.runtime / 'pending').exists())

    def test_busy_cec_does_not_block_latest_explicit_intent(self):
        self.seed(tv='unknown', client='on', cec_command_delay=6)
        unit = self.pending('on')
        worker = subprocess.Popen([str(SCRIPT), '--worker-on', unit], env=self.env,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                  start_new_session=True)
        try:
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                if json.loads(self.data.read_text()).get('cec_command_started'):
                    break
                time.sleep(.05)
            else:
                self.fail('Worker never reached CEC actuator')
            started = time.monotonic()
            result = self.run_script('off')
            self.assertEqual(result.returncode, 0)
            self.assertLess(time.monotonic() - started, 2)
            self.assertTrue((self.runtime / 'pending').read_text().startswith('off '))
        finally:
            os.killpg(worker.pid, signal.SIGTERM)
            worker.communicate(timeout=3)

    def test_concurrent_polls_queue_one_worker(self):
        self.seed(tv='on')
        with concurrent.futures.ThreadPoolExecutor(max_workers=5) as pool:
            results = list(pool.map(lambda _: self.run_script('state'), range(5)))
        for result in results:
            self.unknown(result)
        self.assertEqual(json.loads(self.data.read_text())['queued'], 1)

    def test_hung_cec_is_bounded(self):
        self.seed(delay=30)
        started = time.monotonic()
        self.unknown(self.run_script('state'))
        self.assertLess(time.monotonic() - started, 10)

    def test_hung_systemd_is_bounded(self):
        self.seed(tv='on', client='on', systemctl_delay=30)
        started = time.monotonic()
        self.unknown(self.run_script('state', timeout=15))
        self.assertLess(time.monotonic() - started, 14)


if __name__ == '__main__':
    unittest.main()
