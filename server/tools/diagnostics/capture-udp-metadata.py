#!/usr/bin/env python3
"""Bounded native metadata capture; export artifacts before evaluating its exit status.
Run on the observer host. This never exports packet snapshots, only the observer's
fixed metadata output, stderr statistics, stdout and a separate exit/manifest record.
"""
import argparse
import datetime
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time


def private_write(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(data)


def capture(binary, interface, peer, seconds, output, *, video_header=False, trigger_port=None, runner=subprocess.run):
    if not Path(binary).is_absolute() or not re.fullmatch(r"[A-Za-z0-9_.:-]{1,15}", interface):
        raise ValueError("absolute native binary path and interface name required")
    ipaddress.IPv4Address(peer)
    if not isinstance(seconds, int) or not 1 <= seconds <= 45:
        raise ValueError("capture seconds must be 1..45")
    if trigger_port is not None and not 1 <= trigger_port <= 65535:
        raise ValueError("trigger port outside range")
    output = Path(output)
    output.mkdir(mode=0o700)  # Existing destinations are never overwritten.
    stage = Path(tempfile.mkdtemp(prefix=".capture-", dir=output))
    metadata = stage / "capture.metadata"
    args = [str(binary), interface, peer, str(seconds), str(metadata)]
    if video_header:
        args.append("--video-header")
    if trigger_port is not None:
        args += ["--trigger-port", str(trigger_port)]
    started = time.time()
    returncode, timed_out, execution_error = None, False, None
    with os.fdopen(os.open(stage / "capture.stdout", os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as stdout, os.fdopen(os.open(stage / "capture.stats", os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as stderr:
        try:
            result = runner(args, stdout=stdout, stderr=stderr, timeout=seconds + 5, check=False, umask=0o077)
            returncode = result.returncode
        except subprocess.TimeoutExpired:
            returncode, timed_out = 124, True
        except OSError as error:
            returncode, execution_error = 126, type(error).__name__
    stats = None
    for line in (stage / "capture.stats").read_text(errors="replace").splitlines():
        try:
            value = json.loads(line)
            if isinstance(value, dict):
                stats = value
        except json.JSONDecodeError:
            pass
    record_bytes = 56 if video_header else 40
    metadata_bytes = metadata.stat().st_size if metadata.exists() else None
    strict_counts = ("packetSocketDrops", "missingTimestamp", "truncations", "dataTruncations", "controlTruncations", "ioError")
    valid = returncode == 0 and isinstance(stats, dict) and all(stats.get(k) == 0 for k in strict_counts)
    valid = bool(valid and stats.get("recordBytes") == record_bytes and metadata_bytes is not None and metadata_bytes % record_bytes == 0 and metadata_bytes // record_bytes == stats.get("records"))
    manifest = {"startedUTC": datetime.datetime.fromtimestamp(started, datetime.timezone.utc).isoformat(),
                "elapsedSeconds": time.time() - started, "requestedSeconds": seconds,
                "nativeExitStatus": returncode, "timedOut": timed_out, "executionError": execution_error,
                "captureValid": valid, "recordBytes": record_bytes, "metadataBytes": metadata_bytes,
                "stats": stats, "exportComplete": False}
    # Export every available artifact even when native exit is7, statistics are absent,
    # or the fixed metadata stream ends mid-record. Such captures remain INVALID.
    try:
        for name in ("capture.metadata", "capture.stats", "capture.stdout"):
            source = stage / name
            if source.exists():
                private_write(output / name, source.read_bytes())
        private_write(output / "capture.exit", (str(returncode) + "\n").encode())
        manifest["metadataSHA256"] = hashlib.sha256(metadata.read_bytes()).hexdigest() if metadata.exists() else None
        manifest["exportComplete"] = True
    except OSError as error:
        manifest["exportError"] = type(error).__name__
        manifest["retainedStage"] = str(stage)
    private_write(output / "manifest.json", (json.dumps(manifest, indent=2) + "\n").encode())
    if manifest["exportComplete"]:
        shutil.rmtree(stage)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary"); parser.add_argument("interface"); parser.add_argument("peer")
    parser.add_argument("seconds", type=int); parser.add_argument("output", type=Path)
    parser.add_argument("--video-header", action="store_true"); parser.add_argument("--trigger-port", type=int)
    args = parser.parse_args()
    manifest = capture(args.binary, args.interface, args.peer, args.seconds, args.output,
                       video_header=args.video_header, trigger_port=args.trigger_port)
    print(json.dumps({"manifest": str(args.output / "manifest.json"), "captureValid": manifest["captureValid"],
                      "exportComplete": manifest["exportComplete"], "nativeExitStatus": manifest["nativeExitStatus"]}))
    if not manifest["exportComplete"]:
        return 74
    return manifest["nativeExitStatus"] or (0 if manifest["captureValid"] else 7)


if __name__ == "__main__":
    raise SystemExit(main())
