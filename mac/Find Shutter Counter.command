#!/bin/bash
# Double-click this in Finder to look for where the camera keeps its shutter
# count. It sends only standard PTP read requests: it reads everything the
# camera reports, asks you to take an exact number of photos, reads it all
# again, and shows what went up by that number.

here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=launcher.sh
. "$here/launcher.sh"

log="$readings/$stamp-probe.txt"
before="$readings/$stamp-probe-before.json"
after="$readings/$stamp-probe-after.json"

failed() {
    echo
    echo "That didn't work. Send the files in the readings folder starting with"
    echo "$stamp."
    finish 1
}

start "Find Shutter Counter"

echo "Step 1 of 2: reading everything the camera reports..."
echo "Probe before:" >> "$log"
run probe --out "$before" --trace "$readings/$stamp-probe-before-trace.json" --note "before" || failed

cat <<'EOF'

Now:
  1. Unplug the camera from the Mac. Leave this window open.
  2. Check the camera's Shutter mode is Elec. 1st-curtain, not Electronic,
     so the shutter actually moves.
  3. Take exactly 20 photos, counting carefully.
  4. Plug the camera back in, switched on.

EOF

read -r -p "How many photos did you take? (Return for 20) " frames
frames="${frames:-20}"

case "$frames" in
    '' | *[!0-9]*)
        echo "That isn't a number of photos."
        finish 1
        ;;
esac

echo
echo "Step 2 of 2: reading it all again..."
echo "Probe after $frames photos:" >> "$log"
run probe --out "$after" --trace "$readings/$stamp-probe-after-trace.json" --note "after $frames photos" || failed

echo "Comparison:" >> "$log"
run compare "$before" "$after" --frames "$frames" || failed

echo "Send me the files in the readings folder starting with $stamp."

finish 0
