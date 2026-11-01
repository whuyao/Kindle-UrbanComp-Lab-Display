#!/bin/sh
# Preserve the failed manual-start evidence, then use the verified boot path.
D=/mnt/us/urbancomp-ink
R=/tmp/urbancomp-ink-runtime
[ "$(id -u)" = 0 ] || exit 1
[ -d "$D/state" ] || exit 1
OUT="$D/state/restart-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$OUT" || exit 1
exec > "$OUT/report.log" 2>&1
date
echo '--- Previous runtime logs (before any restart) ---'
for f in session.log timeout-mode power-before restore-failed app.pid app.start cvm.pid cvm.start; do
    if [ -f "$R/$f" ]; then
        echo "--- $f ---"
        tail -c 64000 "$R/$f" > "$OUT/$f"
        cat "$OUT/$f"
    fi
done
if [ -f /tmp/urbancomp-ink-checksums.log ]; then
    cp /tmp/urbancomp-ink-checksums.log "$OUT/boot-checksums.log"
fi
if [ -f /tmp/urbancomp-ink-bootstrap.log ]; then
    cp /tmp/urbancomp-ink-bootstrap.log "$OUT/bootstrap.log"
fi
echo '--- Service and live power/USB state ---'
initctl status urbancomp-ink
initctl status framework
for prop in battLevel state preventScreenSaver; do
    echo "$prop"
    lipc-get-prop com.lab126.powerd "$prop"
done
cat /sys/devices/platform/fsl-usb2-udc/gadget/connected
for prop in userstoreIsAvailable driveModeState; do
    echo "$prop"
    lipc-get-prop com.lab126.volumd "$prop"
done
for pid in $(pidof cvm ink-native); do
    echo "--- process $pid ---"
    cat "/proc/$pid/comm"
    grep '^State:\|^VmRSS:\|^VmHWM:' "/proc/$pid/status"
done
grep 'urbancomp-ink\|ink-native' /var/log/messages 2>/dev/null | tail -80
echo '--- Package and boot configuration ---'
cd "$D" || exit 1
sha256sum -c SHA256SUMS || exit 1
[ -f ENABLED ] && [ ! -f DISABLED ] || exit 1
grep -q '^# UrbanComp Ink owned service v1$' /etc/upstart/urbancomp-ink.conf || exit 1
cp /etc/upstart/urbancomp-ink.conf "$OUT/urbancomp-ink.conf"
echo 'Diagnostics saved. Restarting into the previously verified boot service.'
sync
sleep 2
/sbin/reboot
