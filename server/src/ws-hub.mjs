// The wall's event channel: `/ws`.
//
// A tile cannot poll for motion. By the time a poll noticed, the thing that moved would be gone — so the
// server pushes. Three messages, all read-only:
//
//   motion       {sn, event, at}                a camera reported something
//   hold         {sn, until, owners}            a hold was taken, extended or released
//   streamState  {sn, state: idle|live, codec?} whether there is video to show right now
//
// Read-only deliberately. A client that wants to *request* a stream does it over HTTP (POST /hold/<sn>),
// which keeps this a one-way fan-out with no command parsing, no auth surface and no per-client state to
// get out of sync.
//
// Every message carries `at` so a client can tell a live event from one replayed on connect, and the
// hello carries a snapshot so a tile that connects mid-event knows what is already happening rather than
// waiting for the next one.
import { WebSocketServer } from "ws";

const HEARTBEAT_MS = 30_000;

export function createWsHub(ctx) {
  /** @type {Set<import("ws").WebSocket>} */
  const clients = new Set();
  let wss;
  let heartbeat;

  /** The state a joining client would otherwise have to wait for an event to learn. */
  async function snapshot() {
    const cameras = await Promise.all(
      (ctx.listCameras?.() ?? [])
        .filter((c) => c.enabled)
        .map(async (c) => ({
          sn: c.sn,
          name: c.name,
          mode: c.mode ?? "always",
          codec: ctx.streamStatus?.(c.sn)?.codec ?? c.codec ?? null,
          // The go2rtc stream key (the camera's name, slugged). The wall builds its RTSP URL from this
          // rather than from the serial, so renaming a camera moves its stream without a client change.
          streamKey: ctx.streamKeyFor?.(c.sn) ?? c.sn,
          state: ctx.state.streaming.has(c.sn) ? "live" : ctx.state.starting?.has?.(c.sn) ? "starting" : "idle",
          // Whether GET /snapshot/<sn> has a thumbnail. A wall that renders a still for a camera with
          // none would be pointing a pipeline at a 404 and restarting it forever.
          still: Boolean(await ctx.sdk?.snapshotStored?.(c.sn).catch(() => undefined)),
        })),
    );
    return { type: "hello", at: Date.now(), cameras, holds: ctx.holds?.status?.() ?? {} };
  }

  function send(ws, msg) {
    if (ws.readyState !== ws.OPEN) return;
    try {
      ws.send(JSON.stringify(msg));
    } catch {
      /* a send failing is that client's problem, not the wall's */
    }
  }

  /** Fan one event out to every connected client. Never throws: a broken client must not break a stream. */
  function broadcast(event) {
    const msg = { at: Date.now(), ...event };
    for (const ws of clients) send(ws, msg);
    return msg;
  }

  function attach(server) {
    wss = new WebSocketServer({ server, path: "/ws" });
    wss.on("connection", (ws) => {
      clients.add(ws);
      ws.isAlive = true;
      ws.on("pong", () => {
        ws.isAlive = true;
      });
      ws.on("close", () => clients.delete(ws));
      ws.on("error", () => clients.delete(ws));
      snapshot().then((hello) => send(ws, hello));
    });
    // A wall display that loses power or its network leaves a socket that never closes; without this the
    // server accumulates them and keeps serialising events to nobody.
    heartbeat ??= setInterval(() => {
      for (const ws of clients) {
        if (!ws.isAlive) {
          clients.delete(ws);
          try {
            ws.terminate();
          } catch {
            /* already gone */
          }
          continue;
        }
        ws.isAlive = false;
        try {
          ws.ping();
        } catch {
          /* dropped on the next sweep */
        }
      }
    }, HEARTBEAT_MS);
    heartbeat.unref?.();
    return wss;
  }

  function close() {
    if (heartbeat) clearInterval(heartbeat);
    heartbeat = undefined;
    for (const ws of clients) {
      try {
        ws.terminate();
      } catch {
        /* already gone */
      }
    }
    clients.clear();
    wss?.close();
    wss = undefined;
  }

  return { attach, broadcast, snapshot, close, clientCount: () => clients.size };
}
