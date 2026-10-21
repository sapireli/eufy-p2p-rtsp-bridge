#!/usr/bin/env python3
"""Offline tiny synthetic compositor test; does not access cameras or a display."""
import os
import pathlib
import re
import subprocess
import tempfile
import time


def write_config(path, pipeline):
    temporary = path.with_suffix(".new")
    temporary.write_text("[tile]\npipeline=" + pipeline + "\n")
    temporary.replace(path)


# With every camera idle, force-live composition must still produce black frames.
with tempfile.TemporaryDirectory(prefix="eufytile-idle-") as directory:
    idle = pathlib.Path(directory) / "idle.ini"
    write_config(idle, "")
    result = subprocess.run(["gst-launch-1.0", "-e", "eufytile", "config-file="+str(idle), "!", "compositor", "force-live=true", "ignore-inactive-pads=true", "background=black", "!", "video/x-raw,width=32,height=32,framerate=10/1", "!", "fakesink", "num-buffers=5", "sync=false"], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=4)
    print(result.stdout)
    assert result.returncode == 0 and "Got EOS" in result.stdout, "idle compositor stopped producing black frames"
    print("PASS: initially idle tile keeps black compositor live")


with tempfile.TemporaryDirectory(prefix="eufytile-") as directory:
    root = pathlib.Path(directory)
    healthy, faulty = root / "healthy.ini", root / "faulty.ini"
    source = "videotestsrc is-live=true pattern=black ! video/x-raw,format=RGBA,width=32,height=32,framerate=10/1"
    write_config(healthy, source + " ! appsink name=tile_output")
    write_config(faulty, source + " ! identity error-after=10 ! appsink name=tile_output")
    command = ["gst-launch-1.0", "-e", "eufytile", "name=healthy", "config-file="+str(healthy), "!", "mix.sink_0", "eufytile", "name=faulty", "config-file="+str(faulty), "!", "mix.sink_1", "compositor", "name=mix", "force-live=true", "ignore-inactive-pads=true", "background=black", "!", "video/x-raw,width=64,height=32,framerate=10/1", "!", "fakesink", "sync=false"]
    with (root / "log").open("w") as log:
        process = subprocess.Popen(command, stdout=log, stderr=log)
        try:
            time.sleep(6)
            assert process.poll() is None, "compositor exited during tile errors"
            write_config(faulty, source + " ! appsink name=tile_output")
            time.sleep(6)
            assert process.poll() is None, "compositor exited during tile repoint"
        finally:
            process.terminate()
            try:
                process.wait(timeout=4)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    output = (root / "log").read_text()
    print(output)
    healthy_stats = [(int(a), int(b)) for a,b in re.findall(r"eufytile healthy: delivered=(\d+) restarts=(\d+)", output)]
    assert len(healthy_stats) >= 2 and healthy_stats[-1][0] > healthy_stats[0][0], "healthy sibling stopped delivering"
    assert all(restarts == 1 for _,restarts in healthy_stats), "healthy sibling restarted"
    assert output.count("eufytile faulty: decoder started") >= 3, "faulty tile did not retry independently"
    assert output.count("eufytile faulty: configuration updated") == 2, "live tile repoint not observed"
    assert "ERROR: from element" not in output, "tile error escaped into compositor bus"
    print("PASS: isolated errors, persistent healthy decoder, live tile repoint, persistent compositor")

# A synthetic capacity fixture exercises the actual decoder-owned ERROR bus path.
# Its transform passes 32x32 raw video through, with exactly one simulated slot.
import shlex

