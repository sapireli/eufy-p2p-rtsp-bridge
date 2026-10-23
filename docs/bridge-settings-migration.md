# Bridge settings and YAML retirement

The bridge owns shared camera sessions and media recovery. Android and Linux clients manage its operating settings through the same HTTP API. Layouts, selected tiles, decoder priorities and motion-border preferences remain local to each display.

## Configuration transition completed October 22, 2026

The persistent `BRIDGE_DATA_DIR/settings.json` store is authoritative. Both clients manage normal bridge operation through the settings API. Normal startup accepts bootstrap settings only and rejects legacy operating fields in YAML and retired transcoding environment overrides. It does not import or fall back to those values. A genuinely fresh runtime can create an empty store. Once `settings-initialized` records initialization, a missing store is an error: restore a backup instead of silently recreating it.

Legacy compatibility is confined to the explicit offline migration tool. The installer stops the old service before replacing its files, preserves private rollback copies, runs the tool after dependency installation, removes the four retired operating environment assignments only after migration succeeds, and restarts only a previously active service. A failed upgrade leaves the service stopped and reports the backup location.

The offline import preserves camera overrides, defaults, LAN policy, transcoding, freshness, recovery timers and external sources. For an installation without a settings store, it embeds referenced `go2rtc.sources_file` streams and imports existing `dual-view-modes.json` choices ahead of YAML. If a valid store already exists, that store wins unchanged; obsolete YAML, external-source files and environment values cannot overwrite later client edits. Corrupt or unsupported stores stop the migration.

New installations use `server/bootstrap.example.yaml`. The installer creates the initial empty settings store and initialization marker through the same offline tool. `server/config.example.yaml` is retained only as an example of legacy migration input, not a supported runtime configuration.

Startup settings remain outside the client API: account credentials/session identity, data directory, listener addresses/ports, internal self-host address and executable paths. Keep those in the service environment or a small bootstrap file. The Linux display's `/etc/eufy-wall.yaml` remains a separate supported local configuration.

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

Clients must fetch the current revision before changing settings. Stale writes return409 and must be reviewed after refresh. Invalid settings return400 without changing the durable document. Request bodies are limited to1MiB. The legacy per-camera `/api/cameras/<serial>/view` route uses the same authorized settings path on the current bridge.

Settings use the former YAML field names: `cameras`, `defaults`, `lan`, `go2rtc`, `freshness`, `stall`, and `poll_ms`. Startup fields and unknown fields are rejected. Writes use a new private file, fsync and atomic rename. Export excludes the Eufy login and session, but includes external RTSP/Wyze source credentials needed for a complete restore. Treat backups as secrets. A replacement host still needs its own login/bootstrap and locally provisioned administrator token.

## Upgrade or migrate an existing installation

The normal installer performs this migration, including inactive installations that already have configuration files:

```sh
sudo deploy/install-server.sh
```

It stores private pre-upgrade config, environment, settings and source backups under `/var/backups/eufy-wall-bridge/`. Keep those copies until the upgrade and playback are verified. Credentials and bootstrap values remain in place.

For a manually managed installation, stop the bridge and run the explicit offline tool:

```sh
sudo systemctl stop eufy-wall-bridge
sudo node /opt/eufy-wall-bridge/server/migrate-legacy-config.mjs \
  --config /etc/eufy-wall-bridge.yaml \
  --data-dir /var/lib/eufy-wall-bridge \
  --env-file /etc/eufy-wall-bridge.env
```

The tool imports only when no store exists, validates the result, and retires operating fields from YAML while preserving bootstrap values and making a private copy of the exact original file. Repeating it preserves the current store. The environment file is read as literal assignments, never executed. The tool does not edit that environment file.

After successful migration, remove only these retired assignments from the service environment: `BRIDGE_GO2RTC_TRANSCODE`, `BRIDGE_GO2RTC_MAX_HEIGHT`, `BRIDGE_GO2RTC_VAAPI_DEVICE`, and `BRIDGE_GO2RTC_VAAPI_DECODE`. Preserve bootstrap settings such as `BRIDGE_GO2RTC_API_PORT` and login credentials. The installer handles this cleanup after making its private backup. Set ownership of the data directory to the service user, then start the bridge:

```sh
sudo chown -R eufy-wall:eufy-wall /var/lib/eufy-wall-bridge
sudo systemctl start eufy-wall-bridge
```

If migration fails, correct the reported problem before starting the bridge. Unknown fields, malformed files, unsupported operating overrides and unsafe file paths are rejected rather than discarded.

Verify both clients can read settings, export a private backup, test restoration, and confirm the intended settings survive a bridge restart. Store backups may contain external-camera credentials. Preserve the initialization marker alongside private installation backups. Do not delete `settings.json` to reset a camera; change its override through a client. The administrator token, Eufy session and old external-source file are not removed by migration. The external-source file is no longer read during normal startup.
