#!/bin/sh
set -u
D=/mnt/us/urbancomp-ink
R=/tmp/urbancomp-ink-runtime
umask 077
mkdir -p "$R"
if timeout 1 /bin/sh -c ':' 2>/dev/null; then echo plain > "$R/timeout-mode"
elif timeout -t 1 /bin/sh -c ':' 2>/dev/null; then echo legacy > "$R/timeout-mode"
else echo 'No compatible bounded command runner'; exit 1; fi
bounded() {
    if [ "$(cat "$R/timeout-mode")" = legacy ]; then timeout -t 3 "$@"; else timeout 3 "$@"; fi
}

storage_available() {
    [ "$(bounded lipc-get-prop com.lab126.volumd driveModeState 2>/dev/null)" = 0 ] || return 1
    [ "$(bounded lipc-get-prop com.lab126.volumd userstoreIsAvailable 2>/dev/null)" = 1 ] || return 1
    awk '$2 == "/mnt/us" && $4 ~ /(^|,)rw(,|$)/ {ok=1} END {exit !ok}' /proc/mounts || return 1
}
storage_ok() {
    storage_available || return 1
    [ -f "$D/ENABLED" ] && [ ! -f "$D/DISABLED" ]
}
save_log() {
    storage_available || return 0
    tail -c 60000 "$R/session.log" > "$D/state/session.log.tmp" && mv "$D/state/session.log.tmp" "$D/state/session.log"
}
archive_log() {
    storage_available || return 0
    prior="$R/session.log"
    [ -s "$prior" ] || prior="$D/state/session.log"
    # Rejected low-battery starts must not overwrite the last real run.
    grep -q 'NATIVE_RUNNING' "$prior" 2>/dev/null || return 0
    for number in 2 1; do
        next=$((number + 1))
        if [ -f "$D/state/session.$number.log" ]; then
            cp "$D/state/session.$number.log" "$D/state/session.$next.log.tmp" && mv "$D/state/session.$next.log.tmp" "$D/state/session.$next.log"
        fi
    done
    tail -c 60000 "$prior" > "$D/state/session.1.log.tmp" && mv "$D/state/session.1.log.tmp" "$D/state/session.1.log"
}
flush_exit() {
    [ -f "$R/exit-pending" ] || return 0
    storage_available || return 0
    cp "$R/last-exit.json" "$D/state/last-exit.json.tmp" && mv "$D/state/last-exit.json.tmp" "$D/state/last-exit.json" || return 0
    if [ -f "$D/state/supervisor-events.jsonl" ]; then
        tail -n 63 "$D/state/supervisor-events.jsonl" > "$D/state/supervisor-events.jsonl.tmp"
    else : > "$D/state/supervisor-events.jsonl.tmp"; fi
    cat "$R/last-exit.json" >> "$D/state/supervisor-events.jsonl.tmp" || return 0
    mv "$D/state/supervisor-events.jsonl.tmp" "$D/state/supervisor-events.jsonl" && rm -f "$R/exit-pending"
}
record_exit() {
    case "$battery" in ''|*[!0-9]*) battery=null;; esac
    printf '{"at":"%s","reason":"%s","battery_percent":%s,"native_pid":%s,"native_exit_status":%s,"restore_status":%s}\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$reason" "$battery" "$app" "$app_status" "$restore_status" > "$R/last-exit.json"
    touch "$R/exit-pending"
    flush_exit
}

archive_log
flush_exit
: > "$R/session.log"
exec >> "$R/session.log" 2>&1
date '+START %Y-%m-%d %H:%M:%S'
reason=setup_failed
battery=null
app=null
app_status=null
cleanup() {
    trap - 0 INT TERM HUP
    restore_status=0
    /bin/sh "$R/restore.sh" || restore_status=$?
    echo "EXIT_REASON $reason battery=$battery app_status=$app_status restore=$restore_status"
    record_exit
    save_log
}
trap cleanup 0
trap 'reason=service_stop; exit 0' INT TERM HUP

