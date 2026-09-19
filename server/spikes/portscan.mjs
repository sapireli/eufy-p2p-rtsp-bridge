// Protocol-aware UDP port scan for a eufy station's real LOCAL P2P port.
// Sends a valid CHECK_CAM (and LOCAL_LOOKUP) to every port on the target and reports any reply.
// Run from server/ in a terminal WITH macOS Local Network permission.
//   node spikes/portscan.mjs <station-sn> <lan-ip>
import dgram from "node:dgram";
import { EufyMega, FileSessionStore } from "@mega-yfue/eufy-sdk";

const SN = process.argv[2] || "T8030P1324221481";
const HOST = process.argv[3] || "192.168.23.233";
const MAGIC = "XZYH";

// ---- reproduce the SDK's packet builders exactly ----
function stringWithLength(input, chunk = 8) {
  const b = Buffer.from(input);
  const size = b.byteLength < chunk ? chunk : Math.ceil(b.byteLength / chunk) * chunk;
  const out = Buffer.alloc(size);
  b.copy(out);
  return out;
}
function p2pDidToBuffer(did) {
  const a = did.split("-");
  const b2 = Buffer.allocUnsafe(4);
  b2.writeUInt32BE(Number.parseInt(a[1], 10), 0);
  return Buffer.concat([stringWithLength(a[0], 8), b2, stringWithLength(a[2], 8)], 20);
}
function frame(type, payload = Buffer.alloc(0)) {
  const len = Buffer.allocUnsafe(2);
  len.writeUInt16BE(payload.length, 0);
  return Buffer.concat([type, len, payload]);
}
const CHECK_CAM = Buffer.from([0xf1, 0x41]);
const LOCAL_LOOKUP = Buffer.from([0xf1, 0x30]);

const eufy = new EufyMega({ email: process.env.EUFY_EMAIL, password: process.env.EUFY_PASSWORD, countryCode: "US",
  store: new FileSessionStore("./data/.eufy-session.json"), prewarmEvents: [], autoRealtime: false });
console.log("login", (await eufy.login()).status);
const dev = (await eufy.getDevices()).find((d) => d.sn === SN);
const did = dev?.raw?.p2p_did;
if (!did) { console.log("no p2p_did for", SN); process.exit(1); }
console.log(`scanning ${HOST} for ${SN}  p2p_did=${did}`);

const checkPayload = Buffer.concat([p2pDidToBuffer(did), Buffer.from([0, 0, 0])]);
const checkPkt = frame(CHECK_CAM, checkPayload);
const lookupPkt = frame(LOCAL_LOOKUP, Buffer.from([0, 0]));

const s = dgram.createSocket("udp4");
const hits = [];
s.on("message", (m, r) => {
  const type = m.subarray(0, 2).toString("hex");
  // f142 CAM_ID (the win), f141 LOCAL_LOOKUP_RESP, f184 TURN_SERVER_CAM_ID
  const tag = type === "f142" ? " <<< CAM_ID (REAL PORT!)" : type === "f141" ? " (LOCAL_LOOKUP_RESP)" : type === "f184" ? " (TURN_CAM_ID)" : "";
  console.log(`REPLY from ${r.address}:${r.port}  type=0x${type} ${m.length}B${tag}`);
  hits.push(r.port);
});
s.on("error", (e) => { if (e.code !== "EHOSTUNREACH" && e.code !== "EPIPE" && e.code !== "ECONNREFUSED") console.log("sockerr", e.code); });

s.bind(async () => {
  let sent = 0;
  for (let port = 1; port <= 65535; port++) {
    s.send(checkPkt, port, HOST, () => {});
    if (port % 4 === 0) s.send(lookupPkt, port, HOST, () => {}); // sparse local-lookup probe too
    if (++sent % 4096 === 0) await new Promise((r) => setTimeout(r, 40)); // gentle pacing
  }
  console.log(`sent CHECK_CAM to all 65535 ports; waiting 5s for replies...`);
  setTimeout(() => {
    console.log(`--- done. ${hits.length} reply/replies from ports: ${[...new Set(hits)].join(", ") || "(none)"}`);
    s.close(); eufy.disconnect().finally(() => process.exit(0));
  }, 5000);
});
