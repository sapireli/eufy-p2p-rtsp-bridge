# Settings cutover completed: October22,2026

This completes the bridge configuration transition. The earlier proposed release wait was ended following the instruction to finish the work. No release tags or published releases were fabricated. The breaking change is documented in [the Unreleased note](../releases/2026-10-22-settings-cutover.md).

## Requirement audit

| Requirement | Verified result |
|---|---|
| Import existing settings | Explicit offline migration imports once, including old operating environment values, external streams and saved dual views. Existing stores and later client edits take precedence. |
| Authoritative persistent store | Production `loadConfig` reads only bootstrap options plus the validated JSON store. The store constructor has no YAML, environment or dual-view-file import. |
| Legacy warning and removal | The transition emitted deprecation warnings. Current startup rejects legacy operating YAML and all four retired environment overrides with migration instructions. |
| Bootstrap options only outside clients | Live `/etc/eufy-wall-bridge.yaml` now contains only `host`. Login credentials and essential paths/listeners remain startup inputs. |
| Both clients manage settings | Android and Linux controls were deployed and paired during the transition. API contract and paired token are unchanged. An actual Linux client settings read succeeded against the final bridge at revision4. |
| Backup/restore | Live authorized export/import preserved settings exactly, stale writes returned409, and offline recovery restored the same export into missing and corrupt store copies. |
| Recovery integrity | An initialization marker prevents silent recreation of missing settings. Explicit offline restore preserves service ownership, writes mode0600 and requires `--replace` plus a private rollback copy for an existing store. |

## Live cutover

The candidate first ran against private copies of the actual bootstrap and revision3 store. Legacy startup was rejected before migration; migration retired five operating sections; the effective configuration and store bytes remained identical.

The live bridge was stopped once for the cutover. Private source, configuration, environment, store and pairing-token rollback copies were saved under `/var/lib/eufy-wall-bridge/settings-cutover-20261022/rollback/`. The explicit importer retained revision3, removed `cameras`, `lan`, `go2rtc`, `freshness` and `stall` from the YAML, and retained their values in the store. The initialization marker was assigned to the service user.

At02:15:07UTC, actual production code rejected the old YAML and a retired operating environment override. Settings GET returned200 and unauthorized GET401. Effective settings, store bytes and the pairing token matched their pre-cutover values. Authentication wasOK and go2rtc wasrunning. Store and marker modes were0600.

At02:16:12UTC, live export/import returned200, preserved the entire settings payload and advanced revision3→4. A stale revision3 write returned409. PID237414 remained unchanged. The private export file had mode0600.

At02:31:14UTC, PID237414 was still active, settings GET returned200/revision4/applied, authentication wasOK and go2rtc wasrunning. The bootstrap key list was exactly `["host"]`. This establishes backend continuity; it is not a camera rendering or capture-to-screen latency measurement. Earlier intermittent camera-media failures are not claimed fixed by this configuration work.

## Offline recovery and installer safety

On the bridge, the actual final export was restored into an isolated initialized store after its file was deleted. A normal constructor correctly refused the missing store, and explicit restore recovered identical settings. A corrupt store required `--replace`; its exact corrupt bytes were saved in a0600 backup. Restored store mode was0600, and a separate test verified ownership matched the live service account. The live store remained revision4 throughout these recovery-copy tests.

The installer now stops the service before package/source updates, preserves private rollback files and performs explicit migration before startup. It preserves untracked source inputs and runs migration from the former service working directory. A regression test demonstrates that the former `rsync --delete` removed a private relative source file, while the new update preserves and successfully imports it. No changes to the bridge SDK or media/decoder policy were needed.

## Validation and provenance

- Standard `npm test`:325 passing,0 failed,0 skipped,7.33s. Log: `/tmp/eufy-settings-cutover-tests-complete.log`.
- Installer shell syntax passed. Literal environment-cleanup fixtures preserve credentials, multiline quoted content, bootstrap port overrides and permissions.
- A test-only clock fixture was corrected to use absolute50fps deadlines. Relative sleeps accumulated OS scheduling delay and generated238–245 frames; deadline pacing generated310 with0.388ms maximum true receipt age during the same1000ms clock stall. The original count and latency assertions remain unchanged. No production pacing or decoder code changed.
- Thirteen deployed server/tool files matched local committed bytes: [source manifest](data/settings-cutover-20261022/source-manifest.json).
- [Preflight result](data/settings-cutover-20261022/preflight-result.json), [live cutover](data/settings-cutover-20261022/cutover-result.json), [live export/import](data/settings-cutover-20261022/backup-restore-result.json), [offline recovery](data/settings-cutover-20261022/offline-recovery-result.json).

Implementation commits: `4db4223` (strict runtime and explicit migration), `4e1ede7` (installer and documentation), `af5342b` (offline recovery and private source preservation), `e150f9b` (test fixture pacing). Branch: `fix/balcony-decoder-recovery`.
