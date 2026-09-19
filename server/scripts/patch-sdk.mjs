#!/usr/bin/env node
// Idempotent postinstall patch for @mega-yfue/eufy-sdk.
//
// The SDK has no HomeBase-3 local-connect path: the cloud only exposes a NAT-translated P2P port, and
// the device's real local port is a per-session mapping bound to the connecting socket. This patch makes
// the P2P session, when a LAN address is pinned (localAddresses), sweep CHECK_CAM across all local ports
// ON ITS OWN SOCKET so the CAM_ID reply lands on it and the normal onConnected path fires — a pure-LAN
// session. See docs/hb3-local-port.md. Re-run automatically on `npm install`; safe to run repeatedly.
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

let entry;
try {
  entry = fileURLToPath(import.meta.resolve("@mega-yfue/eufy-sdk"));
} catch {
  console.warn("[patch-sdk] @mega-yfue/eufy-sdk not resolvable yet — skipping");
  process.exit(0);
}

const MARK = "_ewLocalSwept";
let src = readFileSync(entry, "utf8");
if (src.includes(MARK)) {
  console.log("[patch-sdk] local-port sweep patch already present");
  process.exit(0);
}

const ORIGINAL =
  `    if (this.cfg.localAddress)\n` +
  `      this.send({ host: this.cfg.localAddress, port: LOCAL_LOOKUP_PORT }, RequestMessageType.LOCAL_LOOKUP, localPayload);\n`;

const PATCHED =
  `    if (this.cfg.localAddress) {\n` +
  `      const [lh] = String(this.cfg.localAddress).split(":");\n` +
  `      this.send({ host: lh, port: LOCAL_LOOKUP_PORT }, RequestMessageType.LOCAL_LOOKUP, localPayload);\n` +
  `      // eufy-wall patch: HomeBase 3 does not answer LOCAL_LOOKUP and the cloud only exposes a\n` +
  `      // NAT-translated port; the real local P2P port is ephemeral and bound to whichever socket first\n` +
  `      // reaches it. Sweep CHECK_CAM across ALL local ports ON THIS SESSION'S OWN SOCKET so the CAM_ID\n` +
  `      // reply lands here and the normal onConnected path fires — a pure-LAN session.\n` +
  `      if (!this._ewLocalSwept) {\n` +
  `        this._ewLocalSwept = true;\n` +
  `        const camPayload = buildCheckCamPayload(this.cfg.p2pDid);\n` +
  `        let port = 1;\n` +
  `        const burst = () => {\n` +
  `          if (this.connected || this.closed || port > 65535) return;\n` +
  `          const end = Math.min(port + 2048, 65536);\n` +
  `          for (; port < end; port++) this.send({ host: lh, port }, RequestMessageType.CHECK_CAM, camPayload);\n` +
  `          setTimeout(burst, 20);\n` +
  `        };\n` +
  `        burst();\n` +
  `      }\n` +
  `    }\n`;

if (!src.includes(ORIGINAL)) {
  console.warn(
    "[patch-sdk] could not find the expected sendLookups block — the SDK version may have changed. " +
      "Local HomeBase-3 streaming will NOT work until the patch is updated (see docs/hb3-local-port.md).",
  );
  process.exit(0); // don't fail the install
}

src = src.replace(ORIGINAL, PATCHED);
writeFileSync(entry, src);
console.log("[patch-sdk] applied local-port sweep patch to @mega-yfue/eufy-sdk");
