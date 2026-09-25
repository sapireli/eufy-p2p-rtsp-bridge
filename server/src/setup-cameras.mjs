/** Ask for per-camera policy after authentication has made discovery possible. */
export async function chooseCameraPolicies(cameras, existing, { question, emit }) {
  const selected = { ...existing };
  for (const camera of cameras) {
    const power = camera.powerOverride === "always-on" ? "always-on claim (verify external power)" : camera.powered ? "wired" : "battery-budgeted";
    emit(`${camera.name} (${camera.sn}) — ${power}, codec ${camera.codec ?? "unknown"}, ${camera.dual ? "dual lens" : "single lens"}, current mode ${camera.mode}`);
    const answer = (await question("Enable? (yes/no)", camera.enabled ? "yes" : "no")).toLowerCase();
    if (answer !== "yes" && answer !== "no") throw new Error(`${camera.sn}: enable must be yes or no`);
    const enabled = answer === "yes";
    let mode = camera.mode;
    while (enabled) {
      mode = await question("Mode (always/on_motion/on_demand)", mode);
      if (!["always", "on_motion", "on_demand"].includes(mode)) throw new Error(`${camera.sn}: mode must be always, on_motion, or on_demand`);
      if (camera.powered || mode !== "always" || selected[camera.sn]?.power_override === "always-on") break;
      emit(`${camera.sn}: continuous streaming can drain a battery. Claim always-on only if this camera has reliable external power; this setting does not change its power source.`);
      const claim = (await question("Set power_override: always-on? (yes/no)", "no")).toLowerCase();
      if (claim !== "yes" && claim !== "no") throw new Error(`${camera.sn}: power override choice must be yes or no`);
      if (claim === "yes") { selected[camera.sn] = { ...selected[camera.sn], power_override: "always-on" }; break; }
      emit(`${camera.sn}: choose on_motion or on_demand instead.`);
      mode = "on_motion";
    }
    selected[camera.sn] = { ...selected[camera.sn], enabled, mode };
  }
  return selected;
}
