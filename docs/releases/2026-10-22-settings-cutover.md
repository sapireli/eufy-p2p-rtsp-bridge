# Unreleased: bridge settings cutover — October 22, 2026

The configuration transition is complete. Legacy operating settings must be migrated before upgrading to this version.

## Breaking change

Normal bridge startup accepts bootstrap configuration only. Camera policies, defaults, LAN restrictions, transcoding, freshness, recovery settings, polling and external stream registration now come from the authoritative `settings.json` store and are managed through Android or Linux clients.

Runtime YAML operating fields and the four retired `BRIDGE_GO2RTC_TRANSCODE`, `BRIDGE_GO2RTC_MAX_HEIGHT`, `BRIDGE_GO2RTC_VAAPI_DEVICE` and `BRIDGE_GO2RTC_VAAPI_DECODE` environment overrides are rejected. Credentials, listener ports and other bootstrap options remain supported. The Linux display's own `/etc/eufy-wall.yaml` is unchanged.

## Upgrade

Run `sudo deploy/install-server.sh`. The installer stops the old service before replacing files, creates private rollback backups, runs the explicit offline importer, retires YAML operating fields, and removes the four old environment assignments only after successful migration. It preserves an existing settings store and later client edits. Failure leaves the service stopped with the backup location reported.

Legacy import remains available only through `node server/migrate-legacy-config.mjs --config PATH --data-dir PATH --env-file PATH`. It is not part of normal startup. Existing external sources and dual-view choices are included when an old installation is first imported. After initialization, deleting the store fails startup rather than restoring obsolete configuration.

If the bridge cannot start because its initialized store is missing or damaged, `server/restore-settings.mjs` restores the clients’ exported backup offline; replacing an existing store requires `--replace` and creates a private rollback copy.

Both clients support authenticated shared settings, version-checked updates and backup/restore. Display layouts, selected cameras, decoder pins and motion outlines remain local. See [migration instructions](../bridge-settings-migration.md).
