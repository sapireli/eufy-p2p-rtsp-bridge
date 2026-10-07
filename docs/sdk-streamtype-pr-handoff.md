# SDK PR handoff: managed live-start stream type selection

## Proposed upstream change

Expose `streamType?: 1 | 2` on the managed live-source options and forward the first opener's
selection through every start and retry. Preserve the existing omitted-option defaults:
HomeBase-attached starts use `1`; own-session starts use `2`.

Need: on a HomeBase-attached T8214, a managed live request using the attached default caused the
encoded Split composition to change to PiP during motion. A caller needs to select the captured
first-party value `2` while retaining SDK-owned encryption, shared-source ownership, and recovery.
This is a requested option backed by a real-device comparison, not a speculative API extension.

Implementation checkpoints:

- [SDK df535e27](https://github.com/sapireli/eufy-sdk/commit/df535e27f013dea610d2a5b2f775105a71407293)
  implements the managed selector and restores the original defaults.
- [Bridge b854115](https://github.com/sapireli/eufy-p2p-rtsp-bridge/commit/b854115bcc0b869df40a31ee1bc0f87faa49b61c)
  supplies `streamType: 2` on every camera pull and pins that SDK revision.
- [Incident timeline](dual-view-stream-selection.md) includes the earlier controls and deployment checks.
- [Sanitized live evidence](evidence/streamtype-motion-2026-10-07.json) includes original trace line
  references, source-file digests, and the measurements used below.

## Two fields that were confused during diagnosis

| Operation | Command / field | What was tested |
| --- | --- | --- |
| Set the dual-lens composition | `6243` for this model, `payload.video_type` | Split was configured as `12`; explicit PiP/Split switching already worked through raw commands. |
| Start the attached live pull | Outer `1350`, inner `1003`, `payload.streamtype` | Changing only `2` to `1` changed motion-time behavior. |

The earlier per-camera view UI was added in bridge commit `3b1a815`, without an SDK change.
It issues a view-setting command and persists the selected layout. It did not expose the start's
`streamtype` field. Successfully switching the view therefore did not establish that the managed
stream-start selector was configurable.

A third similarly named field exists in incoming video headers: the byte-5 `streamType` codec
hint. That is not the outgoing JSON selector. Do not use it as a live/recording filter.

The observed selector effect does **not** establish a universal name for values `1` and `2`, a
live-versus-recording distinction, or the firmware's internal encoder routing. Do not describe
`streamtype: 2` as a universal Split setting.

## First-party request and controlled comparison

An earlier first-party app packet capture, decrypted and authenticated during this investigation,
contained an attached T8214 start with the following shape. This is redacted request evidence,
not runnable credentials. It is not a capture of the phone's motion test on October 7.

```json
{
  "account_id": "<admin-account>",
  "cmd": 1003,
  "payload": {
    "msg_id": 1,
    "ClientOS": "IOS",
    "key": "<session-RSA-modulus>",
    "streamtype": 2,
    "camera_type": 0,
    "entrytype": 0
  }
}
```

The archived packet timestamp is 2026-09-19T23:08:27.416679 UTC. Its original capture SHA-256 is
`5290131e60869fa2950045854afd520cf6ac328a1941995648ee91448305c344` (15,277,116 bytes).
The evidence JSON includes the redacted packet census: selector `2`, header media type `10`, and
1600×2200 source video between 2.168 and 12.921 seconds after the app start. These counts are
packet-header observations, not whole access-unit counts, and do not establish motion behavior.
The raw capture remains private; the digest identifies it for an authorized local recheck.

The outer command was `1350`; the header selected channel `2`, sign code `8`, and media type `10`.
The app wrapper omitted the SDK's outer `mChannel` and `mValue3`. The controlled test used this
same app-shaped request for both selector values, so those other differences were held constant.
The same SDK session supplied the media encryption key. A stop/start was issued at each selection;
there was only one active bridge request for the target camera during this comparison. Other
camera streams continued running.

The camera remained configured for Split throughout the selector comparison. This means the
configured preference and bridge view commands were unchanged; it does not claim continuous
readback of an internal firmware preference. No bridge view-setting command was observed in
these motion windows.

All times below are UTC on **October 7, 2026** (October 6 locally). Motion times are bridge event
arrival times, not the precise physical instant a person crossed the detection boundary.

| Evidence | Time | Result |
| --- | --- | --- |
| Start selector `2`, original trace line 3381 | 00:59:23.197 | App-shaped `1350/1003`, header media type `10`. |
| First subsequent geometry, line 3386 | 00:59:28.241 | Raw source 1600×2200. |
| Motion, lines 3462 / 3465 | 01:00:16.824 / 01:00:17.686 | 32 subsequent raw keyframe metadata rows remained 1600×2200 through 01:01:19.631, 62.807 seconds after the first motion event. No geometry change or view command was logged in that interval. Operator confirmed normal views. |
| Same request, selector changed to `1`, line 3552 | 01:01:21.267 | Same wrapper, channel, header media type, account, and session key. |
| Motion, line 3598 | 01:01:58.411 | Raw source became 1920×1080 at 01:02:02.615 (line 3605), 4.204 seconds later. |
| Split restored, line 3649 | 01:02:30.857 | PiP geometry lasted 28.242 seconds. Channel `2`, transport DATA type `1`, and media type `10` stayed unchanged across the transitions. |
| Original SDK wrapper, overriding only selector to `2`, line 3673 | 01:02:45.671 | 1600×2200 playback was observed at 01:02:45.688. No additional motion was measured under this wrapper before the next checkpoint. |

A separate observer on the production `LiveStream` assembled-video event started at
01:02:05.413, after the first PiP transition. Its stats counted **376 H.264 access units at
1920×1080** before Split returned. This is a partial-episode count, not the total number of PiP
frames. Saved raw access units were decoded and visually showed a real main view plus inset;
the change was already present before RTSP, transcoding, or TV rendering.

The selector-2 run has raw keyframe metadata counts, not a complete delivered-frame count or
longest-freeze measurement. This change does not introduce frame suppression or keyframe gating.
Do not borrow loss-recovery performance claims from other SDK patches.

## Why earlier parallel results were insufficient

Earlier tests opened both the default SDK request and an app-shaped request on the same camera.
Both received the same motion-time PiP and matching source sequences/timestamps; decoded matching
keyframes had identical hashes. Those were not independent encoder outputs. A selector-1 request
could affect both sessions, so that result did not rule out a start-selector cause.

The next controls were:

1. Stop the bridge and trigger motion with the phone live view open. The operator reported the
   phone stayed in Split. This is an operator observation, not a captured phone video measurement.
2. Resume with only the app-shaped selector-2 request for the target camera. Motion retained Split.
3. Change only that request's selector to `1`. Motion reproduced PiP.

The single-field comparison is the strongest evidence for the selector's effect on this device.
It is one controlled motion episode per value, not a repeated randomized reliability study.
Earlier diagnostic notes that treated parallel feeds as independent or ruled out request fields
are superseded by this sequence.

## How the experiment worked before the public SDK option existed

The SDK already had an internal helper accepting a payload override. Through the Node inspector,
the diagnostic called it directly with `{ streamtype: 2 }` after stopping the existing pull:

```js
session.sendMediaPayloadLevel2(1003, channel, accountId, { streamtype: 2 });
```

The app-shaped A/B test instead constructed the same encrypted start packet twice, changing only
that JSON field. These were temporary internal diagnostic calls. They were not calls to
`openReadable({ streamType: 2 })`, which had not yet been implemented.

Before the fix, the normal path was:

```text
openReadable(options)
  → shared source's selected options
  → LiveStream.sendStart()
  → startLiveMedia(..., { force })
  → sendMediaPayloadLevel2(..., {})
  → attached serializer's built-in streamtype
```

`SharedSourceHints` did not expose `streamType`; the router did not forward it; `LiveStream` only
passed `force`; and `startLiveMedia` had no selector argument. Passing an arbitrary property to
`live()` did not make it reach the packet. The raw command API could carry arbitrary JSON, but
using it for starts would require reproducing the managed session's key/start/retry handling.

The fix threads a choice through that existing path. The bridge chooses the value; the SDK
serializes it and retains it across SDK-owned starts/retries. No debugger override is deployed.

## Synthetic verification and deployed path

Full SDK `npm run verify` passed on `df535e27`: **219 test files, 3,886 tests, 95 documented
snippets**, plus format, typecheck, architecture guards, build, ESM load, and example checks.
On macOS the existing shell guards needed GNU coreutils and GNU sed ahead of BSD tools in PATH.

The relevant specs decrypt synthetic emitted packets and cover:

- Attached omitted-option default `1`, explicit `2`, and unchanged empty stop payload.
- Public managed selection on attached and own-session level-2 starts.
- First-opener ownership and conflicting later hints.
- Warm retries, forced starts, and attached reassertion after silence.
- Own-session omitted-option default `2`, selected level-1 starts, byte-identical retransmissions,
  and forced starts.

Specs: `src/transport/p2p/__tests__/attached-media-requires-level2.spec.ts` and
`src/transport/p2p/__tests__/live-start-ack.spec.ts`. These prove plumbing/default behavior;
the live device comparison establishes why a caller needs the selector.

The bridge passed **102 tests** with this installed revision. DietPi deployment verified the lock
revision and compiled default `1`, then inspected `streamType: 2` on three active managed pulls
(two attached, one own-session). A normal managed forced reassertion emitted two attached starts,
both decrypted with selector `2`. The own-session start was not newly emitted during that short
inspection, so its retry-wire coverage is synthetic.

Health reported all three feeds active, zero stalls, authentication OK, push connected, and go2rtc
running. RTSP probes returned H.264 524×720 and 640×720 for the two dual-lens cameras. A fresh Fire TV
screenshot showed Split playback. Diagnostic wrappers were restored and the inspector was closed.
No additional motion was measured after deploying the public-option implementation. State that
limit in the PR: the controlled motion comparison and deployed wire verification are distinct tests.

## Upstream patch boundaries

Read the SDK's current `AGENTS.md` and `CONTRIBUTING.md`, re-fetch its target beta branch, search
existing PRs/issues for this same fix, and follow its current PR rules. This handoff does not assert
that a new upstream PR is ready against an unexamined current head.

Do not merge the entire fork branch: it contains unrelated local changes. Do not submit the earlier
global-default change `7dba7e8`. The SDK option commit follows that change and reverses it, so blindly
cherry-picking it onto upstream may conflict on a default that upstream already has.

Use the net selector-only change from SDK `e347add323cb7af83d80115780d77ff7f9fac283` through
`df535e27f013dea610d2a5b2f775105a71407293`, restricted to these seven files, and adapt it to the
current upstream base:

- `src/core/contracts.ts`
- `src/transport/p2p/command-router.ts`
- `src/transport/p2p/live-stream.ts`
- `src/transport/p2p/p2p-session.ts`
- `src/transport/p2p/__tests__/attached-media-requires-level2.spec.ts`
- `src/transport/p2p/__tests__/live-start-ack.spec.ts`
- `docs/live-media.md`

Inspect that net patch: omitted defaults must be unchanged relative to the chosen upstream base.
Keep this PR about managed start selection. Exclude loss signaling/gating, reorder waits, media-slot
allocation, view-setting commands, frame suppression, transcoder changes, and client rendering.
Use synthetic identifiers in SDK tests; public prose must omit actual device/account IDs, keys,
addresses, home images, and raw capture attachments. Keep the upstream PR consumer-agnostic.

## Suggested upstream PR explanation

> A HomeBase-attached T8214 was configured for Split, but the default managed start produced PiP
> during motion. An authenticated first-party app start used `streamtype: 2`. In a single-request
> comparison, selector 2 retained 1600×2200 through 62.8 seconds of post-motion keyframe observations;
> changing only that field to 1 produced 1920×1080 PiP 4.204 seconds after motion for 28.242 seconds.
> No view-setting command occurred in either motion window. This establishes the selector's observed
> effect on this model, not a universal protocol meaning.
>
> The raw transport can send the field, but the managed live-source API did not forward a caller's
> choice. This change adds an optional selector to that existing managed path and retains it through
> retries. Omitted defaults remain attached 1 and own-session 2. Decrypted synthetic wire tests cover
> public selection, defaults, retries/reassertion, and stop payloads. The full verification gate passed.
>
> The deployed managed path was checked with explicit selector 2 on active feeds and two decrypted
> attached reassertions. Post-deployment playback was verified; additional motion after deployment was
> not tested. Firmware semantics and broader model behavior remain unestablished.
