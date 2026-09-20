// Holds decide when a camera that is not always-on should be streaming.
//
// Motion does not "start a stream" — it takes a HOLD on a camera, and a camera streams for exactly as
// long as it has at least one. That indirection is what keeps three things consistent that otherwise
// drift apart: the SDK's battery budget (extend while held, let it stop when not), the state a client is
// told over the WS, and a tile deciding whether to show video or a placeholder.
//
// Holds have an owner so they compose. Motion takes one owned by "motion"; a client watching a tile takes
// one owned by that client. A camera held by both stays up until both expire, and neither owner has to
// know about the other.
//
// A hold is always bounded. An unbounded hold on a battery camera is the failure this mode exists to
// prevent, so `seconds` is required and further motion extends the deadline rather than pinning it open.

const TICK_MS = 1000;

export function createHolds(ctx) {
  const { cfg, state } = ctx;
  // sn -> Map<owner, expiresAtMs>
  const held = new Map();
  state.holds = held;

  const now = () => Date.now();
  const cameraOf = (sn) => ctx.getCamera?.(sn);

  /** Every owner still holding `sn`, dropping any that have expired. */
  function owners(sn) {
    const m = held.get(sn);
    if (!m) return [];
    for (const [owner, until] of m) if (until <= now()) m.delete(owner);
    if (!m.size) held.delete(sn);
    return [...(m?.keys() ?? [])];
  }

  const isHeld = (sn) => owners(sn).length > 0;

  /** When the last hold on `sn` expires, or 0 if it is not held. */
  function heldUntil(sn) {
    const m = held.get(sn);
    if (!m?.size) return 0;
    return Math.max(...m.values());
  }

  /**
   * Take or extend a hold. Returns the new expiry.
   *
   * Extending is deliberately "latest wins" rather than additive: repeated motion during an event should
   * keep a camera up for `seconds` past the LAST movement, not accumulate a longer and longer stream.
   */
  function hold(sn, owner, seconds) {
    const cam = cameraOf(sn);
    if (!cam?.enabled) return 0;
    const secs = Number(seconds) > 0 ? Number(seconds) : (cam.holdSeconds ?? cfg.defaults.holdSeconds);
    const until = now() + secs * 1000;
    const m = held.get(sn) ?? held.set(sn, new Map()).get(sn);
    const had = m.size > 0;
    m.set(owner, Math.max(until, m.get(owner) ?? 0));
    ctx.broadcastEvent?.({ type: "hold", sn, until: heldUntil(sn), owners: owners(sn) });
    if (!had) {
      console.log(`[bridge] ${sn}: held by ${owner} for ${secs}s — starting stream`);
      void ctx.ensureWarm?.(sn);
    }
    return heldUntil(sn);
  }

  /** Drop one owner's hold. The stream stops only once nobody is holding it. */
  function release(sn, owner) {
    const m = held.get(sn);
    if (!m?.delete(owner)) return;
    ctx.broadcastEvent?.({ type: "hold", sn, until: heldUntil(sn), owners: owners(sn) });
    if (!m.size) stop(sn, "released");
  }

  function stop(sn, why) {
    held.delete(sn);
    const cam = cameraOf(sn);
    if (!cam || cam.mode === "always") return; // an always-on camera is never stopped by a hold expiring
    console.log(`[bridge] ${sn}: ${why} — stopping stream`);
    ctx.stopCamera?.(sn);
  }

  /**
   * The SDK bounds a battery camera's continuous stream and warns before it auto-stops. Extend only while
   * something is holding the camera: with no hold the notice is the SDK correctly observing that a
   * battery camera has been streaming for a while and nothing wants it.
   */
  function onBudgetNotice(sn, notice) {
    if (!isHeld(sn)) {
      console.log(`[bridge] ${sn}: battery budget reached and nothing holds it — letting it stop`);
      return false;
    }
    console.log(`[bridge] ${sn}: battery budget reached but still held — extending`);
    try {
      notice?.extend?.();
    } catch (e) {
      console.error(`[bridge] ${sn}: extend failed: ${e?.message ?? e}`);
      return false;
    }
    return true;
  }

  /** Expire holds whose deadline has passed. */
  function tick() {
    for (const sn of [...held.keys()]) {
      if (owners(sn).length === 0) stop(sn, "hold expired");
    }
  }

  function status() {
    const out = {};
    for (const sn of held.keys()) {
      const o = owners(sn);
      if (o.length) out[sn] = { owners: o, untilMs: heldUntil(sn) - now() };
    }
    return out;
  }

  function start() {
    state.timers.holds ??= setInterval(tick, TICK_MS);
  }

  return { hold, release, isHeld, heldUntil, owners, onBudgetNotice, tick, status, start };
}
