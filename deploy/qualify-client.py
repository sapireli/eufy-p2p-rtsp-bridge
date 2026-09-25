#!/usr/bin/env python3
"""Read-only host inventory and bounded display qualification probes.

Python 3.7+ standard library only. RTSP and bridge URLs come from environment
variables and are never copied into reports or diagnostics.
"""
import argparse
import datetime
import json
import os
import platform
import re
import shutil
import signal
import subprocess
import sys
import threading
import time
import urllib.request
import urllib.parse
from pathlib import Path

FPS = re.compile(r"rendered:\s*(\d+),\s*dropped:\s*(\d+),\s*current:\s*([\d.]+),\s*average:\s*([\d.]+)")
FPS_DROPS = re.compile(r"rendered:\s*(\d+),\s*dropped:\s*(\d+),\s*fps:\s*([\d.]+),\s*drop rate:\s*([\d.]+)")
DIMENSION = re.compile(r"\b(width|height)=(?:\(int\))?(\d{2,5})\b")
SAFE_TEXT = re.compile(r"[^A-Za-z0-9 .,_()+/\-]")
ELEMENTS = ("gst-launch-1.0", "fpsdisplaysink", "kmssink", "watchdog", "v4l2h264dec", "v4l2slh265dec", "vah264dec", "vah265dec", "avdec_h264", "avdec_h265")


def safe_text(value, limit=100):
    """Keep hardware labels useful without allowing URLs, control chars, or Markdown."""
    value = re.sub(r"\b(?:rtsp|https?)://\S+", "[redacted]", str(value), flags=re.I)
    value = re.sub(r"(?i)(?:password|token|secret)\s*[=:]\s*\S+", "[redacted]", value)
    return SAFE_TEXT.sub("", value).strip()[:limit]


def read_text(path, limit=4096):
    try:
        return Path(path).read_bytes()[:limit].decode("utf-8", "replace").replace("\x00", "").strip()
    except OSError:
        return ""


def command_output(args, timeout=5):
    try:
        return subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                              timeout=timeout, check=False).stdout.decode("utf-8", "replace")
    except (OSError, subprocess.TimeoutExpired):
        return ""


def parse_planes(output):
    """Extract only numeric plane IDs from modetest's Planes table."""
    planes = []
    in_table = False
    for line in output.splitlines():
        if line.strip() == "Planes:":
            in_table = True
            continue
        if not in_table:
            continue
        if line and not line[0].isspace() and line.endswith(":"):
            break
        fields = line.split()
        if line and not line[0].isspace() and len(fields) >= 3 and all(field.isdigit() for field in fields[:3]):
            planes.append(int(fields[0]))
    return sorted(set(planes))[:64]


def classify(model, machine, os_id):
    match = re.search(r"Raspberry Pi\s+([1345])\b", model, re.I)
    if match:
        return "Raspberry Pi " + match.group(1)
    if re.search(r"Raspberry Pi Model\s+[AB]\b", model, re.I):
        return "Raspberry Pi 1"
    if machine in ("x86_64", "amd64") and os_id == "debian":
        return "Debian x86-64"
    return "other or unclassified"


def inventory():
    model = safe_text(read_text("/proc/device-tree/model") or read_text("/sys/devices/virtual/dmi/id/product_name") or "unknown")
    os_release = read_text("/etc/os-release")
    values = dict(re.findall(r'^([A-Z_]+)="?([^"\n]+)"?$', os_release, re.M))
    os_id = values.get("ID", "unknown").strip('"')
    machine = platform.machine()
    connectors = []
    for status in sorted(Path("/sys/class/drm").glob("card*-*/status")):
        if read_text(status) != "connected":
            continue
        modes = [m for m in read_text(status.with_name("modes")).splitlines() if re.fullmatch(r"\d{2,5}x\d{2,5}", m)]
        connectors.append({"name": safe_text(status.parent.name), "modes": modes[:8]})
    plugins = {}
    for element in ELEMENTS:
        if element == "gst-launch-1.0":
            plugins[element] = shutil.which(element) is not None
        else:
            plugins[element] = _element_exists(element)
    modetest = command_output(["modetest", "-p"], 5)
    return {
        "target": classify(model, machine, os_id),
        "model": model,
        "os": safe_text(values.get("PRETTY_NAME", "unknown").strip('"')),
        "kernel": safe_text(platform.release()),
        "architecture": safe_text(machine),
        "connectors": connectors[:8],
        "drm_plane_ids": parse_planes(modetest),
        "drm_planes_status": "observed" if "Planes:" in modetest else "unavailable",
        "gstreamer_elements": plugins,
    }


def _element_exists(element):
    try:
        return subprocess.run(["gst-inspect-1.0", "--exists", element], stdout=subprocess.DEVNULL,
                              stderr=subprocess.DEVNULL, timeout=3, check=False).returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def valid_url(value, scheme, origin_only=False):
    if re.search(r"[\s\x00-\x1f]", value):
        return False
    try:
        parts = urllib.parse.urlsplit(value)
        return (parts.scheme == scheme and bool(parts.hostname) and parts.username is None and
                parts.password is None and not parts.query and not parts.fragment and
                (not origin_only or parts.path in ("", "/")))
    except ValueError:
        return False


