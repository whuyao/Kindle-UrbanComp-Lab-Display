#!/bin/sh
# Remove only our service. Keep cached research and backups.
D=/mnt/us/urbancomp-ink
JOB=/etc/upstart/urbancomp-ink.conf
exec > "$D/remove.log" 2>&1
[ "$(id -u)" = 0 ] || exit 1
touch "$D/DISABLED"
rm -f "$D/ENABLED"
initctl stop urbancomp-ink 2>/dev/null || true
[ ! -f /tmp/urbancomp-ink-runtime/restore.sh ] || /bin/sh /tmp/urbancomp-ink-runtime/restore.sh
[ -f "$JOB" ] || exit 0
grep -q '^# UrbanComp Ink owned service v1$' "$JOB" || exit 1
mntroot rw || exit 1
trap 'sync; mntroot ro' 0
rm -f "$JOB"
initctl reload-configuration
date '+REMOVED %Y-%m-%d %H:%M:%S'
