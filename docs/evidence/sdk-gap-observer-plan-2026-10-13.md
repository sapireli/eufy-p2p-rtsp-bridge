# One bounded SDK gap observation

This is a bridge-only diagnostic, not an SDK behavior change or a playback fix.
The installed SDK was read as package 0.5.0, lock commit
`d711f98e8f584d14e8685a322cb4784abddc7b01`, bundle SHA-256
`d5db321bbd4ce03397dc0d2b8bd92de3603b5edc3f71bfe0be63b5a464835c92`.
Its `videoGap` is session-wide and gates each listener until its own keyframe.
The existing two-stream unit test proves that mechanism; it does not prove the
natural paired Garage timeout was caused by it. Per-camera dedicated sessions
also mean runtime object sharing must be observed rather than assumed.

Opt in with an absolute `EWB_GAP_DIAGNOSTIC_FILE` and
`EWB_GAP_DIAGNOSTIC_SECONDS=300` (integer 1–300). The default does no work.
The root will review, stage with rollback protection, and own the live run. No
runtime observation or deployment was performed while preparing this module.
Stop after one paired natural timeout or the automatic 300-second limit; do not
repeat quiet windows as evidence of a fix.

The module writes an exclusive private 0600 JSONL file, at most 1200 rows, using
one-second counter snapshots and sparse video-gap events. It inventories actual
stream/session object equality against the public session map, recording opaque
station/session identifiers and base/dedicated-media route kinds. Camera aliases
are linked only by exact SDK receipt sourceId against existing bridge slots;
unmatched sources stay opaque. It never invokes session resolution/opening,
creates a media consumer, retains frame payloads, or enables full DEBUG logging.
Unsupported D5 private shapes disable the diagnostic without changing playback.

Boundaries observed together:

- Session `data` events for command 1300 and known media channel; these count
  reassembled P2P commands, not necessarily complete access units.
- Original assembler results, with exact original receiver, arguments, return
  object and exceptions preserved. `gatedCompleteUnits` is a prediction using the
  same awaiting-keyframe branch: a keyframe in a multi-unit result resumes virtual
  gating before subsequent P frames. The starting flag is sampled after the
  original push, because its incomplete-unit discard callback can set the gate. It is not independent decoder proof.
- SDK delivered video/keyframes and post-gap waiting flags. A diagnostic gap
  listener is moved behind newly registered SDK handlers without changing their
  relative order; inactive old stream generations are excluded from fanout.
- Existing SDK receipt sequence/monotonic/frameId and bridge consumer input/
  encoded-HTTP output counters. SDK sequence includes audio as well as video.
  HTTP submission is not RTSP socket delivery or physical presentation.

Decision for one matched event: continuing completed units with increasing gated
counts and no delivered SDK video supports a keyframe-gating hold; no completed
units supports an upstream boundary pause; continuing SDK delivery with stopped
encoded output implicates encoding/HTTP submission; continuing encoded output
with both RTSP readers timing out places the pause further downstream. No SDK
change is justified without the actual event and aligned identities/counters.

Twelve deterministic tests pass: default no-op, absent-shape rejection, opaque
inventory and exact source linking, multi-unit gating, borrowed receiver/return/
exception preservation, metadata-only counts, automatic cleanup, row cap,
third-party replacement preservation, late stream listener ordering, and write
failure cleanup. Timers unhook listeners and restore only owned wrappers. Live
observer overhead, complete event coverage and the natural fault remain unverified.
