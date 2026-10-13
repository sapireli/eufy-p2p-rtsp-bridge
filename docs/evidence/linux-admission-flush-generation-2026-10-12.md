# Async decoded admission: prevent old flush results poisoning a new stream

## Scope and source boundary

This is a narrowly verified correction to a **held, unreleased admission prototype**.
The complete two-slot admission/pacing prototype is not committed by this change
and is not claimed as hardware-validated. Its recorded pre-fix header SHA-256 is
`e1dc9a979329060e6771445f9fa57a004b3aac1fc18e7c8f6a2bd92849120764`.
The fixed held header SHA-256 is
`829755436f5423c096f649ed5f2d45f2fd7213eaf91659ccd1e014ea0d068a1a`.
Both exact headers are preserved privately with the native fixture logs.

## Reproduced defect and correction

An asynchronous output buffer is already inside a downstream push when a flush
starts. The old FLUSH_STOP handler sets its local flushing flag false and flow OK
before its serialized downstream event finishes. The old push can return
GST_FLOW_FLUSHING afterward; the worker then overwrites the resumed generation's
flow with that old return. Subsequent incoming buffers are rejected as flushing.
This is a lifecycle correctness defect, independent of CPU, Wi-Fi or decoder speed.

[GStreamer's event design](https://gstreamer.freedesktop.org/documentation/additional/design/events.html#flush_startstop)
distinguishes the asynchronous unblock stage from serialized flush completion and
requires pending EOS state to be cleared. The focused fix:

- Advances a generation counter on flush transitions and accepts a worker's flow
  return only for the generation that selected its buffer.
- Tracks a forwarded EOS as busy serialized work, just like a forwarded buffer.
- Keeps FLUSH_STOP waiting for old work to finish while the pad remains flushing;
  FLUSH_START has already been forwarded to unblock that work.

Waiting releases the queue mutex through the condition variable. No downstream
GstPad call occurs while that mutex is held, avoiding a new pad/queue lock inversion.
The correction does not change the two queued slots, encoded-reference delivery,
frame selection, pacing allowance, decoder selection or pixel ownership.

## Actual native integration evidence

`client/gst/admission_flush_generation_probe.c` compiles the actual header/type and
uses real GstPad event and buffer scheduling with a controlled synthetic sink.
It sends four-byte raw buffers and no camera, decoder, GPU or network media input.
The same fixture was compiled against both recorded headers with
`cc -std=c99 -O2 -Wall -Wextra -Werror` and run at nice priority 19.

| Header | Exit | Local state when old push is busy | Flow after flush-stop |
| --- | --- | --- | --- |
| Pre-fix | 1 | prematurely reopened | GST_FLOW_FLUSHING (-2) |
| Fixed | 0 | remains flushing | GST_FLOW_OK (0) |

The fixed case also blocks an EOS downstream, runs another flush pair, releases
that old EOS, then accepts a new segment and picture. It records one EOS event,
two completed flush-stop events and two resumed buffers, with no early reopening.
Thus the fixture covers actual old-flow publication and the EOS completion barrier;
it is not a complete proof of every possible downstream lifecycle implementation.
Standalone pacing-clock tests and the Go pipeline tests passed separately during
the read-only audit; they do not substitute for a physical decoder/display gate.

Native runtime/development packages were extracted privately into
`/opt/ewb-gst-audit/root` on Debian 13 x86, with **no system package installation**.
GStreamer runtime, development headers and tools were exactly `1.26.2-2`.
GLib headers/runtime were `2.84.4-3~deb13u5`; pkgconf was `1.8.1-4`.
The private manifest records all 21 extracted package versions and SHA-256 values.
Relevant exact package hashes:

- libgstreamer1.0-0: `08b463307fbde5496a346b1bb300b465cd22bd326f6f4634f482bf441af28a9b`
- libgstreamer1.0-dev: `40fbcf46c24fa453c96134bb868b1ad664c7815c0cb512cc0ba3c949371f1296`
- gstreamer1.0-tools: `981f85b45b550f54c0a64434b68ba3e271ef8effb342483f18c1e5e2008e511a`
- Package manifest: `27f866642b39e10aa2d2cb7934eff35ae45319ac297f3f4df9dd35d51e531167`

PKG_CONFIG_SYSROOT_DIR/PKG_CONFIG_LIBDIR selected only the extracted development
tree; LD_LIBRARY_PATH and GST_PLUGIN_SYSTEM_PATH_1_0 selected the private runtime.
The tests created no additional live viewers or media producers. No Pi action,
service restart, source/quality change or hardware output test occurred.

## Reproduction and held-state handoff

Focused patch SHA-256:
`282cd49d4a5166c3779b94d11c457d20d50528c5dd9e36f3605bf0aab44bf02e`.
Fixture SHA-256:
`c4b10b7743e24305bca3fc2c19b7281308c5691a0f09da94284d7de2185bf601`.
Private preserved path:
`.evidence/frame-freshness-2026-10-12/admission-flush-generation/`.

The patch targets the recorded held header and is applied to that working tree,
but this documentation/fixture/patch commit does **not** release the complete
prototype. Given either preserved header, compile with the installed GStreamer
base development package and the selected header macro:

```sh
cc -std=c99 -O2 -Wall -Wextra -Werror \
  -DADMISSION_HEADER='"/absolute/path/to/admission-header.h"' \
  client/gst/admission_flush_generation_probe.c -o admission-flush-probe \
  $(pkg-config --cflags --libs gstreamer-base-1.0)
./admission-flush-probe
```

Expect exit 1 for the recorded pre-fix header and exit 0 for the fixed header.
The parent must review the delta before committing the complete prototype and
separately verify real decoder/pool/display and reconnect behavior when hardware
is reachable. This result does not establish that the prototype improves live
smoothness, repairs the Wi-Fi driver, or eliminates upstream source pauses.
