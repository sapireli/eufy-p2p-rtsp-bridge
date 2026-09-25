// The wall's event channel: `/ws`.
//
// A tile cannot poll for motion. By the time a poll noticed, the thing that moved would be gone — so the
// server pushes. Three messages, all read-only:
//
//   motion       {sn, event, at}                a camera reported something
//   hold         {sn, until, owners}            a hold was taken, extended or released
//   streamState  {sn, state: idle|live, codec?} whether there is video to show right now
//   heartbeat    {at}                           application-level liveness for quiet walls
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
const MAX_PENDING_EVENTS = 256;

export function createWsHub(ctx, { heartbeatMs = HEARTBEAT_MS } = {}) {
  /** @type {Set<import("ws").WebSocket>} */
  const clients = new Set();
  const pending = new Map(); // events received while a client's async hello snapshot is being built
  const lastMotion = new Map(); // serial -> last motion event time, for clients joining mid-event
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
          holdSeconds: c.holdSeconds ?? ctx.cfg.defaults.holdSeconds,
          codec: ctx.streamStatus?.(c.sn)?.codec ?? c.codec ?? null,
          // The go2rtc stream key (the camera's name, slugged). The wall builds its RTSP URL from this
          // rather than from the serial, so renaming a camera moves its stream without a client change.
          streamKey: ctx.streamKeyFor?.(c.sn) ?? c.sn,
          ...(lastMotion.has(c.sn) ? { lastMotionAt: lastMotion.get(c.sn) } : {}),
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
    if (msg.type === "motion" && msg.sn && Number.isFinite(msg.at)) lastMotion.set(msg.sn, msg.at);
    for (const ws of clients) {
      const queue = pending.get(ws);
      if (queue) {
        if (queue.length >= MAX_PENDING_EVENTS) { pending.delete(ws); clients.delete(ws); ws.close(1013, "hello backlog exceeded"); }
        else queue.push(msg);
      } else send(ws, msg);
    }
    return msg;
  }

  function attach(server) {
    wss = new WebSocketServer({ server, path: "/ws" });
    wss.on("connection", (ws) => {
      clients.add(ws);
      pending.set(ws, []);
      ws.isAlive = true;
      ws.on("pong", () => {
        ws.isAlive = true;
      });
      ws.on("close", () => { clients.delete(ws); pending.delete(ws); });
      ws.on("error", () => { clients.delete(ws); pending.delete(ws); });
      snapshot().then((hello) => {
        if (!clients.has(ws)) return;
        send(ws, hello);
        for (const event of pending.get(ws) ?? []) send(ws, event);
        pending.delete(ws);
      }).catch(() => { pending.delete(ws); clients.delete(ws); ws.close(1011, "hello failed"); });
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
          // Control pings detect dead peers; a JSON heartbeat also completes the Go client's Read call.
          send(ws, { type: "heartbeat", at: Date.now() });
          ws.ping();
        } catch {
          /* dropped on the next sweep */
        }
      }
    }, heartbeatMs);
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
    pending.clear();
    lastMotion.clear();
    wss?.close();
    wss = undefined;
  }

  return { attach, broadcast, snapshot, close, clientCount: () => clients.size };
}
