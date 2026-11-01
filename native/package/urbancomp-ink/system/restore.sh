#!/bin/sh
# Idempotent recovery; uses only volatile state saved by our own runner.
R=/tmp/urbancomp-ink-runtime
D=/mnt/us/urbancomp-ink
failed=0
bounded() {
    if [ "$(cat "$R/timeout-mode" 2>/dev/null)" = legacy ]; then timeout -t 3 "$@"; else timeout 3 "$@"; fi
}

same_process() {
    label=$1
    [ -r "$R/$label.pid" ] && [ -r "$R/$label.start" ] || return 1
    pid=$(cat "$R/$label.pid")
    case "$pid" in ''|*[!0-9]*) return 1;; esac
    [ -r "/proc/$pid/stat" ] || return 1
    [ "$(awk '{print $22}' "/proc/$pid/stat")" = "$(cat "$R/$label.start")" ]
}

if same_process app; then kill -TERM "$pid" 2>/dev/null || true; fi
if [ -f "$R/cvm-paused" ] && same_process cvm; then
    kill -CONT "$pid" 2>/dev/null || true
fi
rm -f "$R/cvm-paused"
if [ -f "$R/pillow-disabled" ]; then
    if bounded lipc-set-prop com.lab126.pillow disableEnablePillow enable 2>/dev/null; then
        rm -f "$R/pillow-disabled"
    else failed=1; fi
fi
if [ -f "$R/power-changed" ]; then
    old=$(cat "$R/power-before" 2>/dev/null)
    case "$old" in
        0|1) if bounded lipc-set-prop com.lab126.powerd preventScreenSaver "$old" 2>/dev/null; then rm -f "$R/power-changed"; else failed=1; fi;;
        *) failed=1;;
    esac
fi
for attempt in 1 2; do
    same_process app || break
    kill -0 "$pid" 2>/dev/null || break
    sleep 1
done
if same_process app; then kill -KILL "$pid" 2>/dev/null || true; fi

# Restore only brightness changed by this app, after its light worker stops.
if [ -f "$R/light-changed" ]; then
    original=$(cat "$R/light-before" 2>/dev/null)
    case "$original" in
        ''|*[!0-9]*) failed=1;;
        *) if [ "$original" -le 25 ] && bounded lipc-set-prop com.lab126.powerd flIntensity "$original" 2>/dev/null; then
               rm -f "$R/light-changed" "$R/light-before"
           else failed=1; fi;;
    esac
fi

# Ask the UI to redraw only while not entering USB mode or shutting down.
if [ -f "$R/redraw-home" ]; then
    rm -f "$R/redraw-home"
    drive=$(bounded lipc-get-prop com.lab126.volumd driveModeState 2>/dev/null)
    available=$(bounded lipc-get-prop com.lab126.volumd userstoreIsAvailable 2>/dev/null)
    if [ "$drive" = 0 ] && [ "$available" = 1 ] && initctl status framework 2>/dev/null | grep -q 'start/running'; then
        bounded lipc-set-prop com.lab126.appmgrd start app://com.lab126.booklet.settings 2>/dev/null || true
        sleep 1
        bounded lipc-set-prop com.lab126.appmgrd start app://com.lab126.booklet.home 2>/dev/null || true
    fi
fi
date '+RESTORED %Y-%m-%d %H:%M:%S'
rm -f "$R/app.pid" "$R/app.start" "$R/cvm.pid" "$R/cvm.start"
if [ "$failed" = 0 ]; then rm -f "$R/restore-failed"; else touch "$R/restore-failed"; fi
exit "$failed"
