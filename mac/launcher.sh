# Shared by the .command launchers in this folder; not run on its own.
#
# Set $here to this folder before sourcing it. It sets $tool, $readings and
# $stamp, and defines start, run and finish. Set $log before calling start.

tool="$here/openshuttercounter"
readings="$here/readings"
stamp="$(date +%Y-%m-%d_%H-%M-%S)"

# start checks the Mac and the tool, then begins $log with the title given,
# the macOS version and the USB devices macOS can see.
start() {
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
        echo "$1, $stamp"
        sw_vers
        echo "Mac: $(sysctl -n hw.model) ($(uname -m))"
        echo
        echo "USB devices (vendor:product):"
        "$tool" list --all 2>&1
        echo
    } > "$log"
}

# run runs the tool. What it prints goes to the screen and $log; its debug
# logging goes to $log only. Returns the tool's exit status.
run() {
    "$tool" --log-level debug "$@" 2>>"$log" | tee -a "$log"
    local status=${PIPESTATUS[0]}
    echo "exit status $status" >> "$log"
    return "$status"
}

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
