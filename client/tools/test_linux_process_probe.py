import unittest
from linux_process_probe import camera_pids, stopped_work

KEYS = {"door": ["T8214510242321E6", "front_door_cle"],
        "garage": ["T8425T1124130A0C", "garage_cle"]}


class ProcessProbeTest(unittest.TestCase):
    def test_actual_alias_and_percent_encoding(self):
        commands = {11: b"gst-launch-1.0\0location=rtsp://user:secret@host/Front_Door_CLE?x=1\0",
                    12: b"gst-launch-1.0\0location=rtsp://host/garage%5fcle\0"}
        self.assertEqual(camera_pids(commands, KEYS), {"door": 11, "garage": 12})

    def test_serial_before_alias_resolution(self):
        commands = {11: b"location=rtsp://host/T8214510242321E6\0",
                    12: b"location=rtsp://host/T8425T1124130A0C\0"}
        self.assertEqual(camera_pids(commands, KEYS), {"door": 11, "garage": 12})

    def test_composite_child_cannot_be_stopped_as_one_camera(self):
        with self.assertRaises(ValueError):
            camera_pids({11: b"location=rtsp://host/front_door_cle\0location=rtsp://host/garage_cle\0"}, KEYS)

    def test_replaced_or_reused_pid_never_stopped(self):
        for current in ({"door": (13, 101), "garage": (12, 100)},
                        {"door": (11, 102), "garage": (12, 100)}):
            signals = []
            with self.assertRaises(RuntimeError):
                with stopped_work({"door": (11, 100), "garage": (12, 100)},
                                  lambda: current, lambda: signals.append("STOP"),
                                  lambda: signals.append("CONT")):
                    self.fail("entered invalid work comparison")
            self.assertEqual(signals, [])

    def test_sampling_error_restores_stopped_work(self):
        signals = []
        with self.assertRaises(ValueError):
            with stopped_work({"door": (11, 100)}, lambda: {"door": (11, 100)},
                              lambda: signals.append("STOP"), lambda: signals.append("CONT")):
                raise ValueError("sample failed")
        self.assertEqual(signals, ["STOP", "CONT"])


if __name__ == "__main__":
    unittest.main()
