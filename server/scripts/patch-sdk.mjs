#!/usr/bin/env node
// The SDK patches this script used to apply now live in the pinned fork
// (github:sapireli/eufy-sdk#eufy-wall) as real source changes, each also open as an upstream PR:
//
//   - multi-socket hole punch          → mega-yfue/eufy-sdk#211
//   - use the device's retransmissions → mega-yfue/eufy-sdk#212
//   - forward warm-up options          → mega-yfue/eufy-sdk#213
//   - lanOnlyForStation (P2P-only)     → mega-yfue/eufy-sdk#214
//
// Nothing is patched into node_modules any more, so an `npm install` no longer silently reverts them.
//
// This file is deliberately kept as a no-op rather than deleted: it previously also applied a
// brute-force CHECK_CAM port sweep across all 65535 ports, which the user DEFERRED. Leaving the script
// present and empty makes that explicit, so it is not resurrected by accident. See docs/handoff/.
console.log("[patch-sdk] no-op — SDK fixes live in the pinned fork (see this file's header)");
