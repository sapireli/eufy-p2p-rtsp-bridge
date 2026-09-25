import { test } from "node:test";
import assert from "node:assert/strict";
import { chooseCameraPolicies } from "../src/setup-cameras.mjs";

const CAMS = [
  { sn: "WIRED", name: "Garage", powered: true, enabled: true, mode: "always", codec: "h264" },
  { sn: "BAT", name: "Door", powered: false, enabled: true, mode: "on_motion", codec: "h265" },
];

function driver(answers) {
  const prompts = [];
  return { prompts, question: async (label) => { prompts.push(label); return answers.shift(); }, emit: () => {} };
}

test("wizard selects enabled cameras and modes without dropping existing overrides", async () => {
  const controls = driver(["yes", "on_demand", "no"]);
  const result = await chooseCameraPolicies(CAMS, { WIRED: { name: "Custom" }, BAT: { codec: "h265" } }, controls);
  assert.deepEqual(result, { WIRED: { name: "Custom", enabled: true, mode: "on_demand" }, BAT: { codec: "h265", enabled: false, mode: "on_motion" } });
  assert.equal(controls.prompts.length, 3);
});

test("wizard rejects continuous battery mode without an explicit power claim", async () => {
  await assert.rejects(chooseCameraPolicies([CAMS[1]], {}, driver(["yes", "always"])), /power_override/);
  const allowed = await chooseCameraPolicies([CAMS[1]], { BAT: { power_override: "always-on" } }, driver(["yes", "always"]));
  assert.equal(allowed.BAT.mode, "always");
});

test("wizard rejects ambiguous yes/no and mode input", async () => {
  await assert.rejects(chooseCameraPolicies([CAMS[0]], {}, driver(["maybe"])), /enable must be/);
  await assert.rejects(chooseCameraPolicies([CAMS[0]], {}, driver(["yes", "all"])), /mode must be/);
});
