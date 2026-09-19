# Spike A — real-device probe (throwaway)

Run from `server/` with a `config.yaml` (or env creds). Node ≥ 24.5.

    node spikes/spike-a.mjs                       # all cameras: codec, geometry, 10 s dump each, peer IPs
    node spikes/spike-a.mjs --sn T8214XXX --dual 12   # E340: set split-view, then dump → record resolution
    node spikes/spike-a.mjs --quality "Full HD (1080P)"   # does the SDK quality write land? (expect "failed: wire unverified" on 0.1.1)

Record the answers in docs/runbook-server.md → "Verified devices":
- per camera: model, wired/battery, codec, WxH, p2p peer IP (must be in lan.cidr)
- E340 in view 12: WxH and whether the frame is stacked (open the .h264 with `ffplay -f h264 file`)
- whether the dual-view command took effect (compare a dump before/after)
- eufy-sdk issue #200 (T8214 stream fails): does the doorbell stream at all?
