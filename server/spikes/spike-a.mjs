// Throwaway probe. Usage: EUFY_EMAIL=… EUFY_PASSWORD=… node spikes/spike-a.mjs [--sn T8214X --dual 12] [--quality "Full HD (1080P)"]
import { writeFileSync, mkdirSync } from "node:fs";
import { createInterface } from "node:readline/promises";
import { loadConfig } from "../src/config.mjs";
import { createSdk } from "../src/sdk-adapter.mjs";

const args = Object.fromEntries(process.argv.slice(2).map((a, i, all) => (a.startsWith("--") ? [a.slice(2), all[i + 1]] : [])).filter((x) => x.length));
const { cfg } = loadConfig({ configPath: process.env.BRIDGE_CONFIG || "./config.yaml" });
const { eufy, sdk } = createSdk({ cfg, DEBUG: false });
const rl = createInterface({ input: process.stdin, output: process.stdout });

let r = await eufy.login();
while (r.status !== sdk.LoginStatus.Ok) {
  if (r.status === sdk.LoginStatus.Captcha) {
    writeFileSync("captcha.png", Buffer.from(r.image.split(",")[1], "base64"));
    r = await eufy.solveCaptcha(await rl.question("captcha written to captcha.png — answer: "));
  } else r = await eufy.submitVerifyCode(await rl.question(`2FA code (${r.method}): `));
}
console.log("login ok");
mkdirSync("spike-out", { recursive: true });

for (const d of await eufy.getDevices()) {
  const m = await sdk.describe(d.sn).catch((e) => ({ sn: d.sn, error: e.message }));
  console.log(JSON.stringify(m));
  if (!m.isCamera) continue;
  if (args.sn && args.sn !== m.sn) continue;
  const client = await sdk.streamClientFor(m.sn);
  client.on("p2pConnect", (st) => console.log(`  ${m.sn}: p2pConnect ${st} peer=${sdk.sessionPeerHost(client, st) ?? "?"}`));
  if (args.dual && m.sn === args.sn) {
    await sdk.sendSetPayload(client, m.sn, 6243, { restore: 1, video_type: Number(args.dual) });
    console.log(`  ${m.sn}: sent dual view ${args.dual}`);
  }
  if (args.quality) await eufy.setProperty(m.sn, "streamingQuality", args.quality).then(() => console.log("  quality set")).catch((e) => console.log(`  quality set failed: ${e.message}`));
  try {
    const feed = await sdk.openFeed(client, m.sn);
    const out = [];
    let codec, geom;
    const t = setTimeout(() => feed.destroy(), 10_000);
    for await (const chunk of feed) {
      out.push(chunk);
      const sets = sdk.extractParamSets(chunk);
      if (sets && !codec) { codec = sets.codec; geom = sdk.codedGeometry(sets); }
    }
    clearTimeout(t);
    const file = `spike-out/${m.sn}.${codec === "h265" ? "h265" : "h264"}`;
    writeFileSync(file, Buffer.concat(out));
    console.log(`  ${m.sn}: codec=${codec} ${geom?.width}x${geom?.height} bytes=${Buffer.concat(out).length} → ${file} (verify: ffprobe ${file})`);
  } catch (e) {
    console.log(`  ${m.sn}: stream failed: ${e.message}`);
  }
}
await sdk.closeStreamClients();
await eufy.disconnect();
process.exit(0);
