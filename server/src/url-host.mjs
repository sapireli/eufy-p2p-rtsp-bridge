import { isIP } from "node:net";

/** Format an address for the host part of an HTTP or RTSP URL. */
export function urlHost(host) {
  const bare = host.startsWith("[") && host.endsWith("]") ? host.slice(1, -1) : host;
  return isIP(bare) === 6 ? `[${bare}]` : host;
}
