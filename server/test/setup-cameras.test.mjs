import { test } from "node:test";
import assert from "node:assert/strict";
import { chooseCameraPolicies } from "../src/setup-cameras.mjs";

const CAMS = [
  { sn: "WIRED", name: "Garage", powered: true, enabled: true, mode: "always", codec: "h264" },
  { sn: "BAT", name: "Door", powered: false, enabled: true, mode: "on_motion", codec: "h265" },
];

function driver(answers) {
  const prompts = [], messages = [];
  return { prompts, messages, question: async (label) => { prompts.push(label); return answers.shift(); }, emit: (message) => messages.push(message) };
}

test("wizard selects enabled cameras and modes without dropping existing overrides", async () => {
  const controls = driver(["yes", "on_demand", "no"]);
  const result = await chooseCameraPolicies(CAMS, { WIRED: { name: "Custom" }, BAT: { codec: "h265" } }, controls);
  assert.deepEqual(result, { WIRED: { name: "Custom", enabled: true, mode: "on_demand" }, BAT: { codec: "h265", enabled: false, mode: "on_motion" } });
  assert.equal(controls.prompts.length, 3);
});

test("wizard requires an explicit power claim for continuous battery mode and offers a safe re-choice", async () => {
  const accepted = driver(["yes", "always", "yes"]);
  const allowed = await chooseCameraPolicies([CAMS[1]], {}, accepted);
  assert.deepEqual(allowed.BAT, { enabled: true, mode: "always", power_override: "always-on" });
  assert.match(accepted.messages.join(" "), /does not change its power source/);
  const declined = driver(["yes", "always", "no", "on_demand"]);
  const safe = await chooseCameraPolicies([CAMS[1]], {}, declined);
  assert.deepEqual(safe.BAT, { enabled: true, mode: "on_demand" });
  assert.equal(declined.prompts.filter((p) => p.startsWith("Mode")).length, 2);
  const existing = await chooseCameraPolicies([CAMS[1]], { BAT: { power_override: "always-on" } }, driver(["yes", "always"]));
  assert.equal(existing.BAT.mode, "always");
});

test("wizard names a power override as a claim, never as measured wiring", async () => {
  const controls = driver(["yes", "always"]);
  await chooseCameraPolicies([{ ...CAMS[1], powered: true, powerOverride: "always-on" }], { BAT: { power_override: "always-on" } }, controls);
  assert.match(controls.messages[0], /always-on claim \(verify external power\)/);
  assert.doesNotMatch(controls.messages[0], /— wired/);
});

test("wizard rejects ambiguous yes/no and mode input", async () => {
  await assert.rejects(chooseCameraPolicies([CAMS[0]], {}, driver(["maybe"])), /enable must be/);
  await assert.rejects(chooseCameraPolicies([CAMS[0]], {}, driver(["yes", "all"])), /mode must be/);
  await assert.rejects(chooseCameraPolicies([CAMS[1]], {}, driver(["yes", "always", "maybe"])), /power override choice must be/);
});
