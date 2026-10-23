/** URL authorities keep IPv6 literals bracketed, including internal pull URLs. */
export function urlHost(host = "127.0.0.1") {
  const value = String(host);
  if (value.startsWith("[") && value.endsWith("]")) return value;
  return value.includes(":") ? `[${value}]` : value;
}

export function requestHost(authority = "127.0.0.1") {
  const parsed = new URL(`http://${authority}`);
  if (parsed.username || parsed.password || parsed.pathname !== "/" || parsed.search || parsed.hash) {
    throw new Error("Invalid Host header");
  }
  return parsed.hostname.replace(/^\[|\]$/g, "");
}

/** One wildcard IPv6 listener explicitly accepts IPv4-mapped connections too.
 * Explicit interface addresses retain their configured binding scope. */
export function listenHttp(server, { port, host = "::" }) {
  return new Promise((resolve, reject) => {
    const failed = error => { server.removeListener("listening", ready); reject(error); };
    const ready = () => { server.removeListener("error", failed); resolve(server.address()); };
    server.once("error", failed);
    server.once("listening", ready);
    server.listen({ port, host, ipv6Only: false });
  });
}
