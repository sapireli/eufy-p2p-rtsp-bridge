# Simplified TV workflow — 0.6.1

The user found the combined setup/editor confusing. The revision separates
everyday viewing, wall editing, and bridge connection into three screens.

- Home: Watch cameras (initial focus), Edit wall, a short list of configured
  views, and Connection settings.
- Editor: Done, Choose cameras (checkboxes), Add motion view (opens watched
  camera selection), Layout, and camera cards.
- Fixed-camera actions contain no motion watch/timing controls. Motion views
  have watched-camera and explicit duration choices. Grid size is advanced.
- Layout offers Automatic and equal-column presets. Adding a camera grows grid
  rows when necessary, preserving existing IDs, spans, positions and motion
  views. Impossible layouts are rejected with a plain-language message.
- Back returns from editing/connection to home, and from live video to home.
  No values require repeated clicks to discover the next setting.

The app unit suite passed **87 tests**, including eight new editing tests for
identity/order/span retention, duplicate camera selection, row expansion,
unique IDs and preset bounds. No decoder, RTSP, bridge, SDK, hold or snapshot
policy implementation was changed by this UI revision.

Fire TV .58 remote checks passed: Home default focus, separate editor,
original checked camera selection with Cancel, fixed-camera action menu,
layout preset navigation/Cancel, Back navigation, separate connection page,
address-field Down to Connect, and return to hardware-decoded playback.
Screenshots were used when Fire OS UIAutomator returned an unavailable root;
stale hierarchy files were not treated as current proof.

The final appearance patch filled the ScrollView background after functional
navigation testing. Its final home screenshot on .85 shows the complete dark
viewport with Watch/Edit actions and the user's three existing cameras.

Both TVs received `install -r` version 0.6.1 / code 10 and were returned to their
saved live walls. Both preference files remained byte-identical to fresh
pre-revision backups (.85 had three views; .58 had two). Final APK SHA-256:
`1a48014e847c45046647ce049f59607fad83795528bf41cf43bfa76134bbf95d`.

Private artifacts are under `.evidence/android-simple-ui-2026-10-13/`:
home screenshots, remote navigation records, and unit XML results. Existing
camera names/configuration are preserved; private preferences are not published.
