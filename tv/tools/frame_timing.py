#!/usr/bin/env python3
"""Summarize EufyFrameTiming DEBUG logs; nanoseconds are local to each device.

Usage: frame_timing.py LOG [--pid PID] [--skip-seconds 10]
Never compare monotonic timestamps between different devices.
"""
import argparse
import collections
import json
import re

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("log")
parser.add_argument("--pid", type=int)
parser.add_argument("--skip-seconds", type=float, default=0)
args = parser.parse_args()
events = collections.defaultdict(lambda: collections.defaultdict(list))
waits = collections.defaultdict(list)
pattern = re.compile(r"(\S+) (received|decoded|rendered) ptsUs=(\d+) atNs=(\d+)(?: targetNs=(-?\d+))?")
with open(args.log) as log:
    for line in log:
        if args.pid and not re.search(rf"\s{args.pid}\s", line):
            continue
        match = pattern.search(line)
        if not match:
            continue
        camera, kind, pts, at, target = match.groups()
        events[camera][kind].append((int(pts), int(at)))
        if target and int(target) >= 0:
            waits[camera].append((int(target) - int(at)) / 1e6)


def summary(values):
    values = sorted(values)
    if not values:
        return None
    return {name: round(values[int((len(values) - 1) * q)], 3)
            for name, q in (("p50", .5), ("p95", .95), ("p99", .99), ("max", 1))}


result = {}
for camera, kinds in events.items():
    rendered = kinds["rendered"]
    if not rendered:
        continue
    start = rendered[0][1] + args.skip_seconds * 1e9
    rendered = [(pts, at) for pts, at in rendered if at >= start]
    if len(rendered) < 2:
        continue
    received = dict(kinds["received"])
    decoded = dict(kinds["decoded"])
    intervals = [(b[1] - a[1]) / 1e6 for a, b in zip(rendered, rendered[1:])]
    duration = (rendered[-1][1] - rendered[0][1]) / 1e9
    result[camera] = {
        "rendered_frames": len(rendered), "duration_seconds": round(duration, 3),
        "render_fps": round((len(rendered) - 1) / duration, 3),
        "render_interval_ms": summary(intervals),
        "decoded_to_render_ms": summary([(at - decoded[pts]) / 1e6 for pts, at in rendered if pts in decoded]),
        "received_to_render_ms": summary([(at - received[pts]) / 1e6 for pts, at in rendered if pts in received]),
        "requested_pacing_wait_ms": summary(waits[camera]),
        "received_to_render_30s_windows": []}
    for window in range(int(duration // 30) + 1):
        values = [(at - received[pts]) / 1e6 for pts, at in rendered
                  if pts in received and start + window * 30e9 <= at < start + (window + 1) * 30e9]
        if values:
            result[camera]["received_to_render_30s_windows"].append({"start_seconds": window * 30, **summary(values)})
print(json.dumps(result, indent=2))
