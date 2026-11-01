#!/bin/sh
# Install a reversible owned job, probe takeover + recovery, then reboot once.
D=/mnt/us/urbancomp-ink
R=/tmp/urbancomp-ink-runtime
JOB=/etc/upstart/urbancomp-ink.conf
umask 077
exec > "$D/install.log" 2>&1
date '+INSTALL %Y-%m-%d %H:%M:%S'
root_rw=0
modified=0
committed=0
backup=
mode=${1:-reboot}
case "$mode" in reboot|--no-reboot) :;; *) exit 1;; esac

finish() {
    trap - 0 INT TERM HUP
    if [ "$modified" = 1 ] && [ "$committed" = 0 ]; then
        echo 'Installation did not pass: disabling and rolling back our job'
        touch "$D/DISABLED"
        rm -f "$D/ENABLED"
        initctl stop urbancomp-ink 2>/dev/null || true
        [ ! -f "$R/restore.sh" ] || /bin/sh "$R/restore.sh"
        if mntroot rw; then
            root_rw=1
            if [ -f "$backup/urbancomp-ink.conf" ]; then
                cp "$backup/urbancomp-ink.conf" "$JOB"
                chmod 644 "$JOB"
            elif grep -q '^# UrbanComp Ink owned service v1$' "$JOB" 2>/dev/null; then
                rm -f "$JOB"
            fi
        fi
        initctl reload-configuration 2>/dev/null || true
    fi
    if [ "$root_rw" = 1 ]; then sync; mntroot ro; fi
    sync
}
trap finish 0
trap 'exit 1' INT TERM HUP
fail() {
    echo "INSTALL_FAILED: $*"
    for log in /tmp/urbancomp-ink-bootstrap.log /tmp/urbancomp-ink-checksums.log "$R/session.log"; do
        if [ -f "$log" ]; then echo "--- $log ---"; tail -c 16000 "$log"; fi
    done
    exit 1
}

[ "$(id -u)" = 0 ] || fail 'root required'
[ "$(uname -m)" = armv7l ] || fail 'unexpected architecture'
grep -q '^Kindle 5\.12\.2\.2 ' /etc/prettyversion.txt || fail 'unexpected firmware'
[ "$(timeout -t 3 lipc-get-prop com.lab126.volumd driveModeState)" = 0 ] || fail 'exit USB drive mode before installation'
[ "$(timeout -t 3 lipc-get-prop com.lab126.volumd userstoreIsAvailable)" = 1 ] || fail 'user storage unavailable'
cd "$D" || exit 1
sha256sum -c SHA256SUMS || fail 'package checksum'
[ "$(cat tested-driver.txt)" = fbink ] || fail 'run native display test first'
[ -s state/frame.png ] || fail 'missing tested frame'
grep -q 'f_emit framework_ready' /etc/upstart/framework_setup.conf || fail 'boot event not verified'
if [ -f "$JOB" ] && ! grep -q '^# UrbanComp Ink owned service v1$' "$JOB"; then fail 'refusing to replace an unrelated job'; fi
initctl status urbancomp-ink 2>/dev/null | grep -q 'start/running' && fail 'job already running'
battery=$(lipc-get-prop com.lab126.powerd battLevel)
case "$battery" in ''|*[!0-9]*) fail 'cannot read battery';; esac
[ "$battery" -gt 30 ] || fail 'charge above 30 percent before installing'
power_before=$(lipc-get-prop com.lab126.powerd preventScreenSaver)
case "$power_before" in 0|1) :;; *) fail 'cannot read power configuration';; esac

backup="$D/backups/$(date +%Y%m%d-%H%M%S)-$$"
mkdir -p "$backup" || fail 'backup directory'
if [ -f "$JOB" ]; then cp "$JOB" "$backup/urbancomp-ink.conf" || fail 'backup'; else echo absent > "$backup/job-was-absent"; fi
cp /etc/prettyversion.txt "$backup/firmware.txt"
cp /etc/upstart/framework.conf "$backup/framework.conf.readonly"
echo "$power_before" > "$backup/power-before"
touch "$D/DISABLED"
mntroot rw || fail 'cannot mount root writable'
root_rw=1
cp "$D/system/urbancomp-ink.conf" /etc/upstart/urbancomp-ink.conf.new || fail 'stage job'
chmod 644 /etc/upstart/urbancomp-ink.conf.new || fail 'job permissions'
mv /etc/upstart/urbancomp-ink.conf.new "$JOB" || fail 'publish job'
modified=1
sync
mntroot ro || fail 'cannot restore root read-only'
root_rw=0
initctl reload-configuration || fail 'reload job definitions'
initctl status urbancomp-ink || fail 'Upstart rejected the job'
touch "$D/ENABLED"
rm -f "$D/DISABLED"
initctl start urbancomp-ink || fail 'cannot start test session'

ready=0
for attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
    sleep 1
    if grep -q 'CACHED_FRAME_DISPLAYED' "$R/session.log" 2>/dev/null; then ready=1; break; fi
    initctl status urbancomp-ink | grep -q 'start/running' || break
done
[ "$ready" = 1 ] || fail 'native takeover failed; see state/session.log'
sleep 8
initctl status urbancomp-ink | grep -q 'start/running' || fail 'native process stopped early'
cp "$R/session.log" "$D/state/install-probe.log"
initctl stop urbancomp-ink || fail 'cannot stop test session'
[ ! -f "$R/restore-failed" ] || fail 'system UI/power restoration reported an error'
[ "$(lipc-get-prop com.lab126.powerd preventScreenSaver)" = "$power_before" ] || fail 'power recovery check failed'
set -- $(pidof cvm)
[ "$#" = 1 ] || fail 'Kindle UI did not recover'
case "$(awk '{print $3}' "/proc/$1/stat")" in T|t) fail 'Kindle UI still paused';; esac
date '+INSTALL_OK %Y-%m-%d %H:%M:%S' > "$D/state/install-success.txt"
sha256sum ink-native system/urbancomp-ink.conf >> "$D/state/install-success.txt"
echo 'Takeover and power/UI recovery passed.'
committed=1
sync
sleep 2
if [ "$mode" = --no-reboot ]; then
    echo 'Starting the updated screen without a reboot.'
    initctl start urbancomp-ink
else
    echo 'Rebooting to verify autostart.'
    /sbin/reboot
fi
