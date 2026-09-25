const FIELD = /^(?:schema_version|eufy(?:\.[\w-]+)?|lan(?:\.[\w-]+)*|defaults(?:\.[\w-]+)*|stall(?:\.[\w-]+)*|go2rtc(?:\.[\w-]+)*|cameras\.[\w-]+(?:\.[\w-]+)?|host|port|self_host|data_dir|go2rtc_bin|poll_ms)\b/;

/** Stable, secret-safe CLI diagnostics for YAML and config transaction failures. */
export function configDiagnostic(error) {
  const message = String(error?.message ?? error).split("\n", 1)[0]; // YAML parser snippets can contain inline secrets.
  const unsupported = /^unsupported config key ([^\s]+)/.exec(message)?.[1];
  const path = unsupported ?? FIELD.exec(message)?.[0] ?? null;
  const yaml = error?.name === "YAMLParseError";
  const usage = message.startsWith("usage: eufy-bridge ");
  const apply = message.startsWith("apply failed;");
  const file = ["ENOENT", "EACCES", "EPERM", "EISDIR"].includes(error?.code);
  const code = usage ? "CLI_USAGE" : yaml ? "CONFIG_YAML_INVALID" : unsupported ? "CONFIG_UNSUPPORTED_KEY" :
    message.startsWith("schema_version ") ? "CONFIG_SCHEMA_UNSUPPORTED" :
    apply ? "CONFIG_APPLY_ROLLED_BACK" : file ? "CONFIG_FILE_UNREADABLE" : "CONFIG_INVALID";
  const remedy = usage ? "Follow the shown command syntax and rerun it." :
    yaml ? "Correct YAML syntax at the reported line; keep secrets in the environment file." :
    unsupported ? "Correct or remove this key; compare with eufy-bridge config example." :
    code === "CONFIG_SCHEMA_UNSUPPORTED" ? "Upgrade eufy-bridge to a version that supports this schema." :
    apply ? "Run eufy-bridge status --json and inspect the retained failed candidate." :
    file ? "Provide an existing readable YAML file or '-' for stdin." :
    path ? `Correct ${path}; see eufy-bridge config explain ${path}.` : "Correct the config and run eufy-bridge config validate again.";
  const line = yaml ? error.linePos?.[0]?.line ?? null : null;
  const column = yaml ? error.linePos?.[0]?.col ?? null : null;
  return { code, severity: "error", path, message, remedy, ...(line != null ? { line, column } : {}) };
}
