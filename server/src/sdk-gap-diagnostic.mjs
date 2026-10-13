import fs from "node:fs";
import { isAbsolute } from "node:path";

// Opt-in D5-layout observer; never creates a media consumer or changes SDK state.
export function initializeSdkGapDiagnostic(ctx, {
  env = process.env, io = fs, setIntervalFn = setInterval, clearIntervalFn = clearInterval,
  setTimer = setTimeout, clearTimer = clearTimeout, nowUs = () => process.hrtime.bigint() / 1000n,
  wallMs = Date.now, maxRows = 1200, warn = message => console.warn(`[sdk-gap-diagnostic] ${message}`),
} = {}) {
  if (!env.EWB_GAP_DIAGNOSTIC_FILE) return () => {};
  const seconds = Number(env.EWB_GAP_DIAGNOSTIC_SECONDS ?? 300);
  if (!isAbsolute(env.EWB_GAP_DIAGNOSTIC_FILE) || !Number.isInteger(seconds) || seconds < 1 || seconds > 300 ||
      !Number.isInteger(maxRows) || maxRows < 1 || maxRows > 1200) { warn("disabled: invalid bounded output configuration"); return () => {}; }
  const router = ctx.eufy?.p2p;
  if (!(router?.liveSources instanceof Map) || !(router?.liveSessionKeys instanceof Map) ||
      typeof ctx.eufy?.getP2pSessions !== "function" || !(ctx.state?.slots instanceof Map)) {
    warn("disabled: required D5 metadata shape absent"); return () => {};
  }
  let fd;
  try { fd = io.openSync(env.EWB_GAP_DIAGNOSTIC_FILE, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, 0o600); }
  catch { warn("disabled: exclusive private output unavailable"); return () => {}; }
  let stopped = false, rows = 0, interval, timer;
  const streams = new Map(), sessions = new Map(), restores = [], stationIds = new Map();
  const mono = () => String(nowUs());
  function stop(reason = "requested") {
    if (stopped) return;
    stopped = true;
    clearIntervalFn(interval); clearTimer(timer);
    for (const restore of restores.reverse()) { try { restore(); } catch {} }
    if (rows < maxRows) {
      try { io.writeSync(fd, JSON.stringify({ kind: "end", reason, monotonicUs: mono(), wallMs: wallMs(), rows }) + "\n"); } catch {}
    }
    try { io.closeSync(fd); } catch {}
  }
  function put(record) {
    if (stopped) return;
    try {
      const bytes = Buffer.from(JSON.stringify({ monotonicUs: mono(), wallMs: wallMs(), ...record }) + "\n");
      if (io.writeSync(fd, bytes) !== bytes.length) throw Error("incomplete write");
      if (++rows >= maxRows) stop("row-cap");
    } catch { stop("write-error"); }
  }
  function snapshot(info) {
    const receiptId = info.source.receiptSourceId;
    let match;
    for (const [sn, slot] of ctx.state.slots) if (receiptId && slot.lastSourceReceipt?.sourceId === receiptId) { match = { sn, slot }; break; }
    const slot = match?.slot;
    const counters = slot?.freshness;
    return {
      stream: info.id, session: info.session.id, sourceId: typeof receiptId === "string" ? receiptId : null,
      camera: match ? String(ctx.getCamera?.(match.sn)?.name ?? "matched-camera").slice(0, 80) : null,
      channel: info.owner.channel, mediaChannel: info.owner.mediaChannel ?? null,
      active: info.source.stream === info.owner, waitingKeyframe: info.owner.awaitingKeyframeAfterGap,
      acceptedData: info.acceptedData, unknownChannelData: info.unknownChannelData,
      assembled: info.assembled, gatedCompleteUnits: info.gated, delivered: info.delivered,
      keyframes: info.keyframes, lastDeliveredUs: info.lastDeliveredUs, lastSourceTimestamp: info.lastSourceTimestamp,
      ingress: slot?.lastSourceReceipt ? { sourceId: slot.lastSourceReceipt.sourceId,
        sequence: String(slot.lastSourceReceipt.sequence), monotonicUs: String(slot.lastSourceReceipt.monotonicUs),
        frameId: slot.lastReceipt ? String(slot.lastReceipt.frameId) : null } : null,
      encodedHttp: counters ? { inputFrames: counters.inputFrames, outputFrames: counters.outputFrames,
        staleFrames: counters.staleFrames, unmappedFrames: counters.unmappedFrames } : null,
    };
  }
  function registerSession(session) {
    if (sessions.has(session)) return sessions.get(session);
    if (sessions.size >= 64 || typeof session?.on !== "function" || typeof session?.off !== "function") throw Error("session shape/cap");
    const info = { id: `session-${sessions.size + 1}`, owner: session, gaps: 0, data: 0, channels: {} };
    sessions.set(session, info);
    const onData = frame => {
      if (stopped || frame?.commandId !== 1300) return;
      const channel = frame.channel;
      if (!Number.isInteger(channel) || channel < 0 || channel > 255) return;
      info.data++; info.channels[channel] = (info.channels[channel] ?? 0) + 1;
      for (const s of streams.values()) if (s.session === info && s.source.stream === s.owner) {
        if (s.owner.mediaChannel === undefined) s.unknownChannelData++;
        else if (s.owner.mediaChannel === channel) s.acceptedData++;
      }
    };
    // Appended after LiveStream's synchronous gate handlers; flags are post-gap.
    const onGap = () => {
      if (stopped) return;
      info.gaps++;
      put({ kind: "video-gap", session: info.id, gaps: info.gaps,
        listenerCount: session.listenerCount("videoGap"),
        members: [...streams.values()].filter(s => s.session === info && s.source.stream === s.owner).map(snapshot) });
    };
    info.onGap = onGap;
    session.on("data", onData); session.on("videoGap", onGap);
    restores.push(() => { session.off("data", onData); session.off("videoGap", onGap); });
    return info;
  }
  function registerStream(source) {
    const owner = source.stream;
    if (!owner || streams.has(owner)) return;
    if (streams.size >= 32 || typeof owner.awaitingKeyframeAfterGap !== "boolean" ||
        typeof owner.units?.push !== "function" || typeof owner.on !== "function" || typeof owner.off !== "function" ||
        !Number.isInteger(owner.channel) || !owner.session) throw Error("stream D5 shape/cap");
    const session = registerSession(owner.session);
    // A control session may have been inventoried before this LiveStream started.
    // Move our listener behind newly installed SDK synchronous gate handlers.
    owner.session.off("videoGap", session.onGap); owner.session.on("videoGap", session.onGap);
    const info = { id: `stream-${streams.size + 1}`, owner, source, session, acceptedData: 0, unknownChannelData: 0,
      assembled: 0, gated: 0, delivered: 0, keyframes: 0, lastDeliveredUs: null, lastSourceTimestamp: null };
    streams.set(owner, info);
    const assembler = owner.units, descriptor = Object.getOwnPropertyDescriptor(assembler, "push"), original = assembler.push;
    const replacement = function (...args) {
      const waitingBefore = owner.awaitingKeyframeAfterGap;
      const result = original.apply(this, args); // Original exception/result/receiver preserved.
      if (!stopped) {
        try {
          if (!Array.isArray(result)) throw Error("assembler result shape");
          let waiting = waitingBefore;
          for (const unit of result) {
            info.assembled++;
            if (waiting && !unit.keyframe) info.gated++;
            if (waiting && unit.keyframe) waiting = false;
          }
        } catch { stop("assembler-shape"); }
      }
      return result;
    };
    assembler.push = replacement;
    restores.push(() => { if (assembler.push === replacement) { if (descriptor) Object.defineProperty(assembler, "push", descriptor); else delete assembler.push; } });
    const onVideo = frame => {
      if (stopped) return;
      info.delivered++; if (frame?.keyframe) info.keyframes++;
      info.lastDeliveredUs = mono(); info.lastSourceTimestamp = Number.isInteger(frame?.sourceTimestamp) ? frame.sourceTimestamp : null;
    };
    owner.on("video", onVideo); restores.push(() => owner.off("video", onVideo));
  }
  function sample() {
    if (stopped) return;
    try {
      const publicSessions = ctx.eufy.getP2pSessions();
      if (!(publicSessions instanceof Map)) throw Error("public session map");
      for (const source of router.liveSources.values()) registerStream(source);
      const inventory = [];
      for (const [key, session] of publicSessions) {
        const info = registerSession(session), split = String(key).split("#live:");
        if (!stationIds.has(split[0])) stationIds.set(split[0], `station-${stationIds.size + 1}`);
        inventory.push({ session: info.id, station: stationIds.get(split[0]), keyKind: split.length === 1 ? "base" : "dedicated-media",
          keyChannel: split.length === 1 ? null : Number(split[1]), videoGaps: info.gaps, data: info.data, channels: { ...info.channels } });
      }
      put({ kind: "sample", serverEpoch: ctx.freshness?.clock?.().serverEpoch ?? null,
        inventory, streams: [...streams.values()].filter(s => s.source.stream === s.owner).map(snapshot) });
    } catch { stop("D5-shape-unavailable"); }
  }
  put({ kind: "start", seconds, scope: "SDK data/assembled/delivered and encoded-HTTP submission; no socket/scanout guarantee" });
  sample();
  if (!stopped) {
    interval = setIntervalFn(sample, 1000); timer = setTimer(() => stop("duration"), seconds * 1000);
    interval.unref?.(); timer.unref?.();
  }
  return stop;
}
