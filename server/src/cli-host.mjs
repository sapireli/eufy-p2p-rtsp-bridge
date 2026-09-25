export function bridgeBase(cfg) {
  const host = cfg.host === "0.0.0.0" ? "127.0.0.1" : cfg.host === "::" ? "[::1]" : cfg.host.includes(":") ? `[${cfg.host}]` : cfg.host;
  return `http://${host}:${cfg.port}`;
}
