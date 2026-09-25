# Plug-and-play setup and terminal layout plan

Status: proposed specification, 2026-09-25. This document describes the next product work; it does not describe features already shipped.

## Outcome

A person with a fresh Debian server and a fresh Raspberry Pi or Debian display client can get a working wall without hand-editing YAML, finding DRM plane IDs, building Go locally, or learning the bridge's HTTP API. The same person can later change cameras and layouts safely, see why a choice will not work on the target hardware, and recover from a bad configuration.

Each existing program owns its own setup and config experience. The Node bridge exposes an `eufy-bridge` command; the Go client extends the existing `eufy-wall` binary. Both can generate a config interactively, validate and apply a hand-written YAML file, diagnose their host, and roll back a failed change. Defaults, templates, schema checks, and help live beside each program's runtime config loader. YAML remains an external file so a user can edit it directly, keep it in Git, or send it to a host through SSH. There is no third setup program and no web interface.

## Decisions

| Area | Decision | Reason |
| --- | --- | --- |
| Installation | Versioned server and client release artifacts | A production install should not require a repo checkout, local Go build, or Git dependency build. |
| Server onboarding | `eufy-bridge setup` in the Node bridge package | Credentials, 2FA, LAN probing, and station configuration belong with the bridge's config loader. |
| Client onboarding | `eufy-wall setup` in the Go client binary | The display host can inspect DRM, GStreamer, codecs, and actual RTSP reachability. |
| Layout editing | `eufy-wall layout edit` terminal editor, with local screen and PNG previews | The layout engine and editor share the same Go geometry code. |
| Hand-written config | Both programs accept YAML from a file or stdin | The interactive path never becomes the only way to configure a host. |
| Layout model | Explicit rectangles on a logical grid, up to 32 columns by 32 rows | A 32×32 canvas gives fine placement without implying 1,024 simultaneous streams. |
| Existing config | Keep current presets and YAML valid; add `schema_version: 2` for new features | Current deployments must upgrade without an immediate migration. |
| Apply | Validate, stage, activate, health-check, and roll back | A bad edit should not leave a display black or a bridge unable to start. |

## What exists and what this plan adds

The bridge already has YAML config, `/api/cameras`, `/healthz`, `/auth/*`, RTSP via go2rtc, and motion events. The client already has presets, first-fit placement, hardware detection, `-dry-run`, and per-tile processes on the planes sink. Both have install scripts and systemd units. The scripts still copy example configs and ask for manual edits. The client currently accepts grids no larger than 6×6, places tiles in order, and requires explicit plane IDs for the planes sink. The runbooks have unfilled device and performance tables.

Before calling setup complete, fix these existing edge cases as part of the work:

- A fixed tile pointed at an `on_demand` camera currently takes no hold, so it can wait forever for a stream. Give fixed on-demand tiles a bounded, refreshed hold while visible. Fixed `on_motion` battery tiles must continue to sleep between events.
- A `motion: latest` tile can switch among cameras with different codecs, but it currently learns a camera's codec only when that camera also has a fixed tile entry. Put codec in bridge inventory/events and select a decoder for the camera actually shown, with a local fallback for offline use.
- The existing 2FA/captcha HTTP examples put challenge answers in query strings. Move wizard use to POST bodies before onboarding so answers do not enter URL logs or shell history; retain a documented compatibility path only if necessary.
- A wall with a compositor sink still restarts the whole pipeline when one tile changes. Make tile changes and source recovery independent so an unaffected tile keeps rendering.
- Add a client progress watchdog for a GStreamer process that remains alive while frames stop advancing. A process-exit supervisor alone cannot detect a frozen picture; recovery must restore frame progress.

## User journeys

### New server

1. Download a versioned server release bundle and run its installer. It verifies the package, prints the exact version, and checks prerequisites before changing services. The bundle includes the Node bridge, its built dependencies, and go2rtc.
2. Run `sudo eufy-bridge setup`. It offers detected LAN interfaces and CIDRs, asks for a dedicated Eufy account, writes secrets with mode `0600`, and starts the bridge on loopback for login.
3. The wizard displays auth state and prompts for the 2FA or captcha answer when required. It never prints or saves the password, token, or challenge answer in logs.
4. After login, it lists discovered cameras with name, serial, power policy, codec when known, station, dual-view capability, and live status. The user selects enabled cameras and per-camera mode; battery and power-override warnings use the same rules as the runtime config loader.
5. The wizard tests the chosen LAN policy and RTSP path, asks which address the clients should use, and offers an optional bounded stream probe for each selected camera. It shows a summary of changes before applying them.
6. On success it prints the bridge URL, RTSP URL, health result, and the command to export a sanitized camera inventory for an offline client. On failure it restores the previous config and service state, with an actionable error.

