#!/bin/sh
# Read-only hardware/startup inspection. Does not install a job or change power settings.
BASE=/mnt/us/urbancomp-ink-check
mkdir -p "$BASE" || exit 1
exec > "$BASE/report.txt" 2>&1
echo 'UrbanComp Ink native preflight'
date
id
cat /etc/prettyversion.txt
uname -a
echo '--- available memory and storage ---'
cat /proc/meminfo
df -k / /var/local /mnt/us
echo '--- executables ---'
for c in initctl start stop mntroot lipc-get-prop lipc-set-prop lipc-probe lipc-wait-event curl wget sha256sum timeout; do command -v "$c"; done
echo '--- services ---'
initctl list
echo '--- boot configuration ---'
for f in /etc/upstart/framework.conf /etc/upstart/userstore.conf /etc/upstart/powerd.conf /etc/upstart/wifid.conf; do
  echo "$f"
  if [ -f "$f" ]; then cat "$f"; fi
done
echo '--- power properties ---'
lipc-probe -v com.lab126.powerd
for p in battLevel isCharging preventScreenSaver state; do
  echo "$p"
  lipc-get-prop com.lab126.powerd "$p"
done
echo '--- framebuffer ---'
/var/local/kmc/bin/fbink -e
/var/local/kmc/bin/fbink --help > "$BASE/fbink-help.txt" 2>&1
echo '--- input devices ---'
cat /proc/bus/input/devices
echo '--- curl ---'
curl --version
echo '--- research HTTPS check ---'
curl --fail --connect-timeout 10 --max-time 25 --output "$BASE/research-version.json" 'https://research.urbancomp.dev/api/dashboard?mode=version'
echo "HTTPS exit: $?"
echo '--- complete ---'
sync
