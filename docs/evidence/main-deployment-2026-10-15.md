# Main deployment, 2026-10-15

Code revision: `b228f2214e08f3a2597fe6c5241c6e6665e3d2b0`.

## Branch cleanup

Fetched and pruned origin. Deleted remote `sync/beta04-wall`, whose tip was
reachable from `origin/main` and whose PR #12 was merged. No other remaining
remote branch was fully merged. Retained the unmerged Linux experiment branch,
open PR branches and other branches with unmerged work.

## GaragePi

The user requested main rather than the experimental arrival/admission branch.
Installed both the ARMv6 Go executable and GStreamer pacing plugin from main.
Built Go on the Mac; cross-compiled the plugin on the bridge using the existing
ARMv6 sysroot/compiler. Nothing was compiled on GaragePi. Saved the previous
binary/plugin under `/var/backups/eufy-wall/pre-main-20261015/`.

The shared checkout was switched to another branch during this work. Final Go
and Android builds therefore used an isolated worktree pinned to the code
revision above. The deployed executable reports that revision with
`vcs.modified=false`. The plugin source hash was checked against that worktree.

Installed artifacts:

| Artifact | SHA-256 |
| --- | --- |
| Go executable | `374911d69f5385ce61c6ceece7694ce2eba872e36d2eb1eb32d1c4455a4d0112` |
| GStreamer plugin | `9e9837b8e443663b850ee1840b13500dcc830cb7e8ae51746d2890520dd575bb` |

The final client restart completed at 05:49:19 EDT. All three configured feeds
used `v4l2h264dec`. Eufy decoded/display-submission observations advanced, with
initial latest submission age approximately 180 ms. Wyze encountered a fatal
stream error at 05:49:32 and was restarted at 05:49:34. It subsequently had an
active display plane. This checks the ordinary fatal-error recovery path; the
TERM-resistant teardown path is covered by the regression test, not a newly
reproduced hardware hang.

This supersedes the deployment limitation in
`linux-fatal-teardown-2026-10-15.md`: the fatal-error fix is now deployed with the
main playback pipeline by explicit user request. It does not establish a fix
for upstream Eufy feed interruptions or the earlier whole-Pi blackout.

## Bridge and Fire TVs

Bridge runtime modules and lockfile matched main by checksum. Synchronized
support files without restarting the unchanged running bridge. Installed SDK
resolution matched the lockfile:
`afcbc3fe5a66d488120dd3d29bcadd1e0eba2c77`. The verified go2rtc cache matched
the installed binary (`7f0858a30dfd8d5450b78458f1abb3dc3240f91165191646b23d65cbf785c585`).
Health reported authenticated, go2rtc running and four active bridge feeds.

Built TV 0.6.1/code 10 from the isolated main worktree. APK SHA-256:
`a93712f1d66cf7cb251fb25ca90effa58e0c34c1dfd534bff537d42129abd7b3`.
Installed with `adb install -r` on Fire TVs `.58` and `.85`, then launched the
saved walls. Both preference files remained byte-identical to their fresh
pre-install backups. Hardware-first/software fallback policy was unchanged.

## Verification

- Server: 239 tests passed.
- Linux: `make test` passed, including native pacing tests and all Go packages.
- Android: `:app:testDebugUnitTest :app:assembleDebug` passed.
- Private device logs and preferences are under `.evidence/main-deploy-2026-10-15/`.

Installation, service/process health and frame progression do not prove physical
screen smoothness or long-term stability. The interrupted earlier baseline is
not a successful 30-minute soak.
