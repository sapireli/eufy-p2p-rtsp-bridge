# GaragePi TV and camera-client power

`scripts/tv-power.sh` is deployed as `/home/dietpi/tv-power.sh`. It keeps the
controller and its background workers in one file. It requires libCEC, GNU
`timeout`, `flock`, systemd, and passwordless sudo for the existing `dietpi`
account. Root invocations switch to `dietpi` so every caller uses the same locks.

## Reported state

`state` prints exactly `on` only when the TV reports CEC `on` and
`eufy-wall.service` is active/running with a live MainPID whose executable is
`/usr/local/bin/eufy-wall`. An executable marked `(deleted)` after an atomic
binary update remains valid until the running service exits.

It prints exactly `off` only when the TV reports CEC `standby`, the service is
inactive/failed with no MainPID, no matching deployed client remains, and any
remaining service cgroup has no tasks, including child pipeline processes.
The deployed role must match the executable path, `wall` account and
`-config /etc/eufy-wall.yaml`; unrelated experimental binaries are ignored.

Unknown CEC replies, CEC transitions, service transitions, adapter contention,
and mismatches return nonzero with no stdout. Homebridge uses exact `on`
matching and treats any other successful output as off, so an unknown state
must never be a successful response.

## Reconciliation

An explicit `on` or `off` queues a transient systemd worker and returns promptly.
The latest explicit request takes priority until the TV and client both match
it. A worker checks its unique request generation before changing state. A later
request supersedes older workers, including requests for the same target state.

Without a pending explicit request, the physical TV is authoritative:

| Observed TV | Client | Action |
|---|---|---|
| On | Off/crashed/transitioning | Queue client start; report unknown until verified |
| Standby | Running/transitioning | Queue client stop; report unknown until verified |
| Unknown/transitional | Any | Preserve client state; report unknown |
| On or standby | Matches TV | Report verified state |

Polling deduplicates workers under a shared lock. Workers keep retrying CEC
and rechecking the service, and only complete after both states match. A dead
worker's pending intent is discarded by the next poll. An invalid process in an
active service triggers a restart; legitimate service transitions are allowed
to finish. Off workers stop the service, then send TERM and finally KILL on
successive retries to remaining service tasks and verified deployed-role
processes. An on request first removes any detached deployed client before
starting a managed replacement, so old DRM ownership cannot create duplicates.
CEC commands run outside the intent lock so a newer request can supersede a
worker while its CEC call is in progress.

Explicit requests still attempt CEC commands when the TV does not
answer its initial power query. Unknown state polls never create an intent.

CEC calls are bounded
to eight seconds plus a one-second kill grace; state polls never wait for the
actuator loop and wait at most one second for its control lock. An outer timeout
bounds the entire state command to 12 seconds plus a one-second kill grace,
including a hung systemd or sudo command. The current
Homebridge state-command timeout is 59,972 ms with a 30-second polling interval.

This checks process and power state. A running process can still render stale
video. The existing freeze watchdog does not detect a queue that keeps
delivering old frames, so power-state parity does not establish stream freshness.

Queue events are logged with the `tv-power` journal tag and an origin of
`explicit` or `state-reconcile`, plus target and worker unit. These distinguish
an on/off invocation from a state poll choosing the physical TV as authority.
Pending-intent removal also logs the worker state and queued job ID.

