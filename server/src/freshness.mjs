import { randomBytes } from "node:crypto";

/** Clock domain for receipt at this bridge, independent of host wall-clock corrections. */
export function createFreshness(ctx, { epoch = randomBytes(8), clock = () => process.hrtime.bigint() / 1000n } = {}) {
  let sequence = 0n;
  const enabled = ctx.cfg.freshness?.enabled !== false;
  const maxAgeMs = ctx.cfg.freshness?.maxAgeMs ?? 500;
  const mode = ctx.cfg.freshness?.mode ?? "observe";
  if (!["observe", "enforce"].includes(mode)) throw new Error("freshness.mode must be observe or enforce");
  if (!Number.isInteger(maxAgeMs) || maxAgeMs < 100 || maxAgeMs > 60000) throw new Error("freshness.max_age_ms must be an integer from 100 to 60000");
  if (!Buffer.isBuffer(epoch) || epoch.length !== 8) throw new Error("freshness epoch must contain 8 bytes");
  const serverEpoch = Buffer.from(epoch);
  function enabledFor(cam) {
    if (!enabled) return false;
    const mode = cam?.transcode ?? ctx.cfg.go2rtcTranscode;
    return mode === "never" || (ctx.platform ?? process.platform) === "linux";
  }
  return {
    enabledFor,
    nowUs: clock,
    clock: () => ({ serverEpoch: serverEpoch.toString("hex"), monotonicUs: Number(clock()), scope: "bridge-ingress", captureClockValidated: false }),
    capability: (cam, host) => enabledFor(cam) ? {
      version: 2, mode, scope: "bridge-ingress", maxAgeMs, captureClockValidated: false,
      clockUrl: `http://${host}:${ctx.cfg.port}/api/clock`,
    } : undefined,
    record: (frame) => ({ serverEpoch, sourceTimestamp: frame.sourceTimestamp, ingressMonoUs: clock(), frameId: ++sequence, maxAgeMs, scope: 1 }),
  };
}
