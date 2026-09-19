import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { LoginStatus } from "@mega-yfue/eufy-sdk";
import { createStreamClients } from "../src/stream-clients.mjs";

const originals = {};
before(() => { for (const k of ["log", "warn", "error"]) { originals[k] = console[k]; console[k] = () => {}; } });
after(() => { for (const k of ["log", "warn", "error"]) console[k] = originals[k]; });

const cfg = (stationAddresses) => ({ email: "e", password: "p", country: "US", session: "/tmp/x.json", lan: { stationAddresses } });

/** Fake EufyMega: records options, resolves login on the next tick (so two callers overlap). */
function fakeCtor(status = LoginStatus.Ok) {
  const made = [];
  class Fake extends EventEmitter {
    constructor(opts) { super(); this.opts = opts; made.push(this); }
    login() { return new Promise((r) => setImmediate(() => r({ status }))); }
    async disconnect() { this.disconnected = true; }
  }
  return { Fake, made };
}

test("streamClientFor passes lan.station_addresses through as localAddresses", async () => {
  const { Fake, made } = fakeCtor();
  const sc = createStreamClients({ cfg: cfg({ T8010A: "192.168.1.50" }), EufyMega: Fake });
  const c = await sc.streamClientFor("CAM1");
  assert.equal(c, made[0]);
  assert.deepEqual(c.opts.localAddresses, { T8010A: "192.168.1.50" });
  assert.equal(c.opts.email, "e");
  assert.equal(c.opts.countryCode, "US");
  assert.equal(typeof c.opts.store, "object", "shared FileSessionStore");

  const empty = fakeCtor();
  const c2 = await createStreamClients({ cfg: cfg({}), EufyMega: empty.Fake }).streamClientFor("CAM1");
  assert.equal(c2.opts.localAddresses, undefined, "empty map → option omitted (SDK default)");
});

test("concurrent streamClientFor calls share one client; distinct stations get distinct clients", async () => {
  const { Fake, made } = fakeCtor();
  const sc = createStreamClients({ cfg: cfg({}), EufyMega: Fake });
  const [a, b] = await Promise.all([sc.streamClientFor("CAM1"), sc.streamClientFor("CAM1")]);
  assert.equal(a, b);
  assert.equal(made.length, 1, "only one EufyMega constructed for concurrent calls");
  assert.equal(await sc.streamClientFor("CAM1"), a, "cached afterwards");
  const other = await sc.streamClientFor("CAM2");
  assert.notEqual(other, a);
  assert.equal(made.length, 2);
  await sc.closeStreamClients();
  assert.equal(a.disconnected, true);
  assert.equal(other.disconnected, true);
});

test("two cameras behind one station share one client (one P2P session, multiplexed by channel)", async () => {
  const { Fake, made } = fakeCtor();
  const sc = createStreamClients({ cfg: cfg({}), EufyMega: Fake });
  const frontDoor = await sc.streamClientFor("CAM_A", "HB3");
  const garage = await sc.streamClientFor("CAM_B", "HB3"); // same HomeBase
  assert.equal(frontDoor, garage, "co-located cameras reuse the station's client/session");
  assert.equal(made.length, 1, "only one EufyMega for the whole station");
  const standalone = await sc.streamClientFor("CAM_C"); // its own station (stationSn defaults to sn)
  assert.notEqual(standalone, frontDoor);
  assert.equal(made.length, 2);
});

test("onClient hook fires before login with the client and sn; failed hydrate is not cached", async () => {
  const { Fake, made } = fakeCtor(LoginStatus.TwoFactor);
  const seen = [];
  const sc = createStreamClients({ cfg: cfg({}), EufyMega: Fake, onClient: (client, sn) => seen.push([client, sn]) });
  await assert.rejects(sc.streamClientFor("CAM1"), /could not hydrate/);
  assert.deepEqual(seen, [[made[0], "CAM1"]]);
  await assert.rejects(sc.streamClientFor("CAM1"), /could not hydrate/);
  assert.equal(made.length, 2, "retried with a fresh client, not the failed one");
});
