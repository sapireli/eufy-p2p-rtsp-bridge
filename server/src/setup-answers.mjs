const fields = new Set([
  "email", "password", "country", "lan_cidr", "lan_force", "host", "client_host", "port",
  "probe_streams", "cameras", "tfa_code", "captcha_code",
]);

export function validateSetupAnswers(answers) {
  if (!answers || typeof answers !== "object" || Array.isArray(answers))
    throw new Error("setup answers must be a YAML mapping");
  for (const key of Object.keys(answers)) {
    if (!fields.has(key)) throw new Error(`unknown setup answer: ${key}`);
  }
  for (const key of ["email", "password", "country", "host", "client_host", "tfa_code", "captcha_code"]) {
    if (answers[key] != null && typeof answers[key] !== "string") throw new Error(`${key} must be a string`);
  }
  if (answers.lan_cidr != null && typeof answers.lan_cidr !== "string")
    throw new Error("lan_cidr must be a CIDR string or null");
  if (answers.lan_force != null && typeof answers.lan_force !== "boolean" &&
      (typeof answers.lan_force !== "string" || !["yes", "no"].includes(answers.lan_force.toLowerCase())))
    throw new Error("lan_force must be true, false, yes, or no");
  if (answers.port != null && (!Number.isInteger(answers.port) || answers.port < 1 || answers.port > 65535))
    throw new Error("port must be an integer from 1 to 65535");
  if (answers.cameras != null && (typeof answers.cameras !== "object" || Array.isArray(answers.cameras)))
    throw new Error("cameras must be a mapping keyed by camera serial");
  if (answers.probe_streams != null && typeof answers.probe_streams !== "boolean" &&
      (!Array.isArray(answers.probe_streams) || answers.probe_streams.some((sn) => typeof sn !== "string")))
    throw new Error("probe_streams must be true, false, or a list of camera serials");
  return answers;
}
