#!/bin/sh
# Start again after a USB maintenance session. Does not install anything.
D=/mnt/us/urbancomp-ink
exec > "$D/start.log" 2>&1
date '+MANUAL_START %Y-%m-%d %H:%M:%S'
[ "$(id -u)" = 0 ] || exit 1
[ "$(timeout -t 3 lipc-get-prop com.lab126.volumd driveModeState)" = 0 ] || exit 1
[ "$(timeout -t 3 lipc-get-prop com.lab126.volumd userstoreIsAvailable)" = 1 ] || exit 1
[ -f /etc/upstart/urbancomp-ink.conf ] || exit 1
grep -q '^# UrbanComp Ink owned service v1$' /etc/upstart/urbancomp-ink.conf || exit 1
cd "$D" || exit 1
# The service performs the package checksum before executing the native client.
touch ENABLED
rm -f DISABLED
initctl start urbancomp-ink
