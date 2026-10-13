#!/usr/bin/env python3
"""Portable metadata-only failure preservation and native flag-accounting fixtures."""
import importlib.util
import json
import pathlib
import subprocess
import tempfile
import unittest

HERE = pathlib.Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("capture", HERE / "capture-udp-metadata.py")
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)


class CaptureTests(unittest.TestCase):
    def run_fixture(self, code, *, timeout=False, size=40, counts=None):
        stats={"records":1,"recordBytes":40,"packetSocketDrops":0,"missingTimestamp":0,"truncations":0,"dataTruncations":0,"controlTruncations":0,"ioError":0}
        stats.update(counts or {})
        def runner(args, **kwargs):
            self.assertFalse(kwargs["check"])
            self.assertEqual(kwargs["umask"],0o077)
            pathlib.Path(args[4]).write_bytes(bytes(size))
            kwargs["stderr"].write((json.dumps(stats)+"\n").encode());kwargs["stderr"].flush()
            if timeout:raise subprocess.TimeoutExpired(args,kwargs["timeout"])
            return subprocess.CompletedProcess(args,code)
        with tempfile.TemporaryDirectory() as root:
            output=pathlib.Path(root)/"private"
            result=module.capture("/trusted/native-observer","eth0","192.0.2.1",1,output,runner=runner)
            self.assertTrue(result["exportComplete"])
            self.assertEqual((output/"capture.metadata").read_bytes(),bytes(size))
            self.assertEqual((output/"capture.exit").read_text().strip(),str(result["nativeExitStatus"]))
            self.assertEqual((output.stat().st_mode&0o777),0o700)
            self.assertTrue(all((f.stat().st_mode&0o777)==0o600 for f in output.iterdir()))
            self.assertFalse(any(output.glob(".capture-*")))
            return result

    def test_real_failed_child_cli_exports_before_returning_exit7(self):
        with tempfile.TemporaryDirectory() as root:
            binary=pathlib.Path(root)/"synthetic-native"
            binary.write_text("#!/usr/bin/env python3\nimport pathlib,sys,json\npathlib.Path(sys.argv[4]).write_bytes(bytes(40))\nprint(json.dumps({'records':1,'recordBytes':40,'packetSocketDrops':0,'missingTimestamp':0,'truncations':1,'dataTruncations':0,'controlTruncations':1,'ioError':0}),file=sys.stderr)\nraise SystemExit(7)\n")
            binary.chmod(0o700);output=pathlib.Path(root)/"exported"
            result=subprocess.run(["python3",str(HERE/"capture-udp-metadata.py"),str(binary),"eth0","192.0.2.1","1",str(output)],capture_output=True,text=True)
            self.assertEqual(result.returncode,7)
            manifest=json.loads((output/"manifest.json").read_text())
            self.assertTrue(manifest["exportComplete"]);self.assertFalse(manifest["captureValid"])
            self.assertEqual((output/"capture.metadata").read_bytes(),bytes(40))
            self.assertEqual((output/"capture.exit").read_text(),"7\n")
            self.assertFalse(any(output.glob(".capture-*")))

    def test_exit7_preserves_failed_metadata_stats_exit_before_cleanup(self):
        result=self.run_fixture(7,counts={"truncations":1,"controlTruncations":1})
        self.assertFalse(result["captureValid"]);self.assertEqual(result["nativeExitStatus"],7)
        self.assertEqual(result["stats"]["controlTruncations"],1)

    def test_success_does_not_relax_strict_gate(self):
        self.assertTrue(self.run_fixture(0)["captureValid"])
        self.assertFalse(self.run_fixture(0,counts={"packetSocketDrops":1})["captureValid"])
        self.assertFalse(self.run_fixture(0,counts={"dataTruncations":1})["captureValid"])

    def test_partial_and_timeout_are_preserved_but_invalid(self):
        self.assertFalse(self.run_fixture(7,size=19)["captureValid"])
        result=self.run_fixture(0,timeout=True,size=19)
        self.assertEqual(result["nativeExitStatus"],124);self.assertTrue(result["timedOut"]);self.assertFalse(result["captureValid"])

    def test_collision_and_bad_duration_never_invoke_observer(self):
        with tempfile.TemporaryDirectory() as root:
            def forbidden(*args,**kwargs):raise AssertionError("observer must not run")
            with self.assertRaises(FileExistsError):module.capture("/trusted/bin","eth0","192.0.2.1",1,root,runner=forbidden)
            with self.assertRaises(ValueError):module.capture("/trusted/bin","eth0","192.0.2.1",301,pathlib.Path(root)/"unused",runner=forbidden)

    def test_maximum_300_seconds_preserves_timeout_and_strict_export(self):
        def runner(args, **kwargs):
            self.assertEqual(args[3], "300"); self.assertEqual(kwargs["timeout"], 305)
            pathlib.Path(args[4]).write_bytes(bytes(40))
            stats={"records":1,"recordBytes":40,"packetSocketDrops":0,"missingTimestamp":0,
                   "truncations":0,"dataTruncations":0,"controlTruncations":0,"ioError":0}
            kwargs["stderr"].write((json.dumps(stats)+"\n").encode());kwargs["stderr"].flush()
            return subprocess.CompletedProcess(args,0)
        with tempfile.TemporaryDirectory() as root:
            result=module.capture("/trusted/bin","eth0","192.0.2.1",300,pathlib.Path(root)/"out",runner=runner)
            self.assertTrue(result["captureValid"]);self.assertTrue(result["exportComplete"])
            self.assertEqual(result["requestedSeconds"],300)
    def test_native_duration_bound_without_socket_or_capture(self):
        source=(HERE/"udp-headers.c").read_text()
        start=source.index("#define MAX_CAPTURE_SECONDS")
        end=source.index("static unsigned truncation_kind",start)
        guard=source[start:end]
        code=guard+"int main(void){if(duration_allowed(0)||!duration_allowed(1)||!duration_allowed(45)||!duration_allowed(300)||duration_allowed(301))return 1;return 0;}\n"
        with tempfile.TemporaryDirectory() as root:
            path=pathlib.Path(root)/"duration.c";path.write_text(code)
            subprocess.run(["cc","-Wall","-Wextra","-Werror",str(path),"-o",root+"/duration"],check=True)
            subprocess.run([root+"/duration"],check=True)

    def test_export_failure_retains_stage_and_manifest(self):
        with tempfile.TemporaryDirectory() as root:
            output=pathlib.Path(root)/"private"
            def runner(args,**kwargs):
                pathlib.Path(args[4]).write_bytes(bytes(40))
                (output/"capture.metadata").write_text("collision")
                return subprocess.CompletedProcess(args,7)
            result=module.capture("/trusted/bin","eth0","192.0.2.1",1,output,runner=runner)
            self.assertFalse(result["exportComplete"]);self.assertTrue(pathlib.Path(result["retainedStage"]).is_dir())
            self.assertEqual((pathlib.Path(result["retainedStage"])/"capture.metadata").read_bytes(),bytes(40))
            self.assertEqual((output/"capture.metadata").read_text(),"collision")
            self.assertTrue((output/"manifest.json").is_file())

    def test_actual_native_statistics_format_keeps_distinct_counts(self):
        source=(HERE/"udp-headers.c").read_text()
        statement=source[source.rindex(' fprintf(stderr,"{'):source.rindex(" return io_error")]
        prefix="#include <stdio.h>\n#include <stdint.h>\nint main(void){uint64_t wall=1,start=1,stop=2,cpu=1,cpuend=2,records=0,received=1,missing=0,trunc=1,data_trunc=1,control_trunc=1,trigger_wall=0;unsigned maximum_received_bytes=128,maximum_control_bytes=32,record_bytes=40;int io_error=0,buf=1024,extended=0;struct {unsigned tp_packets,tp_drops;}stats={1,0};uint8_t packets[32][128];char controls[32][32];\n"
        with tempfile.TemporaryDirectory() as root:
            path=pathlib.Path(root)/"stats.c";path.write_text(prefix+statement+"return 0;}\n")
            subprocess.run(["cc","-std=c11","-Wall","-Wextra","-Werror",str(path),"-o",root+"/stats"],check=True)
            result=subprocess.run([root+"/stats"],check=True,capture_output=True,text=True)
            stats=json.loads(result.stderr)
            self.assertEqual(stats["truncations"],1)
            self.assertEqual(stats["dataTruncations"],1)
            self.assertEqual(stats["controlTruncations"],1)
            self.assertEqual(stats["dataBufferBytes"],128)
            self.assertEqual(stats["controlBufferBytes"],32)

    def test_native_distinct_truncation_flags(self):
        source=(HERE/"udp-headers.c").read_text()
        function=source[source.index("static unsigned truncation_kind"):source.index("static uint64_t ns(")]
        code="#include <sys/socket.h>\n"+function+"\nint main(void){if(truncation_kind(0)!=0)return 1;if(truncation_kind(MSG_TRUNC)!=1)return 2;if(truncation_kind(MSG_CTRUNC)!=2)return 3;if(truncation_kind(MSG_TRUNC|MSG_CTRUNC)!=3)return 4;return 0;}\n"
        with tempfile.TemporaryDirectory() as root:
            path=pathlib.Path(root)/"flags.c";path.write_text(code)
            subprocess.run(["cc","-std=c11","-Wall","-Wextra","-Werror",str(path),"-o",root+"/flags"],check=True)
            subprocess.run([root+"/flags"],check=True)


if __name__ == "__main__":unittest.main()
