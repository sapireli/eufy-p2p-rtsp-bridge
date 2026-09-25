#!/usr/bin/env python3
"""Fixtures for the read-only qualification collector; no hardware required."""
import importlib.util
import io
import json
import os
import pathlib
import tempfile
import unittest
from unittest import mock


SOURCE = pathlib.Path(__file__).resolve().parents[1] / "qualify-client.py"
spec = importlib.util.spec_from_file_location("qualify_client", SOURCE)
qualify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(qualify)


class QualifyClientTests(unittest.TestCase):
    def test_modetest_parser_ignores_other_ids_and_text(self):
        output = "Connectors:\n70 0 connected\nPlanes:\nid crtc fb CRTC x,y\n31 0 0 0,0\n42 0 0 0,0\nproperties:\n999 0 0\n"
        self.assertEqual(qualify.parse_planes(output), [31, 42])

    def test_fps_parser_keeps_only_numbers(self):
        metrics = {"frames_rendered": None}
        line = "rtsp://admin:secret@10.0.0.2/live token=abc rendered: 180, dropped: 3, current: 29.97, average: 28.50"
        qualify.parse_fps_line(line, metrics)
        serialized = json.dumps(metrics)
        self.assertEqual(metrics["frames_rendered"], 180)
        self.assertEqual(metrics["frames_dropped"], 3)
        self.assertEqual(metrics["average_fps"], 28.5)
        for secret in ("admin", "secret", "10.0.0.2", "abc"):
            self.assertNotIn(secret, serialized)
        qualify.parse_fps_line("rendered: 181, dropped: 4, fps: 27.50, drop rate: 1.00", metrics)
        self.assertEqual(metrics["current_fps"], 27.5)
        self.assertIsNone(metrics["average_fps"])
        qualify.parse_caps_line("caps = video/x-h265, width=(int)1920, height=(int)1080, location=rtsp://secret", metrics)
        self.assertEqual((metrics["codec"], metrics["width"], metrics["height"]), ("h265", 1920, 1080))
        self.assertNotIn("secret", json.dumps(metrics))

    def test_report_row_redacts_labels_and_never_claims_qualification(self):
        report = {
            "qualification": "unverified",
            "host": {"target": "Raspberry Pi 4", "os": "Pi OS rtsp://admin:secret@10.0.0.2/live",
                     "kernel": "6.1.0 token=topsecret", "connectors": [{"name": "card0-HDMI-A-1", "modes": ["1920x1080"]}],
                     "drm_plane_ids": [31, 42], "drm_planes_status": "observed"},
            "probe": {"sink": "kmssink", "average_fps": 29.9, "frames_dropped": 1, "cpu_percent_mean": 12.4},
            "recovery": {"bridge_recovery_s": 4.2},
        }
        row = qualify.markdown(report)
        self.assertIn("unverified", row)
        self.assertIn("29.9", row)
        for secret in ("admin", "secret", "10.0.0.2", "topsecret"):
            self.assertNotIn(secret, row)

    def test_recovery_watcher_requires_baseline_outage_and_two_good_polls(self):
        clock = [0.0]
        sequence = iter([True, True, True, False, False, True, True])

        def sleep(seconds):
            clock[0] += seconds

        with mock.patch.object(qualify.time, "monotonic", side_effect=lambda: clock[0]), \
             mock.patch.object(qualify.time, "sleep", side_effect=sleep), \
             mock.patch.object(qualify, "bridge_ok", side_effect=lambda _: next(sequence)), \
             mock.patch.object(qualify, "systemctl_active", return_value=True), \
             mock.patch("sys.stderr", new_callable=io.StringIO) as stderr:
            result = qualify.recovery_watch("http://10.0.0.2:3000", "bridge_restart", 30)
        self.assertTrue(result["baseline_ready"])
        self.assertEqual(result["bridge_recovery_s"], 3.0)
        self.assertEqual(result["status"], "recovered")
        self.assertNotIn("10.0.0.2", json.dumps(result) + stderr.getvalue())

    def test_recovery_without_outage_is_not_a_success(self):
        clock = [0.0]
        with mock.patch.object(qualify.time, "monotonic", side_effect=lambda: clock[0]), \
             mock.patch.object(qualify.time, "sleep", side_effect=lambda seconds: clock.__setitem__(0, clock[0] + seconds)), \
             mock.patch.object(qualify, "bridge_ok", return_value=True), \
             mock.patch.object(qualify, "systemctl_active", return_value=True), \
             mock.patch("sys.stderr", new_callable=io.StringIO):
            result = qualify.recovery_watch("http://bridge:3000", "network_interruption", 5)
        self.assertEqual(result["status"], "no_outage_observed")
        self.assertIsNone(result["bridge_recovery_s"])

    def test_probe_is_bounded_and_keeps_raw_url_out_of_result(self):
        with tempfile.TemporaryDirectory() as directory:
            fake = pathlib.Path(directory) / "gst-launch-1.0"
            fake.write_text("#!/bin/sh\nprintf 'last-message = rendered: 40, dropped: 0, current: 20.00, average: 19.50\\n'\nsleep 30\n")
            fake.chmod(0o755)
            with mock.patch.dict(os.environ, {"PATH": directory + os.pathsep + os.environ["PATH"]}):
                result = qualify.probe("rtsp://10.0.0.2/private-stream", 1, "fakesink")
        self.assertEqual(result["status"], "progress")
        self.assertEqual(result["frames_rendered"], 40)
        self.assertLess(result["duration_observed_s"], 6)
        self.assertNotIn("10.0.0.2", json.dumps(result))

    def test_target_classification_never_infers_qualification(self):
        for model, expected in (("Raspberry Pi Model B Rev 2", "Raspberry Pi 1"),
                                ("Raspberry Pi 3 Model B", "Raspberry Pi 3"),
                                ("Raspberry Pi 4 Model B", "Raspberry Pi 4"),
                                ("Raspberry Pi 5 Model B", "Raspberry Pi 5")):
            self.assertEqual(qualify.classify(model, "aarch64", "debian"), expected)
        self.assertEqual(qualify.classify("PC", "x86_64", "debian"), "Debian x86-64")
        self.assertEqual(qualify.classify("PC", "i686", "debian"), "other or unclassified")

    def test_safe_text_strips_secrets_and_markdown_delimiters(self):
        result = qualify.safe_text("Model | rtsp://u:p@192.168.1.9/live password=bad\nNEXT")
        for value in ("|", "192.168.1.9", "password", "bad", "u:p"):
            self.assertNotIn(value, result)

    def test_cli_writes_allowlisted_report_without_stream_url(self):
        host = {"target": "Raspberry Pi 3", "model": "Raspberry Pi 3 Model B", "os": "Debian",
                "kernel": "6.1", "architecture": "armv7l", "connectors": [], "drm_plane_ids": [],
                "drm_planes_status": "unavailable", "gstreamer_elements": {}}
        probe_result = {"status": "progress", "sink": "fakesink", "frames_rendered": 80,
                        "frames_dropped": 0, "average_fps": 20.0, "cpu_percent_mean": 50.0}
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "report.json"
            with mock.patch.dict(os.environ, {"QUALIFY_RTSP_URL": "rtsp://10.0.0.2/secret-stream"}), \
                 mock.patch.object(qualify, "inventory", return_value=host), \
                 mock.patch.object(qualify, "probe", return_value=probe_result), \
                 mock.patch("sys.stdout", new_callable=io.StringIO) as stdout:
                self.assertEqual(qualify.main(["--output", str(path), "--duration", "5"]), 0)
            content = path.read_text()
            self.assertIn('"qualification": "unverified', content)
            self.assertNotIn("secret-stream", content + stdout.getvalue())
            self.assertNotIn("10.0.0.2", content + stdout.getvalue())

    def test_cli_rejects_embedded_credentials(self):
        with tempfile.TemporaryDirectory() as directory, \
             mock.patch.dict(os.environ, {"QUALIFY_RTSP_URL": "rtsp://user:password@10.0.0.2/live"}), \
             mock.patch("sys.stderr", new_callable=io.StringIO) as stderr:
            with self.assertRaises(SystemExit):
                qualify.main(["--output", str(pathlib.Path(directory) / "report.json")])
            self.assertNotIn("password", stderr.getvalue())
        for url in ("rtsp://:password@10.0.0.2/live", "rtsp://10.0.0.2/live?token=secret", "rtsp://[invalid/live"):
            self.assertFalse(qualify.valid_url(url, "rtsp"))
        self.assertFalse(qualify.valid_url("http://bridge:3000/api", "http", origin_only=True))


if __name__ == "__main__":
    unittest.main()
