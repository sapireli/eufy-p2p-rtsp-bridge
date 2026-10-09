# Front Door missing date/time investigation — 2026-10-09

## Verified boundary

The user reported that the Front Door **T8214** date/time overlay sometimes disappears in both Eufy Wall and the Eufy phone app. At the time of this capture it was missing. A direct capture of the bridge's shared Annex-B HTTP output, **before FFmpeg, go2rtc and either display client**, also has no date/time or Eufy logo anywhere in the whole frame.

Capture: approximately **18:18:53–18:19:03 UTC**, `GET /stream/T8214510242321E6`, ten seconds, existing shared feed. This did not create another camera/P2P session or change camera settings. Independent software decoding produced 109 frames at **1600×2200**, with the normal two vertically stacked lens views and encoded black separator. The first and last complete frames were visually checked; both lack the overlays.

Private artifacts in `.evidence/doorbell-osd-2026-10-09/`:

| File | SHA-256 |
| --- | --- |
| `source-current.h264` | `06d94de288c740e66b46de27ebf66e2d9f219bc82737b5c1f16e7400789570e0` |
| `source-first.png` | `c4313a0df1478d437617037ab7f2419c49f65286ee6c6ff50577efe1c762b9bf` |

This excludes a client crop, display-plane issue or bridge transcoder removing an otherwise present overlay in this sample. It does **not** identify what made the camera stop generating it.

## Correlated event, not yet a cause

Production logs record an earlier source-geometry transition:

| UTC | Event |
| --- | --- |
| 18:14:04.933 | Front Door FFmpeg producer EOF |
| 18:14:06 | Source H.264 1600×2200 → 1600×1200 |
| 18:14:11.751 | Front Door FFmpeg producer EOF |
| 18:14:13 | Source H.264 1600×1200 → 1600×2200 |

The overlay was still absent several minutes after portrait Split geometry returned. No raw sample spanning the transition or simultaneous watermark-property observation was collected, so this does not prove that a composition/motion transition removed the overlay.

## Actual deployed command audit

The deployed SDK bundle is SHA-256 `5ae12418fb1a7048bdff04c55d44d647009dcd57274897f990bf63453023f193`. Its watermark capability uses command **1214 (`CMD_SET_DEVS_OSD`)**, with values 0/1/2; the documented live wire verification is **T8425**, not this T8214 doorbell.

The deployed bridge's only general `setProperty` call is `streamingQuality`; its dual-view setter sends command **6243** with `{restore:1, video_type:12}` for Split. Front Door view pins occurred at boot **17:57:53**, and earlier re-login **17:56:18**. Production still applies these view pins at boot/re-login; it is incorrect to describe the current service as sending them only for explicit user view changes.

The bridge explicitly requests managed `streamType:2`. For this **HomeBase-attached** camera, the deployed SDK uses the attached `1350`/inner `1003` start path with `streamtype:2`; its attached start payload contains **neither `restore` nor `video_type`**. The other own-session branch's `startLiveJson` contains `restore:0`, `streamtype:2`, `video_type:12`, but that branch is not the attached Front Door start. No OSD setter was found in the bridge call path. Source audit alone is not an outgoing command trace and does not prove that every camera-side setting stayed unchanged.

## Cached setting and firmware observation

At **18:25:50.796–18:25:52.720 UTC**, a bounded Node inspector read queried the existing SDK instance's registry directly, without invoking a getter that can refresh stale state. No cloud login, cloud refresh, camera command or setting write was performed. The complete excluded inspector window is **18:25:49–18:25:59 UTC**, coordinated with the independent packet-timing audit. The inspector port was confirmed closed afterward and its SSH tunnel terminated.

The cached camera record reports:

| Field | Observation |
| --- | --- |
| Model/channel | T8214 / 2 |
| Main/secondary firmware | 3.2.7.2 / 0.3.6.0 |
| Raw watermark param 1214 | `"1"` |
| Param 1214 cloud observation timestamp | **18:14:53 UTC**, approximately 11 minutes before the read |
| Cached dual-view param 6243 | Absent |
| Existing bound Device object's state | Not retained in this SDK instance at the time of the read |

