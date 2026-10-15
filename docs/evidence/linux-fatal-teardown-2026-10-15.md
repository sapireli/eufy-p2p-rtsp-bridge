# Linux fatal pipeline teardown, 2026-10-15

## Observed failure

GaragePi remained reachable with client PID 571 and the same boot ID while
individual Eufy pipelines failed. At 05:16:02 EDT, Front Door pipeline PID 1694
reported fatal GStreamer watchdog errors. At 05:18:04 it was still alive, sleeping
in `futex_do_wait`, with no new frame observations. At 05:18:37 it had been alive
for 3m33s. The supervisor waited only for process exit, so it could not restart
this failed pipeline. Wyze continued through a separate pipeline.

The bridge independently reported no bytes for Garage and Front Door at
09:15:36–38 UTC, followed by first-frame timeouts. This establishes a separate
interruption at the bridge feed boundary. It does not identify a camera,
HomeBase, SDK or network root cause. Earlier RTSP writes to the Pi timed out at
09:14:09–11 UTC; those precede the bridge feed restart and must not be treated
as the same proven failure.

## Change and tests

The supervisor recognizes gst-launch's `ERROR:` output, allows five seconds for
normal teardown, then sends TERM. After a further five seconds without exit it
sends KILL and reaps the process before the existing backoff/restart path.
Warnings do not trigger this behavior. Playback configuration is unchanged.

The regression test runs a process that prints a fatal pipeline error, ignores
TERM and sleeps. It verifies that the supervisor starts a second process. A
separate test verifies a warning does not restart a running process. All Linux
Go package tests passed. The ARMv6 binary was cross-compiled on the Mac.

## Deployment limitation and rollback

The test binary was installed at 05:19:44 EDT. Its generated pipeline differed
from the previous deployed binary: the previous binary used `eufyarrival`,
`eufyadmission` and arrival-clock pacing, absent from this checkout. This invalidates
an isolated live comparison. The previous binary was atomically restored and
restarted at approximately 05:20:39. Its SHA-256 matches the pre-change backup:

`5420f3e8f8d2ec2d66584f2f580f9bd404a07f0e52233ece5b98f796d25f6971`

The recovery change is tested in source but is **not deployed**. Matching the
previous binary's source/pipeline is required before a valid deployment. Neither
the Eufy source interruption nor the earlier whole-Pi blackout is fixed by this
record. The interrupted baseline is not a successful 30-minute soak.

Raw local records are under `.evidence/garagepi-blackout-2026-10-15/`, including
bridge and Pi interruption journals, health samples and the initial streamed log.
