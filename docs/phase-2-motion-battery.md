# Phase 2 — motion detection and battery cameras

Phase 1 is wired, always-on cameras: every enabled camera streams continuously and a tile is just an RTSP
URL. Battery cameras cannot work that way — a continuous stream keeps the radio awake and flattens them —
so they stream only when something happens, and the wall has to cope with a tile that is sometimes live and
sometimes not.

This is the plan the design spec referred to as `docs/phase-2-motion-battery.md` (never written until now).

## What the SDK gives us

- **Events arrive over push (FCM), independent of P2P.** A battery camera reports motion without any
  session being open, which is what makes on-motion streaming possible at all.
- **Semantic events**: `motion`, `personDetected`, `doorbellPress`, and friends. `dev.describe()` says
  which a given device emits.
- **A battery budget.** A source whose `powered` hint is `battery` is bounded: after `batteryBudgetMs`
  (default 45 s) it emits a `budget` notice, then auto-stops after `budgetGraceMs` (default 10 s) unless
  the consumer calls `extend()`. Wired sources are unbounded and never emit it.
- **On-demand sessions.** P2P opens when something needs it and idle-detaches after `p2pIdleMs`
  (default 5 min) so the camera sleeps. Reads are served from cache and never wake it.
- **`prewarmEvents`** speculatively opens a camera's session the moment an event lands, so a stream that
  starts a second later attaches to a warm session instead of paying a cold connect.

## Decisions

- **Both motion models, chosen per tile.** A dedicated `motion: latest` tile that follows whichever
  camera most recently fired, *and* per-camera `on_motion` tiles that wake in place. They are different
  enough that one does not subsume the other: `latest` is one tile for N cameras, `on_motion` is one tile
  per camera.
- **All three battery cameras come online**: Front Yard, Solar Wall Light, Garage Interior Door.
- **A hold is the unit of streaming.** Motion does not "start a stream", it *takes a hold* on a camera.
  Holds have an owner and an expiry, they can be extended, and a camera streams exactly while it has at
  least one. This is what keeps battery budget handling, WS state and tile swapping consistent.

## Server

### Per-camera policy

```yaml
cameras:
  T81A0…190D5C: { enabled: true, mode: on_motion, hold_seconds: 60 }
  T8425…:       { enabled: true, mode: always }        # wired, Phase 1 behaviour
```

- `always` — stream continuously (today's behaviour, the default for a wired camera).
- `on_motion` — idle until an event; motion takes a hold; stream while held.
- `on_demand` — stream only while a consumer is attached (a tile asked for it).

The default follows the power source: wired → `always`, battery → `on_motion`. That replaces the Phase 1
rule that skipped battery cameras entirely.

### Hold manager

One place owns "should this camera be streaming right now":

- `hold(sn, owner, seconds)` — take/extend a hold; starts the feed if it was idle.
- holds expire on a timer; the last one expiring stops the feed and lets the session idle-detach.
- a `budget` notice while a hold is live → `extend()`. With no hold, let it stop: the notice is the SDK
  telling us the camera has been streaming a long time, and if nothing is holding it, nothing wants it.

`motion` for an `on_motion` camera takes a hold owned by `motion`. A client asking for a tile takes one
owned by that client. They compose: a camera held by both stays up until both expire.

### WS `/ws`

Same envelope shape as ha-bridge. Three messages:

- `motion`   — `{sn, event, at}` when an event fires.
- `hold`     — `{sn, until, owners}` when a hold is taken, extended or released.
- `streamState` — `{sn, state: idle|starting|live|stopping}` so a tile knows whether to show video.

Read-only for now; a client that wants to *request* a hold can do it over HTTP rather than growing a
command channel.

### Battery care

- Pass `powered: "battery"` for battery cameras so the SDK's budget applies (Phase 1 forces `"wired"`
  for everything, which is right for mains cameras and wrong here).
- `prewarmEvents` for the events we act on, `prewarmTiers: ["battery"]` — the pre-warm only genuinely
  opens a session for a standalone battery camera, which is exactly the one we are about to stream.
- Never hold a battery camera indefinitely. `hold_seconds` is a bound, not a starting point.

## Client

- **WS listener** — reconnecting, feeding a small state store of per-camera `streamState` + last motion.
- **Per-tile pipelines.** Today one GStreamer process renders every tile, so one source dying restarts
  all of them and a tile cannot start or stop independently. A tile that swaps between a placeholder and
  live RTSP needs its own process. This is the largest piece of Phase 2 and the one Phase 1 explicitly
  deferred ("per-tile isolation is a Phase 2 item").
- **Tile modes** in config:
  - `motion: latest` — a dynamic tile that follows the most recent motion across a named set.
  - `mode: on_motion` — a fixed tile for one camera, placeholder until that camera is live.
- **Placeholder** — last snapshot (`/snapshot/<sn>`) with a timestamp, so an idle tile is not a black box.

### The "magic screen"

A `motion: latest` tile watches a SET of cameras and shows whichever fired most recently. The set is
named explicitly and is not limited to battery cameras — pointing it at a wired camera is useful and
costs nothing, because that camera is already streaming and the tile just switches URL.

```yaml
tiles:
  - motion: latest
    watch: [T81A0…190D5C, T81A0…080C44, T8425…]   # any camera; or `watch: all`
    blank_after_seconds: 120                       # nothing recent → show nothing
```

Two behaviours fall out of the same tile:

- **Follow** — a tile in a normal layout that swaps to whatever just moved.
- **Blank until motion** — a whole screen (`layout: 1` with one `motion: latest` tile) that shows
  nothing until something happens, then lights up. `blank_after_seconds` is what makes it a screen that
  "turns on for motion" rather than one permanently showing the last thing that moved.

Mechanics:

- The tile needs the camera streaming. A camera in `on_motion` is already held by the server's own motion
  handler, so the tile only has to follow. A camera in `on_demand` is not, so the tile asks for a hold
  (HTTP `POST /hold/<sn>`, owner = that client) and refreshes it while it displays that camera.
- Ties and flapping: a minimum dwell time, so two cameras firing together do not make the tile strobe.
- The server already broadcasts `motion` for EVERY enabled camera regardless of mode, which is what lets
  the set include wired ones.

## Staging — all four done

1. ✅ **Server policy + holds + battery care.**
2. ✅ **WS `/ws`** plus `POST`/`DELETE /hold/<sn>`.
3. ✅ **Client per-tile pipelines** (planes split per tile; a compositor stays one process).
4. ✅ **Client WS + motion tiles**, including the magic screen.

Verified end to end against the cameras: motion on a sleeping battery camera woke it, the wall showed
it, the hold was refreshed while it stayed on screen, and at `blank_after_seconds` the tile blanked,
the wall released, and the camera went back to sleep with no pipeline left running for it.

`POST /debug/motion?sn=…` injects an event down the same path as a real one, so a motion wall can be
exercised without waiting for something to walk past a camera.

### Not done

- **Snapshot placeholders.** A blank tile is currently blank, not a last-known still. `/snapshot/<sn>`
  does not exist on the bridge yet; the SDK's `snapshotStored()` would supply it without waking a
  camera.
- **`streamState: starting`.** The server reports only `idle` and `live`, so a tile shows nothing
  during the second or two a battery camera takes to wake rather than saying it is coming.

Phase 1 config, layout, RTSP naming and `/api/cameras` do not change shape, as the design spec intended.
