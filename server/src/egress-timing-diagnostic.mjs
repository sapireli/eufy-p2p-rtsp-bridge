import fs from "node:fs";
import { isAbsolute } from "node:path";

// Explicit, short-lived metadata observation. No hook, file, or timer by default.
export function initializeEgressTiming(ctx, {
  env = process.env, io = fs, setTimer = setTimeout, clearTimer = clearTimeout,
  warn = message => console.warn(`[bridge-egress-timing] ${message}`),
} = {}) {
  const path = env.EGRESS_TIMING_FILE;
  if (!path) return () => {};
  const duration = Number(env.EGRESS_TIMING_SECONDS ?? 30);
  if (!isAbsolute(path) || !Number.isFinite(duration) || duration <= 0 || duration > 45) {
    warn("disabled: absolute output path and duration 0 < seconds <= 45 required");
    return () => {};
  }
  let fd;
  try { fd = io.openSync(path, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, 0o600); }
  catch (error) { warn(`disabled: exclusive private file open failed (${error.code ?? "error"})`); return () => {}; }
  let stopped = false, rows = 0, startTimer, endTimer;
  function stop() {
    if (stopped) return;
    stopped = true;
    ctx.freshEgressTimingActive = false;
    clearTimer(startTimer); clearTimer(endTimer);
    try { io.closeSync(fd); } catch {}
  }
  ctx.freshEgressTimingActive = false;
  ctx.maxFreshEgressTimingRecords = 256;
  ctx.onFreshEgressTiming = record => {
    if (stopped || ctx.freshEgressTimingActive !== true) return;
    try {
      const bytes = Buffer.from(JSON.stringify(record) + "\n");
      if (io.writeSync(fd, bytes) !== bytes.length) throw new Error("incomplete diagnostic write");
      rows++;
      if (rows >= 256) stop();
    } catch { stop(); warn("disabled: diagnostic output write failed"); }
  };
  // Delay observation past initial startup. Neither timer keeps the service alive.
  startTimer = setTimer(() => { if (!stopped) ctx.freshEgressTimingActive = true; }, 15000);
  endTimer = setTimer(stop, 15000 + duration * 1000);
  startTimer.unref?.(); endTimer.unref?.();
  return stop;
}