Commands: `eufy-bridge setup`, `eufy-bridge doctor`, `eufy-bridge config validate <file|->`, `eufy-bridge config apply <file|->`, `eufy-bridge config example`, `eufy-bridge inventory export <file>`, `eufy-bridge status`, and `eufy-bridge upgrade`. The same Node modules that run the bridge validate its config; a thin installed command dispatches the subcommands. Noninteractive commands accept `--json`; `setup --answers <file>` supports reproducible installs without putting secrets in command arguments.

### New client

1. Copy or download the `eufy-wall` release binary for the Pi or Debian display host and run its installer; no workstation cross-build is needed.
2. Run `sudo eufy-wall setup`. It finds connected outputs, screen modes, available GStreamer elements, decoders, and usable DRM planes. It selects the display output explicitly when more than one is connected.
3. Enter or discover the bridge address. Discovery may use mDNS, but manual address entry always works. The CLI checks HTTP health, imports camera inventory, and tests RTSP on selected cameras without holding a battery camera indefinitely.
4. Choose a starter template (one camera, split screen, 2×2, 1+5, motion screen), open the terminal layout editor, or import an existing YAML file. The CLI shows a text preview and lists every warning.
5. The CLI checks layout fit, decoder/codec match, usable planes, stream count budget, network reachability, and a short render probe. It applies the config only after validation and reports the service's live status.

Commands: `eufy-wall setup`, `eufy-wall doctor`, `eufy-wall layout edit [file]`, `eufy-wall layout preview <file>`, `eufy-wall config validate <file|->`, `eufy-wall config apply <file|->`, `eufy-wall config example`, `eufy-wall status`, and `eufy-wall upgrade`. These are subcommands of the existing Go binary. Keep `-config`, `-dry-run`, and `-print-layout` working for existing scripts.

### Terminal layout editor and previews

`eufy-wall layout edit` opens a full-screen terminal UI with templates, an existing-config import, a camera list from the bridge, a scaled canvas, and a property panel. A user can add a fixed or `motion: latest` tile, select it, move or resize it by 1/4/8 grid cells, enter exact coordinates, choose its camera or watch set, and undo or redo changes. The canvas preserves the selected display's aspect ratio. It shows tile IDs, overlaps, gaps, letterboxing, and motion simulation. All editing functions also have documented keyboard shortcuts and work in a monochrome terminal.

The editor does not try to draw all 32×32 cells as text on a small terminal. It scales rectangles into a viewport and always shows exact grid coordinates and pixel dimensions in the property panel. It supports terminals at least 80×24, has a linear menu mode for smaller or screen-reader sessions, and never requires mouse input. `eufy-wall layout preview <file> --display` renders numbered colored rectangles and letterboxing on the attached HDMI output without opening camera streams. `--png <path>` creates the same preview for SSH sessions. A short optional live probe can be run after validation to confirm decoder and RTSP behavior.

Every edit runs validation. Errors prevent apply; warnings remain visible in a saved report. The editor saves a regular `eufy-wall.yaml` through the same safe apply path as `eufy-wall config apply`. Drafts can be saved separately and reopened. An interrupted editor never changes the active config.

## Config and compatibility contract

- Add `schema_version: 2` to newly generated server and client YAML. Omitted version means the existing format. Reject future unknown major versions with a clear upgrade message.
- Keep the runtime YAML files as the deployment format. The Node bridge owns its server schema and validation; the Go client owns its client schema and geometry. Put versioned inventory and cross-program fixtures under `config-contract/`. The two programs agree on their shared camera fields without creating a third config implementation. No editor may silently discard an unknown key during import/export.
- Add an explicit `bridge_url` on the client. It is the HTTP/WS control endpoint; `rtsp_base` remains the RTSP endpoint. Existing configs may continue deriving the former from the latter for compatibility, with a warning when that guess fails.
- Camera serial is the stable identity. Stream keys and display names are metadata from the bridge; neither becomes the primary key in an editor. The bridge inventory includes codec, power/mode, stream key, and URL, plus a schema version. The exported inventory omits credentials, session tokens, LAN peer addresses, and retained snapshots.
- Make tile IDs stable and unique. Process names and editing operations use IDs, so two tiles showing the same camera do not collide and reordering the YAML does not restart unrelated tiles.
- An editor round-trip of a supported v2 file must preserve behavior. For a legacy file, the owning program presents a migration diff and writes v2 only after explicit acceptance. Unsupported fields appear as errors with their YAML path.

