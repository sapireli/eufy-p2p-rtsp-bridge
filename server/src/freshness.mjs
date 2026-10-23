import { urlHost } from "./network.mjs";
import { randomBytes } from "node:crypto";

/** Monotonic bridge clock; SDK wall-clock arrivals are projected into this domain before fanout. */
export function createFreshness(ctx, { epoch = randomBytes(8), clock = () => process.hrtime.bigint() / 1000n, wallClock = Date.now } = {}) {
  let sequence = 0n;
  // The SDK retains this immutable receipt through queues and keyframe replay.
  // Weak keys retain no completed frame once the SDK releases it.
  const sourceReceipts = new WeakMap();
  const arrivalReceipts = new WeakMap();
  let arrivalSequence = 0n;
  let arrivalClockSample;
  const enabled = ctx.cfg.freshness?.enabled !== false;
  const maxAgeMs = ctx.cfg.freshness?.maxAgeMs ?? 500;
  const mode = ctx.cfg.freshness?.mode ?? "observe";
  if (!["observe", "enforce"].includes(mode)) throw new Error("freshness.mode must be observe or enforce");
  if (!Number.isInteger(maxAgeMs) || maxAgeMs < 100 || maxAgeMs > 60000) throw new Error("freshness.max_age_ms must be an integer from 100 to 60000");
  if (!Buffer.isBuffer(epoch) || epoch.length !== 8) throw new Error("freshness epoch must contain 8 bytes");
  const serverEpoch = Buffer.from(epoch);
  function enabledFor(cam) {
    if (!enabled || cam?.external) return false;
    const mode = cam?.transcode ?? ctx.cfg.go2rtcTranscode;
    return mode === "never" || (ctx.platform ?? process.platform) === "linux";
  }
  return {
    enabledFor,
    nowUs: clock,
    /** Project the SDK's original wall-clock arrival into the bridge clock, before any fanout. */
    sourceReceipt: ({ frame, timestampMs }) => {
      if (!frame || typeof frame !== "object" || !Number.isSafeInteger(timestampMs) || timestampMs < 0) {
        throw new Error("invalid SDK source arrival time");
      }
      const retained = arrivalReceipts.get(frame);
      if (retained) {
        if (retained.timestampMs !== timestampMs) throw new Error("SDK frame arrival time changed");
        return retained.receipt;
      }
      const wallMs = wallClock(), monoUs = clock();
      const previous = arrivalClockSample;
      arrivalClockSample = { wallMs, monoUs };
      if (previous && Math.abs((wallMs - previous.wallMs) - Number(monoUs - previous.monoUs) / 1000) > maxAgeMs) {
        throw new Error("SDK arrival wall clock changed beyond the freshness budget");
      }
      const ageMs = wallMs - timestampMs;
      const monotonicUs = monoUs - BigInt(ageMs) * 1000n;
      if (ageMs < 0 || monotonicUs < 0n) throw new Error("SDK source arrival time is outside the bridge clock");
      const receipt = Object.freeze({ sourceId: `arrival-${serverEpoch.toString("hex")}`,
        sequence: ++arrivalSequence, monotonicUs });
      arrivalReceipts.set(frame, { timestampMs, receipt });
      return receipt;
    },
    clock: () => ({ serverEpoch: serverEpoch.toString("hex"), monotonicUs: Number(clock()), scope: "bridge-ingress", captureClockValidated: false }),
    capability: (cam, host) => enabledFor(cam) ? {
      version: 2, mode, scope: "bridge-ingress", maxAgeMs, captureClockValidated: false,
      clockUrl: `http://${urlHost(host)}:${ctx.cfg.port}/api/clock`,
    } : undefined,
    record: (frame, receipt) => {
      if (receipt !== undefined) {
        if (!receipt || typeof receipt.sourceId !== "string" || !receipt.sourceId ||
            typeof receipt.sequence !== "bigint" || receipt.sequence <= 0n ||
            typeof receipt.monotonicUs !== "bigint" || receipt.monotonicUs < 0n || receipt.monotonicUs > clock()) {
          throw new Error("invalid SDK monotonic source receipt");
        }
        const retained = sourceReceipts.get(receipt);
        if (retained) return retained;
      }
      const stamped = Object.freeze({ serverEpoch, sourceTimestamp: frame.sourceTimestamp,
        ingressMonoUs: receipt?.monotonicUs ?? clock(), frameId: ++sequence, maxAgeMs, scope: 1 });
      if (receipt) sourceReceipts.set(receipt, stamped);
      return stamped;
    },
  };
}
