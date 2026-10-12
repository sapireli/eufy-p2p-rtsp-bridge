#!/usr/bin/env python3
"""Verify actual GStreamer report identity/bounds after a 50 ms sink delay.
Run with GST_PLUGIN_PATH set and the compiled freshness_probe executable.
"""
import re
import subprocess
import sys

run = subprocess.run([sys.argv[1], "--last-age"], check=True, capture_output=True,
                     text=True, timeout=20)
lines = [line for line in run.stderr.splitlines() if line.startswith("eufy-observe ")]
assert len(lines) == 1, run.stderr
fields = dict(re.findall(r"(\w+)=([^ ]+)", lines[0]))
for scope in ("Decoded", "Kms"):
    assert fields[f"last{scope}Valid"] == "1", fields
    assert fields[f"last{scope}Epoch"] == "65706f6368313233", fields
    assert fields[f"last{scope}FrameId"] == "2", fields
    lower = int(fields[f"last{scope}AgeLowerNs"])
    upper = int(fields[f"last{scope}AgeUpperNs"])
    assert 600_000_000 <= lower <= upper, fields  # deliberately stale source frame
    assert upper - lower < 10_000_000, fields
elapsed = int(fields["lastKmsMonoNs"]) - int(fields["lastDecodedMonoNs"])
assert 40_000_000 <= elapsed <= 250_000_000, fields
for bound in ("Lower", "Upper"):
    increase = int(fields[f"lastKmsAge{bound}Ns"]) - int(fields[f"lastDecodedAge{bound}Ns"])
    assert abs(increase - elapsed) <= 1_000_000, fields
print("exact last-age report: same stamped picture, downstream delay reflected in both bounds")
