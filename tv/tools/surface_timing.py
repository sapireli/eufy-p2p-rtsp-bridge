#!/usr/bin/env python3
"""Match overlapping SurfaceFlinger latency snapshots to exact decoder release targets.

Snapshots are a JSON list of {layer, data}; data is unmodified --latency output.
Example: surface_timing.py timing.log snapshots.json --layer-camera 0=DOOR_SN
Capture current layer names with dumpsys SurfaceFlinger --list after each relaunch.
All times are device-local; this does not measure glass-to-glass latency.
"""
import argparse
import collections
import json
import re

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("log")
parser.add_argument("snapshots")
parser.add_argument("--layer-camera", action="append", required=True, metavar="LAYER=SERIAL")
parser.add_argument("--pid", type=int)
args = parser.parse_args()
mapping = dict(value.split("=", 1) for value in args.layer_camera)
decoded = collections.defaultdict(dict)
received = collections.defaultdict(dict)
callbacks = collections.defaultdict(dict)
pattern = re.compile(r"(\S+) (received|decoded|rendered) ptsUs=(\d+) atNs=(\d+)(?: targetNs=(-?\d+))?")
with open(args.log) as log:
    for line in log:
        if args.pid and not re.search(rf"\s{args.pid}\s", line):
            continue
        match = pattern.search(line)
        if not match:
            continue
        camera, kind, pts, at, target = match.groups()
        pts, at = int(pts), int(at)
        if kind == "received":
            received[camera][pts] = at
        elif kind == "rendered":
            callbacks[camera][pts] = at
        elif target and int(target) >= 0:
            decoded[camera][int(target)] = (pts, at)

rows = collections.defaultdict(set)
with open(args.snapshots) as snapshots:
    for snapshot in json.load(snapshots):
        for line in snapshot["data"].splitlines()[1:]:
            values = line.split()
            if len(values) == 3:
                desired, actual, ready = map(int, values)
                if 0 < actual < 2 ** 62:
                    rows[str(snapshot["layer"])].add((desired, actual, ready))


def quantiles(values):
    values = sorted(values)
    return {key: round(values[int((len(values) - 1) * q)], 3)
            for key, q in (("p50", .5), ("p95", .95), ("p99", .99), ("max", 1))} if values else None


result = {}
for layer, camera in mapping.items():
    ordered = sorted(rows[layer], key=lambda row: row[1])
    matched = [(desired, actual, *decoded[camera][desired])
               for desired, actual, _ in ordered if desired in decoded[camera]]
    if len(matched) < 2:
        result[camera] = {"error": "fewer than two exact target matches", "surface_rows": len(ordered)}
        continue
    span = (matched[-1][1] - matched[0][1]) / 1e9
    result[camera] = {
        "surface_rows": len(ordered), "exact_target_matches": len(matched),
        "span_seconds": round(span, 3), "actual_present_fps": round((len(matched) - 1) / span, 3),
        "actual_present_interval_ms": quantiles([(b[1] - a[1]) / 1e6 for a, b in zip(matched, matched[1:])]),
        "target_to_present_ms": quantiles([(actual - desired) / 1e6 for desired, actual, _, _ in matched]),
        "decoded_to_present_ms": quantiles([(actual - at) / 1e6 for _, actual, _, at in matched]),
        "received_to_present_ms": quantiles([(actual - received[camera][pts]) / 1e6
                                             for _, actual, pts, _ in matched if pts in received[camera]]),
        "callback_skew_ms": quantiles([(callbacks[camera][pts] - actual) / 1e6
                                       for _, actual, pts, _ in matched if pts in callbacks[camera]])}
print(json.dumps(result, indent=2))
