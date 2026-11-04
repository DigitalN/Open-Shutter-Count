#!/bin/bash
# Double-click this in Finder to read the shutter count of the Canon camera
# plugged in over USB.
#
# Each run saves what it printed, plus debug logs, and a trace of everything
# exchanged with the camera, in the "readings" folder next to this file. Those
# two files are what to send along when something goes wrong.

here="$(cd "$(dirname "$0")" && pwd)"
tool="$here/openshuttercounter"
readings="$here/readings"
stamp="$(date +%Y-%m-%d_%H-%M-%S)"
log="$readings/$stamp.txt"
trace="$readings/$stamp-trace.json"

finish() {
    echo
    read -r -p "Press Return to close this window. "
    close_window
    exit "$1"
}

# Closes this Terminal window just after the script exits. Waiting for the
# exit means Terminal doesn't ask whether to stop a running process. nohup
# keeps it alive when the window's shell goes away.
close_window() {
    local me
    me="$(tty)" || return
    nohup /usr/bin/osascript - "$me" >/dev/null 2>&1 <<'EOF' &
on run argv
    delay 0.5
    tell application "Terminal"
        repeat with w in windows
            if (count of tabs of w) is 1 and tty of tab 1 of w is (item 1 of argv) then
                close w
                return
            end if
        end repeat
    end tell
end run
EOF
}

clear

if [ "$(uname -m)" != "arm64" ]; then
    echo "This copy is built for Apple silicon Macs, and this Mac is $(uname -m)."
    finish 1
fi

if [ ! -x "$tool" ]; then
    echo "The tool is missing from $here."
    echo "Fetch the repository again, or run mac/build.sh."
    finish 1
fi

# A copy downloaded through a browser, rather than fetched with git, is
# quarantined, and macOS would refuse to run the tool or load libusb.
xattr -d com.apple.quarantine "$tool" "$here/libusb-1.0.dylib" 2>/dev/null

mkdir -p "$readings"
{
    echo "Read Shutter Count, $stamp"
    sw_vers
    echo "Mac: $(sysctl -n hw.model) ($(uname -m))"
    echo
    echo "USB devices (vendor:product):"
    "$tool" list --all 2>&1
    echo
    echo "Reading:"
} > "$log"

echo "Reading the camera..."

# What the tool prints goes to the screen and the log; its debug logging goes
# to the log only.
"$tool" --raw --trace "$trace" --log-level debug \
    --note "Read Shutter Count.command, $stamp" 2>>"$log" | tee -a "$log"
status=${PIPESTATUS[0]}

echo "exit status $status" >> "$log"
echo

if [ "$status" -eq 0 ]; then
    echo "Saved in readings/$stamp.txt"
else
    echo "That didn't work. The details are in the readings folder:"
    echo "  $stamp.txt"
    [ -f "$trace" ] && echo "  $stamp-trace.json"
fi

finish "$status"
