# Pi network teardown during otherwise live video

## Evidence before changes

The post-GPU-isolation Pi video failures are a separate event from the bridge GPU
hang. At 18:56:22 UTC Garage's GStreamer RTSP receiver reported a parse error.
At 18:58:44/45 UTC both decoded-frame watchdogs fired. SDK raw Garage delivery
continued, and the Fire TV rendered-frame counters continued for both cameras.
No new bridge GPU hang or FFmpeg publisher timeout was recorded for this window.

Pi journal records identify the local network change preceding each failure:

| UTC | Local network action | Video result |
| --- | --- | --- |
|18:56:09|DietPi-WiFi_Monitor declares connection loss| |
|18:56:12|DHCPRELEASE, address withdrawn| |
|18:56:13|wpa_supplicant disconnects, reason3, locally generated| |
|18:56:18/19|Address restored, monitor reports completed|Garage RTSP parse error 18:56:22|
|18:58:28|Monitor declares connection loss again| |
|18:58:30|DHCPRELEASE, address withdrawn| |
|18:58:31|Locally generated disassociation| |
|18:58:42/43|Address restored after DHCP retry|Both 15-second watchdogs fire 18:58:44/45|

These are forced local Wi-Fi teardown/rejoin cycles. The brcmfmac ARP-table errors
occur while addresses are removed/restored; they do not by themselves prove a
firmware fault caused the initial event. The monitor's own script explicitly
executes `ifdown`, sleeps, then `ifup` when one gateway ping fails or no gateway is
found. It checks every 10 seconds and performs no association check or retry
before removing the interface address. Whether the individual ping failure was
actual RF loss or a false connectivity alarm has not yet been measured.

Private records: `.evidence/pi-wifi-monitor-2026-10-09/network-events.txt`,
`monitor-original.sh`, and `interfaces-state.txt`.

## Exact client network

This client is a Raspberry Pi Model B Rev 2, using a USB Broadcom BCM43143
802.11b/g/n adapter, brcmfmac, firmware 01-32bd010e, driver 6.10.198.66. Its only
active external interface is wlan0; eth0 is down. It was associated to its 2.4GHz
access point with RSSI around−41/−42dBm, and Wi-Fi power saving was already off.
Only ifupdown/networking was active; NetworkManager, systemd-networkd, and dhcpcd
were inactive. Native wpa_supplicant and ISC dhclient were running, but had been
spawned by the Wi-Fi monitor's previous repair. No duplicate network-manager
conflict was demonstrated.

## Interrupted diagnostic and safe next step

A requested monitor-disabled comparison was attempted by stopping only the
optional monitor service at 19:05:57 UTC, leaving its original enabled state
unchanged. Remote access then disappeared; the same-LAN bridge's neighbor entry
became FAILED. The likely mechanism is systemd's default control-group stop:
the monitor's prior `ifup` spawned native wpa_supplicant/DHCP into its own service
control group. Stopping that group can also kill the actual connection daemons.
This attribution remains pending recovery and control-group inspection. The
execution omitted an independent network-restoration guard and was unsafe for
this current process tree. This was not a successful comparison or a completed
network repair.

After access recovery, inspect service control-group membership and preserve its
native network children before stopping the monitor. Arm an independent timed
network-restoration command before making any change. Use the supported optional
service enablement/configuration; do not replace vendor script or silently adjust
video buffers. A valid comparison must retain both actual players, record
association/link and gateway probes, and complete a fresh same-camera age check
plus 10 minutes without a forced network reset or client restart.

## Bounded alternate-access checks

After the interrupted stop, direct SSH to the recorded global IPv6 and
link-local IPv6 (Mac en1 scope) both timed out. The bridge's link-local probe on
eth0 received no reply and left that neighbor INCOMPLETE; its IPv4 neighbor for
192.168.23.73 was FAILED. No bridge neighbor matched the recorded Wi-Fi MAC
`d4:7b:b0:7e:ab:1d`. Fresh authoritative DNS still resolved `garagepi.lan` to
192.168.23.73. The previously used 192.168.23.20 also did not accept SSH, and a
bounded mDNS resolution of `garagepi.local` returned no address. These checks
found no usable alternate path; they do not prove that every possible address
or physical access method is unavailable.

## Exact guarded procedure (review only, not deployed)

Do not run this until the Pi is reachable again. First confirm native Wi-Fi/DHCP
control-group membership and stable association; preserve the original monitor
enablement state. The complete proposed commands are retained in
`.evidence/pi-wifi-monitor-2026-10-09/guarded-disable-procedure.sh`.

1. Save `systemctl show dietpi-wifi-monitor.service -p MainPID -p ControlGroup -p KillMode`
   and `systemd-cgls` output; identify the actual wpa_supplicant/dhclient children.
2. Arm a separate 60-second systemd rollback timer **before** stopping anything.
   Its service must use `Type=oneshot`, `RemainAfterExit=yes`, and
   `KillMode=process`: otherwise an `ifup` in the guard can recreate the same
   problem by spawning native daemons that systemd kills when the guard exits.
   The rollback calls `ifup --force wlan0` only if association or IPv4 is missing,
   restores the original monitor enablement, and starts the monitor.
3. Create a uniquely named service drop-in with `[Service]` and `KillMode=process`;
   reject an existing path, reload systemd, and verify the effective value.
4. Disable the optional monitor's boot enablement, then stop it with
   `systemctl stop --no-block dietpi-wifi-monitor.service`. This preserves native
   network children. Do not use unguarded `disable --now` with the old default.
5. Within 60 seconds, establish a **second new SSH connection** and verify
   `iw dev wlan0 link`, `ip -4 addr show dev wlan0`, the default route, WPA/DHCP
   processes, the monitor's inactive state, and increasing live frame counts.
   Only then cancel `eufy-wifi-network-rollback.timer`. If the rollback service
   already ran, record that and abort the comparison rather than claiming success.
6. Record a 10-minute comparison with no interface teardown or player resets and
   a fresh same-camera reference. A few missed ICMP replies while association and
   TCP video continue would demonstrate the monitor's false-positive detector.
   If actual association loss precedes the ping failure, investigate that instead.

This is a reviewable recovery/testing procedure, not a shipped installer change
or a completed repair. No script/vendor replacement or client timeout extension
is justified from the currently interrupted comparison.