Proposed v2 client layout excerpt (illustrative; field names are finalized with the schema task):

```yaml
schema_version: 2
bridge_url: http://192.168.1.10:3000
rtsp_base: rtsp://192.168.1.10:8554
output: HDMI-A-1
layout: custom
canvas: { cols: 32, rows: 32 }
tiles:
  - id: front-door
    camera: T8214XXXXXXXXXXX
    rect: { x: 0, y: 0, w: 20, h: 32 }
  - id: recent-motion
    motion: latest
    watch: [T81A0XXXXXXXXXXX, T8425XXXXXXXXXXX]
    blank_after_seconds: 90
    rect: { x: 20, y: 0, w: 12, h: 32 }
```

Legacy `layout: 1`, `1+5`, and `<cols>x<rows>` keep first-fit placement and `span`/`aspect` behavior. New `layout: custom` requires a rectangle for every tile; there is no hidden placement step. Templates in the editor emit explicit rectangles so a saved design looks the same on the host.

## Hand-written YAML is a first-class path

Both programs embed annotated examples and document every key, default, allowed value, interaction, and compatibility rule. The checked-in references are `docs/config-server.md`, `docs/config-client.md`, and `docs/layouts.md`; the installed commands can show the matching version with `config example` and `config explain <path>`. The docs include complete working examples for a wired wall, battery motion tiles, mixed codecs, two monitors, custom 32×32 rectangles, and an offline client.

`config validate` and `config apply` accept a filename or `-` for stdin. This makes a hand-authored config as easy to use as a generated one, including over SSH:

```sh
eufy-wall config validate ./wall.yaml
sudo eufy-wall config apply ./wall.yaml
cat ./wall.yaml | ssh pi 'sudo eufy-wall config apply -'

eufy-bridge config validate ./bridge.yaml
sudo eufy-bridge config apply ./bridge.yaml
cat ./bridge.yaml | ssh server 'sudo eufy-bridge config apply -'
```

The `-` form reads stdin once, stages the bytes, and runs exactly the same validation, backup, health check, and rollback as a file. Diagnostics name the YAML path and line where possible. The generated config is ordinary YAML and can be edited after the wizard runs. The server keeps credentials in its separate environment file by default; documented legacy YAML credentials continue to load with a warning.

There is no HTTP config upload endpoint. Custom YAML is supplied as a file or through stdin, including over SSH. Neither the bridge nor the display service accepts remote config writes through its network API.

## 32×32 canvas rules

- Canvas dimensions are integers from 1 to 32 on each axis. The editor defaults to 32×32. A rectangle uses integer `x`, `y`, `w`, and `h`, with `x >= 0`, `y >= 0`, `w >= 1`, `h >= 1`, `x + w <= cols`, and `y + h <= rows`.
- Rectangles use half-open bounds. Overlap is invalid in v2; gaps are allowed and render black. Z-order and overlays are deliberately deferred because they require a different compositor model.
- Convert grid edges to pixels independently: `left = floor(x * screenWidth / cols)`, `right = floor((x+w) * screenWidth / cols)`, and the same for Y. Width is `right-left`. This covers the screen edge exactly and avoids cumulative rounding drift. Reject zero-pixel dimensions for a particular output mode.
- Layout geometry is separate from source count. A 32×32 canvas is 1,024 placement cells, not 1,024 decoders. Set a defensible tile-definition limit in the schema after benchmarks, and enforce an active-stream budget from measured target profiles. The editor must not claim that a layout will run on a Pi solely because its rectangles fit.
- For planes, every active tile needs a usable plane that can reach the selected CRTC. The CLI discovers and probes those plane/connector combinations; it does not merely count IDs. A compositor profile has its own decode and memory budget and must report its current whole-wall restart behavior.
- Minimum visible tile size, maximum practical feeds, and acceptable frame rate are target-profile data measured in the hardware workstream, not guesses embedded in the editor. An unmeasured device can pass geometry validation but must show `performance unverified` until a local render probe succeeds.

## Validation and apply transaction

Validation has three levels: (1) pure schema and geometry, available offline in the owning program; (2) inventory checks, including serials, power policy, stream URL, and codec; (3) host checks, including decoder elements, DRM output/planes, reachable bridge, RTSP probe, and render progress. Both CLIs return structured diagnostics with stable codes, severity, field path, and a human remedy. The client editor shows checks as they become available and labels disconnected host checks as pending.

