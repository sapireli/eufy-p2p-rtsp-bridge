# Matched five-second driver warning and CPU census

## Prepared diagnostic, no trial result

`client/tools/cpu_census_probe.c` extends the existing native CPU census with a
concurrent, nonblocking `/dev/kmsg` reader. It seeks to the current end before
sampling, counts records in memory, and never prints or persists message text,
stack lines, addresses or packet contents. Core dumps are disabled by the helper.
It counts only a first-line `WARNING:` headline containing
`brcmf_fws_rxreorder`; function names later in a stack or record properties do not
count as another warning event.

The warning window begins at the reported `seekMonoNs`, which also becomes the
CPU census `startMonoNs` when kmsg is available. Both stop at the reported end
monotonic timestamp. Endpoint `/proc` scans are not instantaneous; their begin
and elapsed times remain explicit. Reads continue during the nominal five-second
window, with a bounded final drain after the endpoint snapshot. Observer CPU
includes setup and scans, making its reported percentage conservative relative
to the sample window. Reading messages cannot generate a claim of neutral timing:
accept a causal performance comparison only if measured observer CPU is at most
2% of one core and document kernel-side diagnostic overhead as unbounded.

Output distinguishes `available=0` from zero warnings. `complete=0` means open/read
errors, `EPIPE` overruns, sequence gaps, malformed records or drain cap hits were
observed. `epipeEvents` and `sequenceMissingRecords` overlap and must not be added
as independent lost-record counts. Loss before the first observed sequence cannot
be counted exactly. The drain cap protects observer cost, not completeness.
No zero-warning conclusion is valid when the counter is unavailable or incomplete.

Process identities still match PID and start tick at both endpoints. Completely
transient processes may be missed. Global IRQ/softIRQ counters overlap process
system CPU; do not add them to per-process percentages as another workload bucket.
WLAN byte deltas reflect the window, not a last-reported PHY link rate.

## Matched A/B use

The root deployment owner must first run the explicitly protected `stock-trial`
control and then the protected candidate trial, each beginning with a fresh driver
state. Use the same native helper, source quality, camera selection, geometry,
power, service policy and comparable post-recovery settling. Capture short
five-second warning/CPU windows together with the already available low-rate
frame age/cadence statistics. A warning-free candidate reload by itself cannot
separate the patch effect from reset of an already damaged stock reorder ring.

Do not parse/export large journal stacks or add per-frame observers during this
comparison. Compiler activity belongs outside the sample and is a confound if
it overlaps. This helper performs no module, network, power or service change;
the trial and recovery worker remain independently controlled by the parent.

## Verification

Local build with `cc -O2 -Wall -Wextra -Werror` and `--self-test` passes. Pure
regressions cover process names containing parentheses, valid warning headlines,
stack/property/other-function exclusion, malformed records, sequence gaps,
duplicate/reordered sequences and unknown first sequence baseline. Live kmsg
availability, counter completeness, Pi overhead and stock/candidate effects are
not established by these parser tests. Native Pi compilation/self-test is the
next authorized staging step; no actual census or trial is run by preparing this
source.
