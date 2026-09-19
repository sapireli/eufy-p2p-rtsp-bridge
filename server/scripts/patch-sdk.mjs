#!/usr/bin/env node
// Idempotent postinstall patches for @mega-yfue/eufy-sdk. Re-run automatically on `npm install`; safe to
// run repeatedly (each patch is skipped when its marker is already present). If a block can't be found the
// SDK version likely changed — we warn and exit 0 (never fail the install). See docs/hb3-local-port.md.
//
// 1. local-port sweep — HomeBase 3 doesn't answer LOCAL_LOOKUP and the cloud only exposes a NAT'd port;
//    the real local port is ephemeral and socket-bound, so sweep CHECK_CAM across all local ports on the
//    session's own socket until CAM_ID arrives (pure-LAN connect).
// 2. multi-channel — the SDK refuses a second camera on a station session ("one camera at a time"), but
//    that's a client-side pre-check: an HB3 serves multiple channels on one session (verified live), so a
//    wall shows co-located cameras at once with no extra IPs. Bypass the refusal unless opted out.
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

let entry;
try {
  entry = fileURLToPath(import.meta.resolve("@mega-yfue/eufy-sdk"));
} catch {
  console.warn("[patch-sdk] @mega-yfue/eufy-sdk not resolvable yet — skipping");
  process.exit(0);
}

const SWEEP_ORIGINAL =
  `    if (this.cfg.localAddress)\n` +
  `      this.send({ host: this.cfg.localAddress, port: LOCAL_LOOKUP_PORT }, RequestMessageType.LOCAL_LOOKUP, localPayload);\n`;

const SWEEP_PATCHED =
  `    if (this.cfg.localAddress) {\n` +
  `      const [lh] = String(this.cfg.localAddress).split(":");\n` +
  `      this.send({ host: lh, port: LOCAL_LOOKUP_PORT }, RequestMessageType.LOCAL_LOOKUP, localPayload);\n` +
  `      // eufy-wall patch: HomeBase 3 does not answer LOCAL_LOOKUP and the cloud only exposes a\n` +
  `      // NAT-translated port; the real local P2P port is ephemeral and bound to whichever socket first\n` +
  `      // reaches it. So sweep CHECK_CAM across ALL local ports ON THIS SESSION'S OWN SOCKET — the CAM_ID\n` +
  `      // reply then lands here and the normal onConnected path fires, giving a pure-LAN session.\n` +
  `      if (!this._ewSweeping) {\n` +
  `        this._ewSweeping = true;\n` +
  `        const camPayload = buildCheckCamPayload(this.cfg.p2pDid);\n` +
  `        // Sweep CONTINUOUSLY until connected: one dropped UDP probe to the (single, ephemeral) real port\n` +
  `        // would otherwise cost a full 15 s connect timeout. Repeating the pass gives ~15 chances in the\n` +
  `        // window, so a drop is retried a beat later instead of failing the whole attempt.\n` +
  `        const sweep = () => {\n` +
  `          if (this.connected || this.closed) return;\n` +
  `          let port = 1;\n` +
  `          const burst = () => {\n` +
  `            if (this.connected || this.closed) return;\n` +
  `            const end = Math.min(port + 1024, 65536);\n` +
  `            for (; port < end; port++) this.send({ host: lh, port }, RequestMessageType.CHECK_CAM, camPayload);\n` +
  `            if (port <= 65535) setTimeout(burst, 12);\n` +
  `            else setTimeout(sweep, 150); // finished a pass, still not connected → sweep again\n` +
  `          };\n` +
  `          burst();\n` +
  `        };\n` +
  `        sweep();\n` +
  `      }\n` +
  `    }\n`;

const MULTI_ORIGINAL =
  `    if (homeBaseAttached) {\n` +
  `      const serving = this.occupiedSiblingChannel(parentSn, key);\n` +
  `      if (serving !== void 0)\n` +
  `        throw new StationBusyError(serving);\n` +
  `    }\n`;

const MULTI_PATCHED =
  `    // eufy-wall patch: allow MULTIPLE cameras (channels) on ONE station session. The "one camera per\n` +
  `    // session" refusal is a CLIENT-side pre-check, not the station rejecting — an HB3 (T8030) serves two\n` +
  `    // channels on a single P2P session / single client IP (verified live), so a wall shows co-located\n` +
  `    // cameras at once with no extra IPs and no hole-punch. releaseLingeringSiblings only releases siblings\n` +
  `    // with NO consumers, so a watched camera is never dropped. Opt out with globalThis.__ewMultiChannel === false.\n` +
  `    if (homeBaseAttached && globalThis.__ewMultiChannel === false) {\n` +
  `      const serving = this.occupiedSiblingChannel(parentSn, key);\n` +
  `      if (serving !== void 0)\n` +
  `        throw new StationBusyError(serving);\n` +
  `    }\n`;

const PATCHES = [
  { name: "local-port sweep", mark: "_ewSweeping", original: SWEEP_ORIGINAL, patched: SWEEP_PATCHED },
  { name: "multi-channel", mark: "__ewMultiChannel === false", original: MULTI_ORIGINAL, patched: MULTI_PATCHED },
];

let src = readFileSync(entry, "utf8");
let changed = false;
for (const p of PATCHES) {
  if (src.includes(p.mark)) {
    console.log(`[patch-sdk] ${p.name} patch already present`);
    continue;
  }
  if (!src.includes(p.original)) {
    console.warn(
      `[patch-sdk] could not find the block for the ${p.name} patch — the SDK version may have changed. ` +
        "The affected feature will not work until the patch is updated (see docs/hb3-local-port.md).",
    );
    continue;
  }
  src = src.replace(p.original, p.patched);
  changed = true;
  console.log(`[patch-sdk] applied ${p.name} patch to @mega-yfue/eufy-sdk`);
}
if (changed) writeFileSync(entry, src);