`config apply` writes a candidate in the same filesystem as `/etc/eufy-wall*.yaml`, preserves owner/mode, validates it, makes a dated backup, atomically renames it into place, restarts only the affected service, then waits for a bounded healthy interval. A failed start, missing frames, or lost bridge health triggers automatic rollback and a second health check. The command exits nonzero and retains the failed candidate and diagnostic report. `status` names the active version and last rollback. The procedure must be safe to retry and safe across power loss.

The server setup flow must stage credentials separately from nonsecret YAML. The client config contains no Eufy secrets. Re-applying an unchanged config should not restart a healthy service. Automated installation must be noninteractive when answer files and a secret source are supplied.

## Security and privacy

- The bridge and client setup commands run locally and do not send inventory, configs, diagnostics, or credentials to a project service. Discovery and API calls stay on the LAN; release downloads are the only expected external requests after installation.
- The existing bridge HTTP and RTSP surfaces trust the LAN. Setup does not expose them through a public tunnel. The wizard uses loopback for credential-bearing auth actions and does not widen bridge access controls.
- Secrets use a root-owned `0600` environment file or a documented external secret source. Never put passwords or 2FA codes in URLs, shell history, diagnostic bundles, or setup logs. Redact auth responses and tokens from `doctor --json`.
- Release artifacts are pinned by version and verified with checksums and a published signature/provenance. Rollback must also work across a failed binary upgrade, not only a bad YAML edit.

## Delivery workstreams and order

### 0. Baseline and hardware evidence

1. Fill in the server runbook's verified-device table and the client runbook's Pi performance table with actual model, codec, geometry, output, kernel, frame rate, CPU, dropped frames, and planes/compositor results. Include H.265 and dual-lens cases.
2. Run a repeatable soak/recovery matrix: 30 minutes live, camera restart, station restart, network interruption, stale frames with a living GStreamer process, bridge restart, and motion on a sleeping battery camera. Record baseline failures before changing setup.
3. Fix the fixed `on_demand`, mixed-codec motion tile, compositor isolation, and frozen-frame cases listed above, with regression tests and host verification. These are product correctness dependencies for reliable setup recommendations.
4. Audit the existing server and client paths for confirmed bugs, including config loading, camera policy, stream recovery, event reconnects, layout placement, and shutdown. Record each finding with a reproducer; fix it or document why the observed behavior is intentional.

### 1. Config contract and host diagnostics

1. Specify v2 schemas, inventory format, error codes, migration rules, and fixture corpus. Add contract tests in the Node bridge and Go client. Add POST-body auth challenge handling for the server setup wizard.
2. Extend the Go layout engine for explicit rectangles and 32×32 bounds; retain preset behavior. Add property/golden tests for overlap, rounding, full-screen edges, repeated camera tiles, and multiple outputs.
3. Add stable tile IDs, explicit `bridge_url`, and per-camera runtime codec data. Ensure the event handshake and reconnect refresh the inventory without restarting unaffected tiles.
4. Build `doctor`, `config validate`, and the client's `layout preview` before interactive wizards. Diagnostics must distinguish missing package, unsupported codec, inaccessible DRM output, unusable plane, unreachable bridge, auth failure, and absent RTSP frames.

### 2. Safe installation and apply

1. CI produces versioned server and client artifacts for supported architectures. Bundle the built pinned SDK and production Node dependencies so target hosts do not compile a Git dependency. Package or pin go2rtc and all external version references.
2. Replace repo-relative install assumptions with idempotent release installers. Support online and offline artifact input, prerequisite checks, checksum/signature verification, existing-install detection, upgrade, and binary rollback.
3. Implement atomic config apply and automatic rollback for both services, including power-loss and interrupted-process tests. Keep existing hand-authored configs working.
4. Add a migration command that shows a diff, backs up the old config, and can restore it. Never silently replace `/etc` files.

### 3. Integrated server and client setup

1. Add `eufy-bridge` subcommands in Node, reusing `server/src/config.mjs` and existing bridge modules. Ship them in the same release package and version as the running service.
2. Add `eufy-wall` subcommands in Go, reusing `client/internal/config`, `layout`, `detect`, and `pipeline`. Ship them in the same binary and version as the renderer.
3. Server wizard: LAN/interface choice, dedicated account, auth challenges, discovered camera policy, dual view/quality only when requested, bounded live probe, and sanitized inventory export.
4. Client wizard: output and plane probing, bridge discovery/manual address, inventory import, starter templates, codec checks, render test, and safe apply.
5. Provide noninteractive answer files, JSON results, clear interruption/retry behavior, and docs for headless SSH use. Run usability tests on a truly fresh host without repo knowledge.

### 4. Terminal layout editor and previews

