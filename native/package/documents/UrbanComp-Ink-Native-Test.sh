#!/bin/sh
# One-time native test. No autostart or root partition changes.
DIR=/mnt/us/urbancomp-ink
exec > "$DIR/native-test.log" 2>&1
echo 'UrbanComp Ink native test'
date
id
echo '--- Read-only boot and USB details ---'
for f in /etc/upstart/filesystems_userstore.conf /etc/upstart/framework /etc/upstart/kmc.conf; do
  echo "$f"
  if [ -f "$f" ]; then cat "$f"; fi
done
grep -n 'framework_ready\|usbPlug\|usbUnplug\|userstore' /etc/upstart/*.conf
lipc-probe -a com.lab126.volumd
echo '--- Kernel USB state ---'
for f in /sys/devices/platform/fsl-usb2-udc/gadget/connected /sys/devices/platform/fsl-usb2-udc/gadget/suspended; do
  echo "$f"
  if [ -r "$f" ]; then cat "$f"; fi
done
cd "$DIR" || exit 1
sha256sum -c SHA256SUMS || exit 1
cp "$DIR/ink-native" /tmp/urbancomp-ink-test || exit 1
chmod 700 /tmp/urbancomp-ink-test
echo '--- Native HTTPS and rendering ---'
/tmp/urbancomp-ink-test -dir "$DIR" -once -no-display
result=$?
echo "Native exit: $result"
if [ "$result" -ne 0 ]; then rm -f /tmp/urbancomp-ink-test; exit "$result"; fi
cat "$DIR/state/receipt.json"
echo '--- Full-frame display test ---'
# Allow the script-launcher's own notification to disappear.
sleep 4
LD_LIBRARY_PATH=/var/local/kmc/lib /var/local/kmc/bin/fbink -g "file=$DIR/state/frame.png" -W GC16 -f
result=$?
echo "FBInk image exit: $result"
if [ "$result" -ne 0 ]; then
  echo 'Trying stock eips PNG rendering'
  /usr/bin/eips -g "$DIR/state/frame.png"
  result=$?
  echo "eips image exit: $result"
  if [ "$result" -eq 0 ]; then echo eips > "$DIR/tested-driver.txt"; fi
else
  echo fbink > "$DIR/tested-driver.txt"
fi
rm -f /tmp/urbancomp-ink-test
echo '--- memory after test ---'
cat /proc/meminfo
echo '--- test complete ---'
sync
exit "$result"
