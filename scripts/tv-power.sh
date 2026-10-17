#!/bin/sh
# One-file CEC/client controller. Unknown or mismatched state never prints off.
set -u
export LC_ALL=C
umask 002
mode=${1:-}
case "$mode" in on|off|state|--worker-on|--worker-off) ;; *) echo "Usage: $0 on|off|state" >&2; exit 2;; esac
if [ "$mode" = state ] && [ "${TV_POWER_STATE_GUARD:-0}" != 1 ]; then
    # Also bound systemd/sudo/filesystem failures, not just the CEC subprocess.
    exec timeout -k 1s 12s env TV_POWER_STATE_GUARD=1 "$0" "$@"
fi
adapter=${CEC_DEVICE:-/dev/cec0}
# All callers (root, dietpi, Homebridge workers) must share the same locks.
runtime=${TV_POWER_RUNTIME:-/home/dietpi/.cache/tv-power}
mkdir -p "$runtime" || exit 1
if [ "$(id -u)" -eq 0 ]; then
    chown dietpi:dietpi "$runtime" || exit 1
    exec runuser -u dietpi -- env TV_POWER_RUNTIME="$runtime" CEC_DEVICE="$adapter" "$0" "$@"
fi
privileged() { if [ "$(id -u)" -eq 0 ]; then "$@"; else sudo -n "$@"; fi; }
log_event() { logger -t tv-power -- "$*" >/dev/null 2>&1 || :; }
control_lock() {
    exec 9>"$runtime/control.lock" || return 1
    flock -w 1 9
}
release_lock() { flock -u 9; exec 9>&-; }
cec() (
    exec 8>"$runtime/cec.lock" || exit 1
    flock -w 0 8 || exit 1
    printf '%s\n' "$1" | timeout -k 1s 8s cec-client -s -d 1 "$adapter"
)
query_tv() {
    output=$(cec 'pow 0' 2>&1) || return 1
    tv=$(printf '%s\n' "$output" | awk '/^power status: / {sub(/^power status: /, ""); sub(/\r$/, ""); s=$0} END {print s}')
    case "$tv" in on) tv=on;; standby) tv=off;; *) return 1;; esac
}
owned_client() {
    candidate_exe=$(privileged readlink "/proc/$1/exe" 2>/dev/null)
    case "$candidate_exe" in /usr/local/bin/eufy-wall|'/usr/local/bin/eufy-wall (deleted)') ;; *) return 1;; esac
    [ "$(ps -o user= -p "$1" | tr -d ' ')" = wall ] || return 1
    args=$(privileged cat "/proc/$1/cmdline" 2>/dev/null | tr '\000' ' ')
    case "$args" in *' -config /etc/eufy-wall.yaml '*) return 0;; *) return 1;; esac
}
query_client() {
    # is-active alone can be true while the supervisor has died or is starting.
    snapshot=$(systemctl show eufy-wall.service -p ActiveState -p SubState -p MainPID -p ControlGroup) || return 1
    active=$(printf '%s\n' "$snapshot" | sed -n 's/^ActiveState=//p')
    sub=$(printf '%s\n' "$snapshot" | sed -n 's/^SubState=//p')
    pid=$(printf '%s\n' "$snapshot" | sed -n 's/^MainPID=//p')
    group=$(printf '%s\n' "$snapshot" | sed -n 's/^ControlGroup=//p')
    client=unknown
    repair=start
    tasks=
    case "$pid" in ''|*[!0-9]*) return 1;; esac
    executable=$(privileged readlink "/proc/$pid/exe" 2>/dev/null)
    case "$executable" in /usr/local/bin/eufy-wall|'/usr/local/bin/eufy-wall (deleted)') valid_exe=1;; *) valid_exe=0;; esac
    if [ "$active" = active ] && [ "$sub" = running ] && [ "$pid" -gt 0 ] && [ "$valid_exe" = 1 ]; then
        client=on
    elif [ "$active" = active ]; then
        repair=restart
    elif [ "$active" = activating ] || [ "$active" = deactivating ]; then
        repair=wait
    fi
    # Find this deployed client role, not unrelated experimental binaries.
    orphan_pids=
    for candidate in $(pgrep -x eufy-wall 2>/dev/null); do
        owned_client "$candidate" && orphan_pids="$orphan_pids $candidate"
    done
    if { [ "$active" = inactive ] || [ "$active" = failed ]; } && [ "$pid" -eq 0 ] && [ -z "$orphan_pids" ]; then
        # systemd may still be killing child pipelines after the supervisor exits.
        tasks=
        if [ -n "$group" ] && [ -d "/sys/fs/cgroup$group" ]; then
            tasks=$(find "/sys/fs/cgroup$group" -name cgroup.procs -exec cat '{}' \;) || return 1
        fi
        [ -z "$tasks" ] && client=off
    fi
}
pending_request() {
    pending=; pending_unit=
    if [ -r "$runtime/pending" ]; then
        read -r pending pending_unit <"$runtime/pending"
        case "$pending" in on|off) ;; *) pending=;; esac
        # A dead worker cannot permanently mask a crashed/failed operation.
        if [ -n "$pending" ]; then
            worker_snapshot=$(systemctl show "$pending_unit" -p ActiveState -p Job 2>/dev/null)
            worker_state=$(printf '%s\n' "$worker_snapshot" | sed -n 's/^ActiveState=//p')
            worker_job=$(printf '%s\n' "$worker_snapshot" | sed -n 's/^Job=//p')
            case "$worker_state" in
                inactive|failed|'')
                    # --no-block accepts a queued job before its unit becomes
                    # active. Never replace an explicit intent still queued.
                    if [ -z "$worker_job" ] || [ "$worker_job" = 0 ]; then
                        log_event "pending-cleared target=$pending unit=$pending_unit state=${worker_state:-missing} job=${worker_job:-none}"
                        rm -f "$runtime/pending"; pending=
                    fi;;
            esac
        fi
    fi
}
queue() {
    target=$1
    request_origin=$2
    # Caller holds control.lock. Stop old workers before publishing latest intent.
    units=$(systemctl list-units --all --plain --no-legend 'tv-power-cec-*' | awk '{print $1}') || return 1
    for unit in $units; do privileged systemctl --no-block stop "$unit" || return 1; done
    unit="tv-power-cec-$(date +%s)-$$"
    printf '%s %s.service\n' "$target" "$unit" >"$runtime/pending"
    if ! privileged systemd-run --quiet --no-block --collect --unit="$unit" \
        --property=User=dietpi --setenv="TV_POWER_RUNTIME=$runtime" --setenv="CEC_DEVICE=$adapter" \
        "$0" "--worker-$target" "$unit.service"; then
        rm -f "$runtime/pending"; return 1
    fi
    log_event "request origin=$request_origin target=$target unit=$unit.service tv=${tv:-unread} client=${client:-unread} caller_uid=$(id -u)"
}
case "$mode" in
    on|off) control_lock || exit 1; queue "$mode" explicit; exit $?;;
    state)
        # Polling never waits for an actuator loop and never invents TV state.
        query_tv || { echo 'TV power unavailable or transitional.' >&2; exit 1; }
        control_lock || exit 1
        pending_request
        query_client || exit 1
        if [ -n "$pending" ]; then
            if [ "$tv" = "$pending" ] && [ "$client" = "$pending" ]; then
                printf '%s\n' "$pending"; exit 0
            fi
            echo 'Power request is still converging.' >&2; exit 1
        fi
        if [ "$tv" = "$client" ]; then printf '%s\n' "$tv"; exit 0; fi
        # Physical TV state is authoritative in the absence of explicit intent.
        queue "$tv" state-reconcile || exit 1
        echo "Reconciling TV=$tv and client=$client." >&2
        exit 1;;