def parse_fps_line(line, metrics):
    match = FPS.search(line)
    if match:
        rendered, dropped = int(match.group(1)), int(match.group(2))
        metrics.update({"frames_rendered": rendered, "frames_dropped": dropped,
                        "current_fps": float(match.group(3)), "average_fps": float(match.group(4))})
        return
    match = FPS_DROPS.search(line)
    if match:
        metrics.update({"frames_rendered": int(match.group(1)), "frames_dropped": int(match.group(2)),
                        "current_fps": float(match.group(3)), "average_fps": None})


def parse_caps_line(line, metrics):
    lower = line.lower()
    if metrics.get("codec") is None:
        if "video/x-h265" in lower or re.search(r"encoding-name=\(string\)h265\b", lower):
            metrics["codec"] = "h265"
        elif "video/x-h264" in lower or re.search(r"encoding-name=\(string\)h264\b", lower):
            metrics["codec"] = "h264"
    if "video/x-raw" in lower or "video/x-h26" in lower:
        for key, value in DIMENSION.findall(lower):
            metrics[key] = int(value)


def process_ticks(pid):
    stat = read_text("/proc/{}/stat".format(pid))
    end = stat.rfind(")")
    if end < 0:
        return None
    fields = stat[end + 2:].split()
    try:
        return int(fields[11]) + int(fields[12])
    except (ValueError, IndexError):
        return None


def process_rss_mib(pid):
    match = re.search(r"^VmRSS:\s*(\d+)\s+kB", read_text("/proc/{}/status".format(pid)), re.M)
    return round(int(match.group(1)) / 1024, 1) if match else None


def systemctl_active():
    try:
        return subprocess.run(["systemctl", "is-active", "--quiet", "eufy-wall"], stdout=subprocess.DEVNULL,
                              stderr=subprocess.DEVNULL, timeout=3, check=False).returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def probe(url, duration, sink):
    if sink == "kmssink" and systemctl_active():
        raise ValueError("stop eufy-wall before a KMS display probe; its DRM output is in use")
    metrics = {"sink": sink, "duration_requested_s": duration, "frames_rendered": None,
               "frames_dropped": None, "current_fps": None, "average_fps": None,
               "codec": None, "width": None, "height": None,
               "cpu_percent_mean": None, "cpu_percent_peak": None, "rss_mib_peak": None,
               "progress_observed": False, "status": "not_run"}
    args = ["gst-launch-1.0", "-v", "rtspsrc", "location=" + url, "protocols=tcp", "latency=200", "!",
            "decodebin", "!", "videoconvert", "!", "fpsdisplaysink", "video-sink=" + sink,
            "text-overlay=false", "sync=true"]
    try:
        child = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                 start_new_session=True, universal_newlines=True, errors="replace")
    except OSError:
        metrics["status"] = "gstreamer_unavailable"
        return metrics
    lock = threading.Lock()

    def drain():
        for line in child.stdout:
            with lock:
                parse_fps_line(line, metrics)
                parse_caps_line(line, metrics)

    reader = threading.Thread(target=drain, daemon=True)
    reader.start()
    start = time.monotonic()
    samples = []
    rss_peak = 0.0
    previous = None
    while time.monotonic() - start < duration and child.poll() is None:
        now, ticks = time.monotonic(), process_ticks(child.pid)
        if ticks is not None and previous is not None:
            elapsed = now - previous[0]
            if elapsed > 0:
                samples.append(max(0.0, (ticks - previous[1]) / os.sysconf("SC_CLK_TCK") / elapsed * 100))
        if ticks is not None:
            previous = (now, ticks)
        rss_peak = max(rss_peak, process_rss_mib(child.pid) or 0.0)
        time.sleep(min(1.0, max(0.0, duration - (time.monotonic() - start))))
    early_exit = child.poll() is not None
    if not early_exit:
        try:
            os.killpg(child.pid, signal.SIGINT)
        except ProcessLookupError:
            pass
        try:
            child.wait(timeout=5)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            child.wait(timeout=5)
    reader.join(timeout=2)
    child.stdout.close()
    with lock:
        metrics["progress_observed"] = (metrics["frames_rendered"] or 0) > 0
    metrics["duration_observed_s"] = round(time.monotonic() - start, 1)
    metrics["cpu_percent_mean"] = round(sum(samples) / len(samples), 1) if samples else None
    metrics["cpu_percent_peak"] = round(max(samples), 1) if samples else None
    metrics["rss_mib_peak"] = rss_peak or None
    metrics["status"] = "early_exit" if early_exit else ("progress" if metrics["progress_observed"] else "no_measured_progress")
    return metrics


