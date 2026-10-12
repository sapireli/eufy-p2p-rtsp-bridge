import test from "node:test";
import assert from "node:assert/strict";
import {spawnSync} from "node:child_process";
import {fileURLToPath} from "node:url";

test("patched deployment preserves installed binary on archive, patch, cache and rename failures", () => {
  const script=fileURLToPath(new URL("../../deploy/build-go2rtc.py",import.meta.url));
  const result=spawnSync("python3",["-c",String.raw`
import importlib.util,json,pathlib,sys,tempfile,unittest,os
from unittest import mock
sp=importlib.util.spec_from_file_location('builder',sys.argv[1]);m=importlib.util.module_from_spec(sp);sp.loader.exec_module(m)
class Integrity(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.root=pathlib.Path(self.tmp.name);self.cache=self.root/'cache';self.cache.mkdir();self.target=self.root/'go2rtc';self.target.write_bytes(b'KEEP_EXISTING_BINARY')
 def tearDown(self):self.tmp.cleanup()
 def seed(self):
  data=b'TEST_ARTIFACT_NOT_EXECUTED';meta={**m.profile('amd64'),'binarySha256':m.digest(data),'goVersion':'test fixture'}
  (self.cache/'go2rtc_linux_amd64').write_bytes(data);(self.cache/'go2rtc_linux_amd64.json').write_text(json.dumps(meta));return data,meta
 def invoke(self,extra=[]):
  with mock.patch.object(sys,'argv',['builder','--target','amd64','--cache',str(self.cache),'--install',str(self.target),*extra]):m.main()
 def preserved(self):self.assertEqual(self.target.read_bytes(),b'KEEP_EXISTING_BINARY')
 def test_bad_archive_never_builds_or_replaces(self):
  bad=self.root/'bad.tar.gz';bad.write_bytes(b'UNVERIFIED_SOURCE')
  with mock.patch.object(m.subprocess,'run') as run:
   with self.assertRaisesRegex(RuntimeError,'archive SHA-256'):self.invoke(['--source-archive',str(bad)])
   run.assert_not_called()
  self.preserved()
 def test_changed_patch_invalidates_cache_and_preserves_binary(self):
  patched=self.root/'patch';patched.write_bytes(m.PATCH.read_bytes())
  with mock.patch.object(m,'PATCH',patched):
   self.seed();patched.write_bytes(patched.read_bytes()+b'CHANGED_PATCH')
   with self.assertRaisesRegex(RuntimeError,'provenance mismatch'):self.invoke(['--verify-only'])
  self.preserved()
 def test_corrupted_cached_binary_preserves_binary(self):
  self.seed();(self.cache/'go2rtc_linux_amd64').write_bytes(b'CORRUPTED')
  with self.assertRaisesRegex(RuntimeError,'provenance mismatch'):self.invoke(['--verify-only'])
  self.preserved()
 def test_wrong_manifest_sha_preserves_binary(self):
  _,meta=self.seed();meta['binarySha256']='0'*64;(self.cache/'go2rtc_linux_amd64.json').write_text(json.dumps(meta))
  with self.assertRaisesRegex(RuntimeError,'provenance mismatch'):self.invoke(['--verify-only'])
  self.preserved()
 def test_malformed_manifest_preserves_binary(self):
  self.seed();(self.cache/'go2rtc_linux_amd64.json').write_text('[]')
  with self.assertRaisesRegex(RuntimeError,'provenance mismatch'):self.invoke(['--verify-only'])
  self.preserved()
 def test_final_rename_failure_preserves_binary_and_removes_temporary_file(self):
  self.seed();replace=m.os.replace
  def injected(source,destination):
   if pathlib.Path(destination)==self.target:raise OSError('injected final rename failure')
   return replace(source,destination)
  with mock.patch.object(m.os,'replace',injected):
   with self.assertRaisesRegex(OSError,'final rename'):self.invoke(['--verify-only'])
  self.preserved();self.assertFalse(list(self.root.glob('.go2rtc-*')))
 def test_verified_cached_install_needs_no_compiler_and_records_provenance(self):
  data,meta=self.seed()
  with mock.patch.object(m,'build',side_effect=AssertionError('cache unexpectedly rebuilt')):self.invoke(['--verify-only'])
  self.assertEqual(self.target.read_bytes(),data)
  provenance=self.target.with_name('go2rtc.provenance-'+meta['binarySha256']+'.json')
  self.assertEqual(json.loads(provenance.read_text()),meta)
  self.assertTrue(self.target.stat().st_mode&0o111)
unittest.main(argv=['test'],verbosity=1)
`,script],{encoding:"utf8"});
  assert.equal(result.status,0,result.stderr);
});
