# Bridge settings and YAML retirement

The bridge owns shared camera sessions and media recovery. Android and Linux clients manage its operating settings through the same HTTP API. Layouts, selected tiles, decoder priorities and motion-border preferences remain local to each display.

## Transition policy

This transition release imports existing bridge operating settings once into `BRIDGE_DATA_DIR/settings.json`. After that import, the JSON store is authoritative. Editing the old YAML operating settings or their old environment overrides has no effect. Startup logs warn while those legacy fields remain present.

The import preserves camera overrides, defaults, LAN policy, transcoding, freshness, recovery timers and external sources. It embeds the referenced `go2rtc.sources_file` streams and imports existing `dual-view-modes.json` choices ahead of YAML, matching the former precedence. Later edits to either legacy file are ignored. Corrupt or unsupported settings files stop startup; the bridge never silently falls back to stale YAML.

Legacy operating-field import remains available for this transition release and the next release. Remove it in the following release after both clients have shipped, existing installations have migrated, and backup/restore has passed deployment verification. Publish that removal in the release notes. This document records the removal gate; it does not claim that legacy import has already been removed.

Startup settings remain outside the client API: account credentials/session identity, data directory, listener addresses/ports, internal self-host address and executable paths. Keep those in the service environment or a small bootstrap file. The Linux display's `/etc/eufy-wall.yaml` is a separate local configuration and is not deprecated by this migration.

## Client controls

Android: open the menu and choose **Shared bridge settings**. Pair using the administrator token, then edit camera policies, defaults, LAN policy or transcoding. The advanced JSON editor exposes the remaining operating fields. Backup/restore uses the document picker, with a text fallback on devices without a document provider.

Linux: use `eufy-wall settings`; see [Linux settings commands](../client/SETTINGS.md). These commands can connect directly to a bridge without a local YAML file.

Read the pairing token locally from `<data directory>/settings-admin-token`. The bridge creates it with mode0600 and logs its path, never its value. Clients send it in an Authorization header. It authorizes settings reads, edits, exports, imports and restarts. Existing media endpoints retain their existing access policy. Serve this API only on a trusted network or behind authenticated TLS; the token does not encrypt HTTP traffic.

## Applying changes

Camera power claims, stream modes, holds, quality/view requests and recovery timers apply without restarting the entire bridge. A power-claim change retires only that camera's media source so its next open uses the new claim. Unchanged sibling cameras retain their sessions. Only the changed view or quality property is sent to the camera. Before login, changes report `waiting-for-login`; edits during boot return503 for a later retry. Unsupported camera setters produce a failed apply result; desired settings remain saved so the operator can correct or restore them.

Changes to LAN policy, SDK polling, motion event subscriptions, freshness, go2rtc settings or stream registration require an explicit bridge restart. Clients show `restart-required`; saving does not automatically interrupt every display. Camera name, enabled state, codec declaration and transcode/decode overrides also require restart because they affect go2rtc registration. The restart API is available under the supported systemd service (`Restart=always`); an unsupervised foreground process reports that it must be restarted manually.

`apply.state=applied` describes bridge policy application. It does not prove that a physical camera adopted a setting or that decoded pictures match it. Every response marks `scope=bridge-policy` and `cameraVideoVerified=false`. Coded dimensions and stream status remain available from `/api/cameras`; actual composition still requires decoded-video evidence.

## API

All routes below require `Authorization: Bearer <token>` and return `Cache-Control: no-store`.

- `GET /api/settings`: `{revision, settings, apply}`.
- `PATCH /api/settings`: `{revision, settings: <partial object>}`. Nested objects merge; a null camera field or camera entry deletes that override and restores inheritance. Arrays replace.
- `GET /api/settings/export`: complete versioned backup `{format:"eufy-wall-settings", version:1, settings}`.
- `POST /api/settings/import`: `{revision, backup:<export object>}`. Replaces operating settings after full validation.
- `POST /api/settings/restart`: `{revision}`. Explicit supervised restart; returns202.

Clients must fetch the current revision before changing settings. Stale writes return409 and must be reviewed after refresh. Invalid settings return400 without changing the durable document. Request bodies are limited to1MiB. The legacy per-camera `/api/cameras/<serial>/view` route uses the same authorized settings path once migration is enabled.

Settings use the former YAML field names: `cameras`, `defaults`, `lan`, `go2rtc`, `freshness`, `stall`, and `poll_ms`. Startup fields and unknown fields are rejected. Writes use a new private file, fsync and atomic rename. Export excludes the Eufy login and session, but includes external RTSP/Wyze source credentials needed for a complete restore. Treat backups as secrets. A replacement host still needs its own login/bootstrap and locally provisioned administrator token.

## Before removing legacy files

1. Confirm both clients can read and update the bridge settings.
2. Export a private backup and test restoration.
3. Confirm `settings.json` contains the intended settings after a bridge restart.
4. Remove only operating sections from the old YAML. Preserve required bootstrap settings and credentials.
5. Retain a private pre-migration backup until sustained playback is verified.

Do not delete `settings.json` to reset a camera. Doing so re-enables first-run legacy import during the transition and can restore obsolete policies.