esac
target=${mode#--worker-}
worker_unit=${2:-}
[ -n "$worker_unit" ] || exit 2
stop_attempt=0
start_cleanup_attempt=0
while :; do
    control_lock || { sleep 1; continue; }
    read -r wanted unit <"$runtime/pending" || exit 1
    [ "$wanted" = "$target" ] && [ "$unit" = "$worker_unit" ] || exit 0
    # Keep service actions inside the intent lock so an older worker cannot
    # reverse a later request after it has been accepted.
    query_client || { release_lock; sleep 3; continue; }
    if [ "$client" != "$target" ]; then
        if [ "$target" = on ]; then
            if [ "$repair" = start ] && { [ -n "$orphan_pids" ] || [ -n "$tasks" ]; }; then
                # A detached old client must release DRM before a managed
                # replacement starts. Never launch duplicate deployed roles.
                start_cleanup_attempt=$((start_cleanup_attempt + 1))
                signal=TERM; [ "$start_cleanup_attempt" -ge 2 ] && signal=KILL
                [ -z "$tasks" ] || privileged systemctl kill --kill-whom=all --signal="$signal" eufy-wall.service || :
                for candidate in $orphan_pids; do
                    if owned_client "$candidate"; then privileged kill "-$signal" "$candidate" || :; fi
                done
            elif [ "$repair" != wait ]; then
                privileged systemctl --no-block "$repair" eufy-wall.service || {
                    release_lock; sleep 3; continue;
                }
            fi
        else
            stop_attempt=$((stop_attempt + 1))
            privileged systemctl --no-block stop eufy-wall.service || :
            if [ "$stop_attempt" -ge 2 ]; then
                signal=TERM; [ "$stop_attempt" -ge 3 ] && signal=KILL
                privileged systemctl kill --kill-whom=all --signal="$signal" eufy-wall.service || :
                # Only exact deployed, wall-owned client/config processes qualify.
                for candidate in $orphan_pids; do
                    if owned_client "$candidate"; then privileged kill "-$signal" "$candidate" || :; fi
                done
            fi
        fi
    fi
    release_lock
    if query_tv; then
        if [ "$tv" = "$target" ]; then
            control_lock || continue
            read -r wanted unit <"$runtime/pending" || exit 1
            [ "$wanted" = "$target" ] && [ "$unit" = "$worker_unit" ] || exit 0
            query_client || exit 1
            if [ "$client" = "$target" ]; then
                rm -f "$runtime/pending"
                echo "Verified TV and client $target."
                exit 0
            fi
            release_lock
        fi
    else
        tv=unknown
    fi
    if [ "$tv" != "$target" ]; then
        # Explicit/reconciled worker intent authorizes retries even if the TV
        # cannot answer pow yet. A state poll alone never invents such intent.
        control_lock || continue
        read -r wanted unit <"$runtime/pending" || exit 1
        [ "$wanted" = "$target" ] && [ "$unit" = "$worker_unit" ] || exit 0
        command='on 0'; [ "$target" = off ] && command='standby 0'
        # Do not hold intent acceptance hostage to an eight-second CEC call.
        # A newer request stops this worker and its generation invalidates any
        # work still in flight; its own worker verifies the final desired state.
        release_lock
        cec "$command" >/dev/null 2>&1 || :
    fi
    sleep 3
done
