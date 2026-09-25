import { urlHost } from "./url-host.mjs";

export function bridgeBase(cfg) {
  const host = cfg.host === "0.0.0.0" ? "127.0.0.1" : cfg.host === "::" ? "::1" : cfg.host;
  return `http://${urlHost(host)}:${cfg.port}`;
}