[ "$(id -u)" = 0 ] || exit 1
storage_ok || exit 0
cd "$D" || exit 1
# Upstart has already verified the whole package before copying this runner.
case "$(cat "$D/tested-driver.txt" 2>/dev/null)" in fbink) driver=fbink;; *) echo 'No validated FBInk driver'; exit 1;; esac
[ -s "$D/state/frame.png" ] || { echo 'No tested cached frame'; exit 1; }
mkdir -p "$D/state"
cp "$D/ink-native" "$R/ink-native.new" || exit 1
chmod 700 "$R/ink-native.new"
mv "$R/ink-native.new" "$R/ink-native" || exit 1
# Do not keep the current directory on a disk which USB mode will unmount.
cd "$R" || exit 1

# Let framework boot / the book launcher's notification settle.
sleep 5
storage_ok || exit 0
battery=$(bounded lipc-get-prop com.lab126.powerd battLevel 2>/dev/null)
case "$battery" in ''|*[!0-9]*) reason=battery_read_failed; echo 'Cannot read battery'; exit 1;; esac
[ "$battery" -gt 20 ] || { reason=low_battery_start; echo "Low battery ($battery%): retain normal Kindle power management"; exit 0; }
old=$(bounded lipc-get-prop com.lab126.powerd preventScreenSaver 2>/dev/null)
case "$old" in 0|1) printf '%s\n' "$old" > "$R/power-before";; *) echo 'Cannot read power state'; exit 1;; esac
set -- $(pidof cvm)
[ "$#" = 1 ] || { echo 'Expected one Kindle UI process'; exit 1; }
cvm=$1
[ "$(cat "/proc/$cvm/comm")" = cvm ] || exit 1
case "$(awk '{print $3}' "/proc/$cvm/stat")" in T|t) echo 'UI already paused by another program'; exit 1;; esac
printf '%s\n' "$cvm" > "$R/cvm.pid"
awk '{print $22}' "/proc/$cvm/stat" > "$R/cvm.start"
touch "$R/power-changed"
bounded lipc-set-prop com.lab126.powerd preventScreenSaver 1 || exit 1
touch "$R/pillow-disabled"
bounded lipc-set-prop com.lab126.pillow disableEnablePillow disable || exit 1
touch "$R/cvm-paused" "$R/redraw-home"
kill -STOP "$cvm" || exit 1

"$R/ink-native" -dir "$D" -driver "$driver" &
app=$!
printf '%s\n' "$app" > "$R/app.pid"
awk '{print $22}' "/proc/$app/stat" > "$R/app.start"
echo "NATIVE_RUNNING app=$app cvm=$cvm interval=1800"
echo "BATTERY_START $battery"
save_log
tick=0
while kill -0 "$app" 2>/dev/null; do
    # Upstart's userstore event and the native mount guard handle USB.
    # Slow LIPC polling here avoids several process launches every second.
    if [ "$((tick % 10))" = 0 ]; then
        storage_ok || { reason=storage_unavailable_or_disabled; echo 'USB or disabled: returning screen to Kindle'; exit 0; }
        power=$(bounded lipc-get-prop com.lab126.powerd state 2>/dev/null)
        case "$power" in active) :;; *) reason=power_state; echo "Power state $power: returning to Kindle"; exit 0;; esac
    fi
    if [ "$((tick % 60))" = 0 ]; then
        battery=$(bounded lipc-get-prop com.lab126.powerd battLevel 2>/dev/null)
        case "$battery" in ''|*[!0-9]*) reason=battery_read_failed; echo 'Battery read failed'; exit 0;; esac
        [ "$battery" -gt 20 ] || { reason=low_battery; echo "Battery $battery% at or below 20%: returning to Kindle"; exit 0; }
        if [ "$((tick % 1800))" = 0 ]; then echo "BATTERY_SAMPLE $battery"; fi
        save_log
        # Keep the volatile log bounded over long unattended runs.
        size=$(wc -c < "$R/session.log")
        if [ "$size" -gt 200000 ]; then : > "$R/session.log"; fi
    fi
    tick=$((tick + 1))
    sleep 1
done
wait "$app"
app_status=$?
reason=native_exit
if grep -q 'HOME_BUTTON:' "$R/session.log"; then reason=home_button; fi
echo "Native process exited: $app_status"
exit 0