def bridge_ok(base):
    try:
        with urllib.request.urlopen(base.rstrip("/") + "/healthz", timeout=2) as response:
            if response.status != 200:
                return False
            return json.loads(response.read(4096)).get("ok") is True
    except (OSError, ValueError, KeyError):
        return False


def recovery_watch(base, event, duration, poll=1.0):
    """Observe a manually triggered event; never restart services or change network state."""
    start = time.monotonic()
    healthy = 0
    ready = False
    outage = None
    recovered = None
    wall_active_before = systemctl_active()
    while time.monotonic() - start < duration:
        now = time.monotonic()
        ok = bridge_ok(base)
        if not ready:
            healthy = healthy + 1 if ok else 0
            if healthy >= 3:
                ready = True
                print("Bridge baseline ready: trigger the {} now.".format(event.replace("_", " ")), file=sys.stderr)
        elif not ok and outage is None:
            outage = now
            healthy = 0
        elif outage is not None and ok:
            healthy += 1
            if healthy >= 2:
                recovered = now
                break
        elif outage is not None:
            healthy = 0
        time.sleep(min(poll, max(0.0, duration - (time.monotonic() - start))))
    return {"event": event, "baseline_ready": ready, "outage_observed": outage is not None,
            "bridge_recovery_s": round(recovered - outage, 1) if recovered is not None else None,
            "wall_active_before": wall_active_before, "wall_active_after": systemctl_active(),
            "status": "recovered" if recovered is not None else ("no_outage_observed" if ready and outage is None else "incomplete")}


def markdown(report):
    host = report["host"]
    probe_result = report.get("probe") or {}
    recovery = report.get("recovery") or {}
    connector = host["connectors"][0] if host["connectors"] else None
    output = connector["name"] + " " + (connector["modes"][0] if connector["modes"] else "mode unknown") if connector else "unknown"
    fps = probe_result.get("average_fps")
    fps_kind = "average FPS"
    if fps is None and probe_result.get("current_fps") is not None:
        fps = probe_result["current_fps"]
        fps_kind = "current FPS"
    drops = probe_result.get("frames_dropped")
    cpu = probe_result.get("cpu_percent_mean")
    measurement = "{} {}, drops {}, CPU {}%".format(fps_kind, fps if fps is not None else "unknown", drops if drops is not None else "unknown", cpu if cpu is not None else "unknown")
    recovered = recovery.get("bridge_recovery_s")
    recovery_text = "bridge {} s; visual check pending".format(recovered) if recovered is not None else "not measured"
    plane_text = str(len(host["drm_plane_ids"])) if host["drm_planes_status"] == "observed" else "unknown"
    codec_size = "single RTSP probe " + (probe_result.get("codec") or "codec unknown")
    if probe_result.get("width") and probe_result.get("height"):
        codec_size += " {}x{}".format(probe_result["width"], probe_result["height"])
    fields = [host["target"] + " / " + host["os"] + " / " + host["kernel"], output,
              codec_size, probe_result.get("sink", "none") + "; planes " + plane_text,
              measurement, "unverified; " + recovery_text]
    return "| " + " | ".join(safe_text(field, 180) for field in fields) + " |"


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, help="sanitized JSON report path")
    parser.add_argument("--duration", type=int, default=60, help="RTSP probe seconds, 5..1800")
    parser.add_argument("--sink", choices=("fakesink", "kmssink"), default="fakesink")
    parser.add_argument("--recovery-event", choices=("none", "bridge_restart", "network_interruption"), default="none")
    parser.add_argument("--recovery-duration", type=int, default=120, help="manual recovery watch seconds, 15..600")
    args = parser.parse_args(argv)
    if not 5 <= args.duration <= 1800 or not 15 <= args.recovery_duration <= 600:
        parser.error("duration must be 5..1800 and recovery duration 15..600 seconds")
    rtsp = os.environ.get("QUALIFY_RTSP_URL", "")
    if not valid_url(rtsp, "rtsp"):
        parser.error("set QUALIFY_RTSP_URL to a LAN rtsp:// URL without credentials, query, or whitespace")
    bridge = os.environ.get("QUALIFY_BRIDGE_URL", "")
    if args.recovery_event != "none" and not valid_url(bridge, "http", origin_only=True):
        parser.error("set QUALIFY_BRIDGE_URL to a LAN http:// origin without credentials for recovery observation")
    report = {"schema_version": 1, "collected_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "qualification": "unverified; manual display and camera/station gates remain",
              "host": inventory(), "probe": probe(rtsp, args.duration, args.sink), "recovery": None}
    if args.recovery_event != "none":
        report["recovery"] = recovery_watch(bridge, args.recovery_event, args.recovery_duration)
    destination = Path(args.output)
    destination.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(markdown(report))
    print("Sanitized report written to {}".format(destination), file=sys.stderr)
    return 0 if report["probe"]["status"] == "progress" else 1


if __name__ == "__main__":
    sys.exit(main())