The **model matters** when interpreting `1`. The deployed SDK's generic watermark enum describes the T8425-verified values 0=Off, 1=Timestamp, 2=Timestamp+Logo. However, the [primary eufy-security-client model table](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/http/types.ts) maps `BATTERY_DOORBELL_PLUS_E340` to `DeviceWatermarkBatteryDoorbellCamera1Property`, which defines **1=Off, 2=On**. This table supports interpreting the cached T8214 value as disabled, not enabled. It is reference implementation evidence, not a fresh command/response verification on this camera. The raw value and timestamp are therefore retained without substituting the SDK's generic enum label.

The same reference's [`Device.isBatteryDoorbell`](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/http/device.ts) includes this model, and its [`Station.setWatermark`](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/http/station.ts) sends command 1214 with value 2 for On, device channel as both `valueSub` and routing channel, and authenticated account identity. Its dual-view setter sends the same 6243 `{restore:1,video_type:12}` Split payload used by the bridge. This documents the proposed controlled test's family, values and routing; it does not establish that a view command resets watermark state.

The cloud timestamp is an observation time, not proof that the setting **changed** at 18:14:53. This read cannot identify who disabled it or establish its value before the 18:14 geometry cycle.

The reference's label `Off` also does not, by itself, prove that T8214 disables **date/time** rather than only the Eufy logo. Battery-doorbell watermark UI semantics differ from the generic three-state timestamp/logo enum. Thus `1214="1"` must not be presented as the established root cause of the missing date. A controlled accepted On command and actual source pixels are required before attributing either overlay to that setting.

The bridge was independently restarted for the client test at **18:25:24 UTC**. Front Door's boot view pin was sent at **18:25:30**, and its feed became active at **18:25:40**. A new raw capture at approximately **18:26:17–18:26:22**, 28 independently decoded frames at 1600×2200, still lacked date/time and logo. Thus this bridge reconnect and Split pin did not restore the overlay.

Private evidence: `cached-osd-state.json`, `source-after-restart.h264` (SHA-256 `a6931088d9cc0b2cd015d4a92630570a6eb04bdb81151605db32d323a7632616`) and `source-after-restart.png`.

## Controlled test history

A bounded On-then-unchanged-Split test was prepared using documented T8214 family values and the existing SDK command route. It was **not executed**: an independent shared-stream outage at 18:35:25 caused the parent investigation to hold all setting writes. An inspector was opened at 18:36:21, then immediately closed at 18:36:37 when the hold arrived, before any heap query, observation hook or camera write. Port closure and SSH tunnel termination were verified. This window is excluded from packet-timing claims; the outage preceded it. No watermark or view settings were changed by this investigation.

The hold was subsequently released after the independent outage was attributed to an Intel video-engine hang. The On trial below supersedes the initial no-write status; it does not establish that the camera applied the attempted write.

### On trial: acceptance gate failed

Excluded inspector/command window: **18:41:34–18:42:08 UTC**. No bridge/SDK restart, cloud login or cloud refresh was performed. One logical existing-SDK command dispatch requested `{kind:"set-param",param:1214,value:2,form:"auto",channel:2}`. The SDK's normal redundant-send policy produced five actual level-2 sends:

| UTC | Actual command |
| --- | --- |
| 18:41:42.082 | Outer 1214, route channel 2, body channel 2, value 2, signCode 8; sent=true |
| 18:41:42.300 | Same |
| 18:41:42.515 | Same |
| 18:41:42.789 | Same |
| 18:41:42.990 | Same |

The observer retained only channel, value, command and timestamp; account identity and encryption material were omitted. The existing SDK route chose its `[u32LE channel][u32LE value][account-id padding]` level-2 body. The transport-return `sent=true` proves construction/submission, **not camera acceptance**.

No matching channel-2 command-1214 four-byte success response or realtime watermark observation was captured during the bounded trial. The cached value remained `"1"` with its earlier 18:14:53 observation. Independent software decoding of the simultaneous shared HTTP capture produced **187 frames at 1600×2200**. A whole frame more than eight media seconds into that capture still had **neither date/time nor Eufy logo**. Therefore the accepted-On/pixel baseline gate failed, and **no 6243 Split command was sent for the second phase**. Repeating or trying alternate wire encodings is not justified by this trial alone.