with tempfile.TemporaryDirectory(prefix="eufytile-priority-") as directory:
    root = pathlib.Path(directory)
    flags = shlex.split(subprocess.check_output(["pkg-config", "--cflags", "--libs", "gstreamer-base-1.0"], text=True))
    subprocess.run([os.environ.get("CC", "cc"), "-std=c99", "-O2", "-Wall", "-Wextra", "-Werror", "-shared", "-fPIC", str(pathlib.Path(__file__).with_name("eufytile_test_decoder.c")), "-o", str(root / "libgsteufytiletest.so"), *flags], check=True)
    low, high = root / "low.ini", root / "high.ini"
    source = "videotestsrc is-live=true pattern=black ! video/x-raw,format=RGBA,width=32,height=32,framerate=10/1"
    hardware = source + " ! v4l2testh264dec ! appsink name=tile_output"
    software = source + " ! appsink name=tile_output"
    def priority_config(path, pinned, priority, pipeline):
        temporary = path.with_suffix(".new")
        temporary.write_text("[tile]\npipeline="+pipeline+"\nsoftware-pipeline="+software+"\nhardware-pinned="+str(pinned).lower()+"\nhardware-priority="+str(priority)+"\n")
        temporary.replace(path)
    priority_config(low, False, 100, hardware)
    priority_config(high, True, 0, "")
    env = os.environ.copy()
    env["GST_PLUGIN_PATH"] = str(root) + os.pathsep + env.get("GST_PLUGIN_PATH", "")
    command = ["gst-launch-1.0", "-e", "eufytile", "name=low", "config-file="+str(low), "!", "mix.sink_0", "eufytile", "name=high", "config-file="+str(high), "!", "mix.sink_1", "compositor", "name=mix", "force-live=true", "ignore-inactive-pads=true", "background=black", "!", "video/x-raw,width=64,height=32,framerate=10/1", "!", "fakesink", "sync=false"]
    with (root / "log").open("w") as log:
        process = subprocess.Popen(command, stdout=log, stderr=log, env=env)
        try:
            time.sleep(1)
            priority_config(high, True, 0, hardware)
            time.sleep(5)
            assert process.poll() is None, "priority reclamation stopped compositor"
        finally:
            process.terminate()
            try:
                process.wait(timeout=4)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    output = (root / "log").read_text()
    print(output)
    assert "eufytile high: requesting hardware release from low" in output, "pinned requester did not reclaim eligible decoder"
    assert "eufytile low: yielding hardware" in output, "victim did not acknowledge preemption"
    assert "eufytile low: decoder started mode=2" in output, "victim did not select its software fallback"
    assert re.search(r"eufytile high: delivered=[1-9]\d* restarts=2 mode=0 active=1", output), "requester did not regain hardware after actual release"
    assert "ERROR: from element" not in output, "capacity error escaped into compositor bus"
    print("PASS: decoder-owned allocation error, pin priority, release-before-retry, isolated software fallback")

# Return to the original inode with new same-size contents inside one stat-second.
# This reproduces inode reuse after rapid atomic replacement deterministically.
with tempfile.TemporaryDirectory(prefix="eufytile-stat-") as directory:
    root = pathlib.Path(directory)
    target, first, second = root / "tile.ini", root / "first.ini", root / "second.ini"
    source = "videotestsrc is-live=true pattern=black ! video/x-raw,format=RGBA,width=32,height=32,framerate=10/1 ! identity error-after="
    initial = "[tile]\npipeline=" + source + "90 ! appsink name=tile_output\n"
    final = "[tile]\npipeline=" + source + "10 ! appsink name=tile_output\n"
    assert len(initial) == len(final)
    first.write_text(initial)
    second.write_text(initial)
    os.link(first, target)
    original_stat = target.stat()
    log_path = root / "log"
    command = ["gst-launch-1.0", "-e", "eufytile", "name=reload", "config-file="+str(target), "!", "compositor", "force-live=true", "ignore-inactive-pads=true", "!", "video/x-raw,width=32,height=32,framerate=10/1", "!", "fakesink", "sync=false"]
    with log_path.open("w") as log:
        process = subprocess.Popen(command, stdout=log, stderr=log)
        try:
            deadline = time.monotonic()+2
            while "decoder started" not in log_path.read_text():
                assert time.monotonic()<deadline, "initial decoder did not start"
                time.sleep(0.005)
            os.link(second, root / "replace")
            (root / "replace").replace(target)
            first.write_text(final)
            # Preserve seconds while changing sub-second metadata on the reused inode.
            nanoseconds = original_stat.st_mtime_ns + (1 if original_stat.st_mtime_ns % 1_000_000_000 < 999_999_999 else -1)
            os.utime(first, ns=(original_stat.st_atime_ns, nanoseconds))
            os.link(first, root / "replace")
            (root / "replace").replace(target)
            after = target.stat()
            assert (after.st_ino,after.st_size,int(after.st_mtime)) == (original_stat.st_ino,original_stat.st_size,int(original_stat.st_mtime))
            time.sleep(2)
            assert process.poll() is None, "rapid replacement stopped compositor"
        finally:
            process.terminate()
            try:
                process.wait(timeout=4)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    output = log_path.read_text()
    print(output)
    assert output.count("eufytile reload: configuration updated") >= 2, "same-size reused-inode configuration was missed"
    assert "Failed after iterations as requested" in output, "latest rapid replacement pipeline was not applied"
    print("PASS: same-size same-second atomic inode reuse applies final configuration")
