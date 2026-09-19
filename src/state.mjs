// One mutable runtime-state object shared through ctx (mirrors upstream's state.mjs so the vendored
// auth/watchdog modules find the flags they expect). Our additions: per-camera stream slots + LAN blocks.
export function createState() {
  return {
    flags: {
      ready: false,
      sessionLost: false,
      lastLogin: undefined,
      booting: false,
      recovering: false,
      lastActivity: Date.now(),
      pushConnected: false,
      pushSince: Date.now(),
      go2rtcProc: undefined,
    },
    timers: { watchdog: null, stream: null },
    streaming: new Set(), // sns with a live feed right now (upstream name; read by vendored code paths)
    slots: new Map(), // sn -> Slot (stream-manager.mjs)
    blocked: new Map(), // sn -> reason (lan-guard.mjs), e.g. "wan-path 203.0.113.9"
  };
}

export function newSlot(sn) {
  return {
    sn,
    feed: undefined,
    client: undefined,
    lastBytesAt: 0,
    startedAt: 0,
    firstFailureAt: 0,
    failures: 0,
    backoffIdx: 0,
    restartTimer: null,
    consumers: new Set(),
    codec: undefined,
    width: undefined,
    height: undefined,
    lastKeyChunk: undefined,
    stalls: 0,
  };
}
