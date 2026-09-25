import { test } from "node:test";
import assert from "node:assert/strict";
import { bridgeBase } from "../src/cli-host.mjs";

test("local probe uses the actual configured bind address", () => {
  assert.equal(bridgeBase({ host: "0.0.0.0", port: 3000 }), "http://127.0.0.1:3000");
  assert.equal(bridgeBase({ host: "192.168.1.10", port: 3100 }), "http://192.168.1.10:3100");
  assert.equal(bridgeBase({ host: "::", port: 3000 }), "http://[::1]:3000");
  assert.equal(bridgeBase({ host: "2001:db8::1", port: 3000 }), "http://[2001:db8::1]:3000");
});