1. Build a keyboard-accessible terminal editor with templates, import/migration, camera selection, move/resize, undo/redo, a scaled aspect-correct viewport, a source inspector, and motion simulation. Provide a linear mode for small terminals and screen readers.
2. Produce the same layout as an HDMI test-pattern preview and optional PNG. Share geometry code with the client renderer where practical and run the same fixtures against both. Add edit/save/reopen, import/export round-trip, terminal resize, and accessibility tests.
3. Integrate draft save and safe apply into the `eufy-wall` editor. Publish the config schema version in generated files and diagnostic reports.

### 5. Manual config documentation

1. Write the server, client, and layout references alongside implementation. Include defaults, precedence, examples, error remedies, stdin/file apply, migration, backup, and rollback.
2. Generate installed `config example` output from versioned templates used by tests. Add `config explain <path>` and JSON diagnostic examples so docs cannot drift silently from the binaries.
3. Follow the manual YAML path on a clean server and client, including a config sent through stdin over SSH. It must meet the same validation and recovery criteria as wizard output.

### 6. Resilience and adversarial verification

1. Inject bridge loss, LAN partitions, DNS and RTSP timeouts, WebSocket disconnects, packet stalls, corrupt or partial camera events, Eufy auth expiry, and camera or station errors. The wall must keep unaffected tiles rendering, mark unavailable tiles honestly, retry with bounded backoff and jitter, refresh inventory after reconnect, and release stale on-demand holds.
2. Kill the bridge, go2rtc, individual tile pipelines, the compositor, the wall process, and each config-apply command at each transaction phase. Verify systemd and transaction recovery, no permanent black screen from a stale frame, no duplicate holds, and no half-applied config or binary upgrade.
3. Exercise repeated failure and recovery cycles, including an offline start followed by reconnection and a camera changing codec mid-session. Define a measurable recovery deadline and frame-progress check for each supported hardware profile, record failures in the runbooks, and fix confirmed runtime gaps before declaring setup advice trustworthy.
4. Add adversarial, behavior-focused unit and integration tests in both languages, including malformed input, concurrent apply, interrupted writes, retries, and reordered events. Keep implementation files focused and easy to refactor. Require more than 80% aggregate statement or line coverage in both the Go client and Node bridge, measured by the project CI, without adding tests that merely mirror implementation.

### 7. Release qualification

1. Exercise fresh install, upgrade, config migration, failed apply, rollback, and offline install on Debian amd64/arm64 and supported Pi OS images.
2. Verify at least one measured layout at each supported hardware profile, including a battery motion tile, a dual-lens portrait feed, and a mixed H.264/H.265 setup where hardware permits.
3. Verify docs by following them from a clean machine. Include `doctor` output and recovery steps in both runbooks. Mark unmeasured combinations as unverified rather than supported.

## Acceptance criteria

- A new server and client reach a working two-camera wall through installers and interactive CLIs with no manual YAML edits or local source build.
- A user can make an arbitrary nonoverlapping arrangement on a 32×32 canvas in the terminal editor and get the same tile rectangles from its HDMI, PNG, and runtime previews on a 1920×1080 and an odd-size output.
- The editor catches malformed geometry and unknown cameras; host checks catch missing codecs, unusable planes, and unreachable streams before replacing a working config.
- A bad client or server config automatically restores the previous running version, and the rollback reason is visible in `status`.
- Legacy configs keep their prior placement and startup behavior. A round-trip through migration cannot silently lose settings.
- Server setup/config commands live in the Node bridge package; client setup/config/layout commands live in the Go client binary. Both accept custom YAML files and stdin over SSH. No setup step requires a browser or public exposure of the bridge.
- A user can complete the documented manual YAML path without running either wizard and receives the same diagnostics and rollback behavior.
- The documented supported hardware profiles pass the soak/recovery matrix with measured frame progress and bounded recovery time.
- Network, camera, and process failures recover within the documented bounds without disrupting unaffected tiles; both CI coverage reports exceed 80% overall and the adversarial and unit tests pass.

## Decisions to confirm before implementation

1. Which hardware is a release target: Pi 1, Pi 3, Pi 4/5, Debian x86, or a smaller subset? The benchmark and package matrix should match actual deployment intent.
2. Should a local HDMI preview be mandatory before apply on a headless SSH session, or optional when the PNG and render probe pass? This plan makes it optional.
3. Should deliberate overlapping tiles/overlays be a later feature? This plan rejects overlap in v2 so the render result is predictable.

## References

- Current design and implementation: `docs/superpowers/specs/2026-09-18-eufy-wall-design.md`, `docs/phase-2-motion-battery.md`, `client/internal/layout/`, `client/internal/config/`, and `deploy/`.