A queued systemd job retains pending intent even if its worker is still
inactive. `systemd-run --no-block` only enqueues startup; inactive alone does
not prove that the request is dead. See the official
[systemd-run contract](https://github.com/systemd/systemd/blob/main/man/systemd-run.xml)
and [Unit Job property](https://github.com/systemd/systemd/blob/main/man/org.freedesktop.systemd1.xml).
This corrects an independently identified source race; it is not proof that the
race caused the competing requests during the live trial below.

A separate harmless transient-unit proof on the bridge reproduced this exact
systemd condition: a command queued behind a three-second oneshot dependency
reported `ActiveState=inactive` with `Job=281` before execution. Both proof units
finished and were collected. No camera service or TV state was changed.

## Verification

On Linux, run `python3 scripts/test-tv-power.py`. The suite substitutes fake CEC,
systemd and process inspection commands. It covers parity, service crashes,
pending intent, transitional/unknown replies, orphaned client processes, dead
workers, latest-request precedence, stale-worker generations, worker completion,
concurrent polls and hung CEC/systemd calls. Worker tests also verify convergence
after an invalid MainPID, an initially unresponsive TV, and remaining client and
pipeline processes. It makes no real power or service changes.

### GaragePi trial and rollback, 2026-10-17

The 20-case Linux suite passed in 67.032 seconds. The final orphan-before-start
and busy-CEC supersession regressions passed in a separate 12.013-second run.
The candidate used during that live trial had SHA-256
`c7a663e24056c85292be2f3c14b3f9d63bc9fa6b9de112debfa73f33b1eda570`.

After initial deployment, a live state read returned exact `off` with exit zero and no
stderr in 5.214 seconds. The service was inactive, MainPID was zero, and no
`eufy-wall` process remained. The previous script is preserved at
`/var/backups/eufy-wall/tv-power-20261017/tv-power-93fcdd0c2039f5c5430e03d03927beb70d3b3b761776e5d0868861a05f6cd359.sh`.

A subsequent live ON/mismatch/OFF trial did not establish reliable convergence.
ON was accepted in 0.794 seconds; the service and three decoder pipelines ran,
but concurrent CEC polls often returned unknown or converging. The journal
records a worker verifying ON at 13:56:55, competing OFF/ON/OFF requests between
13:57:11 and 13:58:04, and a worker verifying OFF at 13:58:35. The journal does
not establish whether the competing requests were explicit Homebridge writes
or automatic reconciliation. A root-driver OFF request returned in 1.170
seconds but another ON worker subsequently restarted the service.

The candidate was rolled back to the original script above. The original
deployed SHA-256 is
`93fcdd0c2039f5c5430e03d03927beb70d3b3b761776e5d0868861a05f6cd359`.
The candidate is also preserved at
`/var/backups/eufy-wall/tv-power-20261017/tv-power-candidate-c7a663e.sh`.
At that point the live parity fix remained unverified. Original state polling
only read CEC and could not restart the client during separate decoder tests.

After the queued-job guard and origin logs were added, all 24 Linux mock tests
passed in 87.761 seconds. The revised offline candidate SHA-256 is
`5bd94d09f7977acbacf25a71e16a130f72d621ed5dfbb3c39a3528debd820e90`.

### Revised deployment and live parity verification

The revised script above was redeployed on 2026-10-17 and retained after this
bounded live test:

| Step | Observed result |
|---|---|
| Explicit ON | Accepted in 2.544 seconds while the existing three-stream client ran |
| Deliberate mismatch | Client stopped; a separate CEC query confirmed TV ON in 4.181 seconds |
| State-driven recovery | First state poll returned nonzero with empty stdout and queued one `state-reconcile` ON worker |
| Recovered parity | Exact `on`, exit zero, active/running service with new MainPID 8310 after 18.073 seconds |
| Explicit OFF | Accepted in 2.401 seconds |
| Final parity | Exact `off`, exit zero after 28.123 seconds; final CEC read took 5.178 seconds |
| Process cleanup | Service inactive/dead, MainPID zero, empty ControlGroup, no `eufy-wall` or `gst-launch-1.0` processes |

Journal origins matched the test: explicit ON at 14:25:00, state-reconcile ON at
14:26:37, and explicit OFF at 14:27:06. No competing opposite request appeared
in this trial. Polls during adapter contention returned nonzero with no stdout.
The source and deployed SHA-256 both match `5bd94d09f7977acbacf25a71e16a130f72d621ed5dfbb3c39a3528debd820e90`.

The TV and client were left OFF, restoring their baseline. No boot configuration
or automatic service enablement changed. This establishes power/process parity
and recovery for the tested path, not video freshness or three-stream capacity.
