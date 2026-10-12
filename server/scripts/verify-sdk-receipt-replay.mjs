// Real SDK source and bridge stamping, synthetic transport, no device/network access.
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { resolve, join } from "node:path";
import { EventEmitter } from "node:events";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { createFreshness } from "../src/freshness.mjs";

const sdkRoot = resolve(process.argv[2] ?? "");
if (!process.argv[2]) throw new Error("Usage: node verify-sdk-receipt-replay.mjs /path/to/eufy-sdk");
const { build } = createRequire(join(sdkRoot, "package.json"))("esbuild");
const bundled = await build({ stdin: {
  contents: "export {SharedLiveSource} from './src/transport/p2p/shared-live-source.ts'; export {openReadableFromConsumer} from './src/transport/p2p/readable-egress.ts';",
  resolveDir: sdkRoot,
}, bundle: true, platform: "node", format: "cjs", write: false });
const module = { exports: {} };
new Function("require", "module", "exports", bundled.outputFiles[0].text)(createRequire(import.meta.url), module, module.exports);
const { SharedLiveSource, openReadableFromConsumer } = module.exports;
const stream = new EventEmitter();
stream.start = () => stream;
stream.stop = () => {};
stream.requestKeyframe = () => {};
stream.sendStart = () => {};
const source = new SharedLiveSource({ makeStream: () => stream, lingerMs: 8000, warmRetryMs: 2000, warmTimeoutMs: 20000 });
let offsetUs = 0n;
const now = () => process.hrtime.bigint() / 1000n + offsetUs;
const freshness = createFreshness({ cfg: { freshness: {} } }, { clock: now });
let first, second;
const frames = [];
const frame = { codec: "h264", width: 16, height: 16, keyframe: true,
  sourceTimestamp: 123456, data: Buffer.from([0, 0, 0, 1, 0x65, 1, 2, 3]) };
function attach() {
  const readable = openReadableFromConsumer(source.attach(), { objectMode: true, receiptMetadata: true });
  readable.on("error", (error) => { throw error; });
  readable.on("data", (item) => frames.push({ item, stamp: freshness.record(item.frame, item.receipt) }));
  return readable;
}
try {
  first = attach();
  stream.emit("video", frame);
  await new Promise((done) => setImmediate(done));
  assert.equal(frames.length, 1);
  first.destroy();
  // Advance only the bridge observation clock; the SDK's real monotonic receipt
  // remains unchanged. No sleeping, wall-clock reconstruction, or device is used.
  offsetUs = 5_000_000n;
  second = attach();
  await new Promise((done) => setImmediate(done));
  assert.equal(frames.length, 2);
  assert.equal(frames[1].item.frame, frame);
  assert.equal(frames[1].item.receipt, frames[0].item.receipt);
  assert.ok(Object.isFrozen(frames[1].item.receipt));
  assert.equal(frames[1].stamp, frames[0].stamp);
  assert.equal(frames[1].stamp.ingressMonoUs, frames[0].item.receipt.monotonicUs);
  assert.ok(now() - frames[1].stamp.ingressMonoUs >= 5_000_000n);
  console.log(JSON.stringify({ scope: "offline-real-SDK-source-and-bridge", liveDevice: false,
    sdkSourceHead: execFileSync("git", ["-C", sdkRoot, "rev-parse", "HEAD"], { encoding: "utf8" }).trim(),
    bundledSourceSha256: createHash("sha256").update(bundled.outputFiles[0].text).digest("hex"),
    sourceId: frames[0].item.receipt.sourceId, sourceSequence: String(frames[0].item.receipt.sequence),
    originalMonotonicUs: String(frames[0].item.receipt.monotonicUs),
    replayedMonotonicUs: String(frames[1].stamp.ingressMonoUs),
    frameIds: frames.map(({ stamp }) => String(stamp.frameId)),
    observedReplayAgeUs: String(now() - frames[1].stamp.ingressMonoUs) }));
} finally {
  first?.destroy();
  second?.destroy();
  source.dispose();
}
