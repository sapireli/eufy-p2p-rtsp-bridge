# Live bridge SDK fork upgrade, 2026-10-10

## Target and installed version

The requested target is `sapireli/eufy-sdk` branch **`eufy-wall`**. Its verified
remote head was `d35abffa61e7794a4731fadb0e237466dd00077b`, reporting package
version **0.5.0**. It incorporates upstream npm beta `0.5.0-beta.19`, whose
published git head is `96f9a4133429baaa201a90c726412451a0485883`, plus fork fixes.

The published npm beta alone lacks the fork's pre-connection LAN peer callback
and adopted-probe receive-buffer request. A local compatibility check of that
package failed two bridge contract tests. That trial was reverted before any
production dependency change. The intended fork then passed **114/114 bridge
tests**. The bridge dependency remains `github:sapireli/eufy-sdk#eufy-wall`,
locked to the exact commit above; consumer pin commit is `2d27c97`.

The fork retains the LAN callback through the facade, router and session, with
rejection before peer handshake/connection; the adopted-socket buffer request;
the managed stream selector; and the source-header timestamp exposure. The
deployed buffer fix was cherry-picked onto the refreshed fork as `46f1232`.
Source-header timestamps are distinct from the camera's visible date/time
overlay; exposing them does not restore missing overlay pixels.

## Deployment procedure and artifact

Before this change, production's lockfile still referenced `df535e2`, version
0.3.0, with a locally applied buffer-only candidate bundle. A clean production
install of the new lockfile was prepared in a separate staging directory with
`npm ci --omit=dev`. Its SDK exports loaded successfully, its resolved source
matched the target commit, and its bundle matched the locally tested build:

`8d97e4ff7daa442a3c052b6208941bfe3a066f295b5d7a306e3abf7fa9e8b9ad`

The service was briefly stopped, dependency directories and manifests were
switched, and the bridge started at **17:46:33 UTC**. Original dependencies and
manifests were retained under
`/opt/eufy-wall-bridge/sdk-backup-df535-before-d35abff-20261010T174631Z`.
The installed source mirror's manifests were updated to the same pin for future
installer runs. No application source, camera setting, resolution, decoder
preference, credentials, or LAN configuration was changed by this upgrade.

## Actual runtime verification

- The running installation reports SDK **0.5.0** and the exact `d35abff` resolved
  source. Its loaded-entry bundle hash matches the staged/local hash above.
- `/healthz` reports authenticated/healthy, three active streams, no blocked
  cameras, zero stalls after restart, and go2rtc running.
- Existing `lan.cidr: 192.168.23.0/24` and `lan.force: true` remained in place.
  The startup guard reported LAN peers for the HomeBase and independent camera.
- Between approximately **17:47:11.376 and 17:47:41.397 UTC**, a passive capture
  counted **12,331 incoming P2P DATA packets** to bridge-owned UDP ports. All
  came from the two LAN peers; **zero** came from outside the configured CIDR.
  Port ownership was refreshed every second. Cloud lookup replies were excluded
  by message type; internet authentication/lookup remains allowed.
- Both Fire TVs automatically reconnected. On one, Front Door rendered frames
  increased from 176 to 330 and Garage from 206 to 356 during 17:46:57–17:47:07.
  On the other, Front Door increased from 195 to 349 and Garage from 206 to 356
  during 17:46:57–17:47:08. Both used the Amlogic hardware AVC decoder and
  reported zero queued compressed units.

These observations verify the requested fork deployment, LAN-only behavior in
the captured interval, and playback recovery. They do not establish a fix for
the separate source pauses, GPU failure trigger, or missing visible timestamp.