The cache timestamp histogram supplies an additional fact: **1214 alone** has observation time 18:14:53; the other parameters have different earlier timestamps. This is not a bulk refresh that stamped every property identically. It supports treating that watermark observation as distinct, but still does not prove the value changed or identify its writer.

All observer hooks were restored, including the original instance-property shape. The inspector was closed and port 9229 confirmed unreachable; its SSH tunnel was stopped. No persistent override, repeated setting reset or client overlay was added.

Private evidence: `controlled-on.json`, `source-on.h264`, `source-on-after.png`. The trial did not demonstrate an accepted setting change, does not validate the SDK's T8214 write encoding, and leaves the original missing-overlay cause unresolved.

| Trial artifact | SHA-256 |
| --- | --- |
| `controlled-on.json` | `c75dc5faf4079eb1a34f1618954c720b4dfd23a748684a91c7de9cc1bb4ac50a` |
| `source-on.h264` | `caeac8e7e5ec2f07c0c641e262b542cbd1a6dbfe2f3a0bcdc3de0ed60cb2df96` |
| `source-on-after.png` | `3d7d39a24665cc6926013bfe305206b95aba14f2ce6d4e3329412781e6d1dda7` |

Possible explanations for this trial's nonacceptance include model-specific command encoding, account permission, or a setting that is applied only on a later camera/live-session transition. The trial does not distinguish them. The bridge account's previously reported inability to change camera streaming quality is not proof of its watermark permission, and source/reference code alone does not resolve that permission.

### Later observation: persistent setting accepted, overlays still absent

An independently required GPU-path maintenance test stopped/restarted the bridge, then restored it at **18:48:16.796 UTC**. After that restore, a read-only inspector snapshot at **18:49:10.059–18:49:11.697** found raw camera watermark param **1214=`"2"`**, with observation timestamp **18:41:44 UTC**—two seconds after the On trial's actual sends. This later camera cloud record supports **persistent acceptance of the On request**. It supersedes a conclusion of overall write nonacceptance; the earlier twelve-second cache/response gate failed because it supplied no immediate success observation or visible On baseline.

The diagnostic command was routed directly through the existing SDK sink; it did not add an optimistic local parameter update or cloud write. No further watermark or view commands were sent by this investigation. The On setting was left in place as authorized.

Yet a simultaneous fresh whole-source HTTP capture after the restore produced **74 independently decoded frames at 1600×2200**, and the whole frame still had **no date/time and no Eufy logo**. Therefore an accepted `1214=2` setting did not restore either overlay in this sample, even after a fresh bridge/live-session opening. Neither a simple disabled-watermark explanation nor a blanket write-permission/encoding failure explains all observations. This does not identify the camera compositor's underlying trigger.

Permission diagnostics retained only `member_type=1` for camera and parent, and the boolean fact that their admin identities differ from the logged-in identity. No identities were output. The raw member type is not assigned an undocumented permission meaning. The successful later parameter observation is stronger evidence of this particular write's acceptance than the earlier speculation about account restrictions.

Excluded inspector window: **18:49:02–18:49:17 UTC**. No cloud login, cloud refresh or camera write occurred. Inspector closure, port refusal and SSH tunnel termination were verified.

| Later artifact | SHA-256 |
| --- | --- |
| `cached-osd-state-after-gpu-tests.json` | `f138d415af1b3b4caa2591e0bb030e75d19cf6d5eb465e711b6738311b03af90` |
| `source-after-gpu-tests.h264` | `59d89cb7c6369a9714f75d5036774ceea04e255358fc353a26dbd7eccd18d603` |
| `source-after-gpu-tests.png` | `7f4d1a44e37a0e5700f54920659046e65244e7e73985b0ea81b336084b481895` |

## Remaining evidence required

Correlate subsequent actual source-overlay transitions with property notifications and outgoing settings commands. Confirm the T8214-specific watermark value interpretation with actual app wire evidence before changing SDK semantics. If the reported watermark remains enabled while source pixels omit it, that supports a camera compositor/firmware defect; if the property changes to disabled, identify the command or camera state transition that changed it. Do not add a client-generated clock, continually reset view/OSD settings, or claim a firmware update fixes this camera without verification.
