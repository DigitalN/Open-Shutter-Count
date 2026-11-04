#!/bin/bash
# Double-click this in Finder to read the shutter count of the Canon camera
# plugged in over USB.
#
# Each run saves what it printed, plus debug logs, and a trace of everything
# exchanged with the camera, in mac/readings. Those two files are what to send
# along when something goes wrong.

here="$(cd "$(dirname "$0")/mac" && pwd)"
# shellcheck source=mac/launcher.sh
. "$here/launcher.sh"

log="$readings/$stamp.txt"
trace="$readings/$stamp-trace.json"

check_setup
start "Read Shutter Count"
echo "Reading:" >> "$log"
echo "Reading the camera..."

run --raw --trace "$trace" --note "Read Shutter Count.command, $stamp"
status=$?
echo

if [ "$status" -eq 0 ]; then
    echo "Saved in mac/readings/$stamp.txt"
else
    echo "That didn't work. The details are in mac/readings:"
    echo "  $stamp.txt"
    [ -f "$trace" ] && echo "  $stamp-trace.json"
fi

finish "$status"
