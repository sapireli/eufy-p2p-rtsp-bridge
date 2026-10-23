# Shared bridge settings from Linux

The Linux client provides a settings CLI. These controls change the bridge for every display. Layout, selected tiles, motion outlines and decoder priorities remain local to each display. The Linux display configuration in `/etc/eufy-wall.yaml` remains supported; deprecation applies to bridge operating settings.

The persistent bridge settings store is authoritative. Runtime YAML accepts bootstrap settings only. The installer imports and retires old operating fields with an explicit offline tool; normal startup never falls back to them. Use `show` to inspect saved values, revision and apply status before changing them.

## Connect

Obtain the bridge administrator token from its private `settings-admin-token` file in the bridge data directory, then save a copy in a file readable only by your user. Do not place the token in a command argument or commit it. The examples use `~/.config/eufy-wall/bridge-token`.

```sh
eufy-wall settings --bridge http://homebridge.lan:3000 \
  --token-file ~/.config/eufy-wall/bridge-token show
```

`EUFY_WALL_ADMIN_TOKEN` is also supported. Without `--bridge`, the command uses the existing display configuration (`--config`, default `/etc/eufy-wall.yaml`), including mDNS discovery when `rtsp_base: auto`. An explicit HTTP URL supports custom API ports and works without a display configuration or attached screen.

Options precede the command. Use the same connection options with each command below.

## Camera controls

List cameras to find a name or serial:

```sh
eufy-wall settings --token-file ~/.config/eufy-wall/bridge-token cameras
```

Change a camera using its serial or an unambiguous name. Values are JSON, so strings need double quotes inside the shell quotes.

```sh
eufy-wall settings --token-file ~/.config/eufy-wall/bridge-token camera "Garage Doorbell" power_override '"always-on"'
eufy-wall settings --token-file ~/.config/eufy-wall/bridge-token camera "Garage Interior Door" power_override '"battery"'
eufy-wall settings --token-file ~/.config/eufy-wall/bridge-token camera "Garage Interior Door" mode '"on_motion"'
eufy-wall settings --token-file ~/.config/eufy-wall/bridge-token camera "Front Door" dual_view '"split"'
```

Use `null` to remove an override and inherit the default. The bridge validates allowed values and camera capabilities. Requested composition does not prove the camera is currently sending that composition; inspect the camera's observed stream separately.

## Other bridge controls

`patch` accepts a partial settings JSON object, either inline or in an `@file`. Other settings are retained.

```sh
eufy-wall settings --token-file ~/.config/eufy-wall/bridge-token patch '{"lan":{"force":true}}'
eufy-wall settings --token-file ~/.config/eufy-wall/bridge-token patch @bridge-changes.json
```

Every update supplies the current revision. If another client changes settings between the read and write, the command reports a conflict and does not retry. Review `show` and repeat your intended update.

The response reports whether settings applied or require a restart. A restart affects every viewer and happens only when explicitly requested:

```sh
eufy-wall settings --token-file ~/.config/eufy-wall/bridge-token restart
```

## Backup and restore

```sh
eufy-wall settings --token-file ~/.config/eufy-wall/bridge-token export bridge-settings.json
eufy-wall settings --token-file ~/.config/eufy-wall/bridge-token import bridge-settings.json
```

Export creates a new file with mode `0600` and refuses to overwrite an existing file. Backups can contain private external stream URLs, including credentials. Import replaces shared settings using the bridge's versioned backup format and revision check; it does not replace local display settings. Inspect the returned apply status before explicitly restarting.

## Existing view-switch flags

The older flags remain available. On the current bridge they require the same administrator token and save the shared camera view through the bridge's settings store:

```sh
eufy-wall -settings-token-file ~/.config/eufy-wall/bridge-token \
  -view-camera "Front Door" -view-mode split
```

`EUFY_WALL_ADMIN_TOKEN` also works with these flags. Older bridges without persistent settings remain compatible. Shared composition changes affect all viewers.
