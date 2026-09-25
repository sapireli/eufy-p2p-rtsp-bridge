/** Ask for per-camera policy after authentication has made discovery possible. */
export async function chooseCameraPolicies(cameras, existing, { question, emit }) {
  const selected = { ...existing };
  for (const camera of cameras) {
    emit(`${camera.name} (${camera.sn}) — ${camera.powered ? "wired" : "battery"}, codec ${camera.codec ?? "unknown"}, ${camera.dual ? "dual lens" : "single lens"}, current mode ${camera.mode}`);
    const answer = (await question("Enable? (yes/no)", camera.enabled ? "yes" : "no")).toLowerCase();
    if (answer !== "yes" && answer !== "no") throw new Error(`${camera.sn}: enable must be yes or no`);
    const enabled = answer === "yes";
    const mode = enabled ? await question("Mode (always/on_motion/on_demand)", camera.mode) : camera.mode;
    if (!["always", "on_motion", "on_demand"].includes(mode)) throw new Error(`${camera.sn}: mode must be always, on_motion, or on_demand`);
    if (!camera.powered && mode === "always" && selected[camera.sn]?.power_override !== "always-on")
      throw new Error(`${camera.sn}: battery camera needs an explicit power_override: always-on for continuous streaming`);
    selected[camera.sn] = { ...selected[camera.sn], enabled, mode };
  }
  return selected;
}
