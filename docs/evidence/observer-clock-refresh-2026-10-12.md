# RTSP observer clock refresh must not block packet reads

The diagnostic `measure-frame-freshness.py` previously fetched `/api/clock`
synchronously on its RTSP reader every five seconds. A slow HTTP response could
hold already-arriving pictures in that observer's socket, manufacturing receipt
age. This is an observer defect, not a production-player fix.

The refresh now runs on an owned background thread. Frame reads use the latest
completed calibration; drift allowance still expands its age interval. Epoch
changes and calibration errors still fail the observation. The worker is stopped
and joined with a shutdown bound, and the RTSP connection closes normally.
Neither camera nor player buffering changes. A wide calibration interval remains
uncertain timing and must not be interpreted as measured physical capture age.

An actual loopback HTTP/RTSP regression streams stamped access units continuously
while the periodic clock request sleeps one second. Its known clock offset checks
true socket-receipt age independently of HTTP interval uncertainty:

| Exact same regression | Result |
| --- | --- |
| Unchanged base script | FAIL: maximum true receipt age **1005.911 ms** |
| Independent clock refresh | PASS: maximum true receipt age **0.423 ms**, 281 complete units |

The three focused clock/SEI/RTSP tests passed. This synthetic test establishes that
clock refresh no longer delays the diagnostic reader. It does not establish smooth
production playback or explain NIC-arrival pauses. Historical captures using the
old observer need their own clock-request and independent boundary evidence before
attributing observed age to production; independent NIC quiet intervals remain
their own evidence.
