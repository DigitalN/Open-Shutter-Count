# Used by Find Shutter Count.command; not run on its own.
#
# Set $here to this folder before sourcing it. It sets $tool, $readings and
# $stamp, and defines check_setup, start, run and finish. Set $log before
# calling start.

tool="$here/openshuttercount"
readings="$here/readings"
stamp="$(date +%Y-%m-%d_%H-%M-%S)"

# check_setup asks whether the camera is set up, and stops if it isn't.
# Nothing has been sent to the camera at this point.
check_setup() {
    clear
    cat <<'SETUP'
Before reading, set up the camera:

  1. USB connection set to photo transfer:
       MENU > Setup (wrench) > Choose USB connection app >
       Photo Import/Remote Control
     (On the EOS R8 it's on page 4 of the Setup menu.)
  2. Wi-Fi and Bluetooth off.
  3. A card in, and the camera switched on.
  4. Plugged straight into this Mac with a data cable, not a hub. Allow it
     if macOS asks whether the accessory may connect.
  5. Apps that use cameras closed: Photos, Image Capture, Preview,
     Lightroom, EOS Utility and the like.

SETUP

    local answer
    read -r -p "Have you done all of that? (y/n) " answer

    case "$answer" in
        [yY]*) ;;
        *)
            echo
            echo "Do those first, then double-click this again."
            finish 1
            ;;
    esac
}

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
        echo "Download the repository again, or run tool/mac/build.sh."
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
