#!/bin/bash
# Double-click this in Finder to look for where the camera keeps its shutter
# count. It reads everything the camera reports, including Canon's event
# channel, asks you to take exact numbers of photos with each shutter mode,
# reads it all again, and shows what went up by those numbers.
#
# Besides standard PTP reads, it sends Canon's tethering commands, as EOS
# Utility does: SetRemoteMode, SetEventMode, GetEvent and
# RequestDevicePropValue. Nothing that changes a setting or a file.

here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=launcher.sh
. "$here/launcher.sh"

log="$readings/$stamp-probe.txt"
before="$readings/$stamp-probe-before.json"
after="$readings/$stamp-probe-after.json"

failed() {
    echo
    echo "That didn't work. Commit and push the .txt file in mac/readings starting"
    echo "with $stamp."
    finish 1
}

# ask_count asks how many photos were taken, with $2 as the default, and
# leaves the answer in $count.
ask_count() {
    read -r -p "$1 (Return for $2) " count
    count="${count:-$2}"

    case "$count" in
        '' | *[!0-9]*)
            echo "That isn't a number of photos."
            finish 1
            ;;
    esac
}

check_setup
start "Find Shutter Counter"

echo "Step 1 of 2: reading everything the camera reports..."
echo "Probe before:" >> "$log"
run probe --events --out "$before" --trace "$readings/$stamp-probe-before-trace.json" --note "before" || failed

cat <<'EOF'

Now:
  1. Unplug the camera from the Mac. Leave this window open.
  2. Set Shutter mode to Elec. 1st-curtain. Take exactly 20 photos,
     counting carefully.
  3. Set Shutter mode to Electronic. Take exactly 7 photos.
  4. Plug the camera back in, switched on.

EOF

ask_count "How many photos with Elec. 1st-curtain?" 20
frames="$count"
ask_count "How many photos with Electronic?" 7
electronic="$count"

if [ "$frames" -eq "$electronic" ]; then
    echo "Those need to be different numbers, or the comparison can't tell them apart."
    finish 1
fi

echo
echo "Step 2 of 2: reading it all again..."
echo "Probe after $frames Elec. 1st-curtain and $electronic Electronic photos:" >> "$log"
run probe --events --out "$after" --trace "$readings/$stamp-probe-after-trace.json" \
    --note "after $frames Elec. 1st-curtain and $electronic Electronic photos" || failed

echo "Comparison:" >> "$log"
run compare "$before" "$after" --frames "$frames" --electronic "$electronic" || failed

echo "Commit and push the .txt file in mac/readings starting with $stamp."

finish 0
