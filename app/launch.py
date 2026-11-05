"""Where Open Shutter Count runs from, and relaunching it.

Adapted from Mac Engine Room's launch.py (MIT; see THIRD-PARTY-NOTICES).

Open Shutter Count has to run from an Applications folder to update itself. Opened
straight from the downloaded disk image or the Downloads folder, macOS runs a temporary
hidden copy ("app translocation") that can't replace itself. On launch from anywhere
else, it offers to move.

Everything here is a no-op when running from source (`python3 app/main.py`), where the
"app" is Python itself.
"""

import logging
import os
import plistlib
import subprocess
import sys
from pathlib import Path

log = logging.getLogger("openshuttercount")

APP_NAME = "Open Shutter Count"
REPOSITORY = "DigitalN/openshuttercounter"
REPOSITORY_URL = f"https://github.com/{REPOSITORY}"
RELEASES_URL = f"{REPOSITORY_URL}/releases/latest"

# Waits for the quitting copy to exit, opens the app, and checks that it's running, trying
# once more if it isn't after 5 seconds. `open -n`: LaunchServices can still list an app as
# running for a moment after it has quit, and a plain `open` then goes to the instance that's
# gone, so nothing starts (seen updating Mac Engine Room from 1.2.8 to 1.2.9). The check
# matches the app's own executable exactly.
RELAUNCH_SCRIPT = (
    'pid=$1; exe=$2; shift 2; tries=0\n'
    'while kill -0 "$pid" 2>/dev/null && [ $tries -lt 100 ]; do sleep 0.1; tries=$((tries + 1)); done\n'
    'for attempt in 1 2; do\n'
    '  /usr/bin/open -n "$@"\n'
    '  tries=0\n'
    '  while [ $tries -lt 50 ]; do\n'
    '    /bin/ps -axo comm= | /usr/bin/grep -qxF -- "$exe" && exit 0\n'
    '    sleep 0.1; tries=$((tries + 1))\n'
    '  done\n'
    'done\n'
)


def bundle_path():
    """The running app bundle, or None when running from source."""
    if not getattr(sys, "frozen", False):
        return None
    from Foundation import NSBundle

    return Path(NSBundle.mainBundle().bundlePath())


def in_applications_folder(path):
    resolved = str(Path(path).resolve())
    folders = ("/Applications/", str(Path.home() / "Applications") + "/")
    return resolved.startswith(folders)


def activate_app():
    import AppKit

    app = AppKit.NSApp
    if hasattr(app, "activate"):
        app.activate()
    else:
        app.activateIgnoringOtherApps_(True)


def offer_to_move(source):
    """Asks to move the app into Applications. Returns True if it's relaunching from there."""
    import AppKit
    from Foundation import NSURL, NSFileManager

    activate_app()
    alert = AppKit.NSAlert.alloc().init()
    alert.setMessageText_(f"Move {APP_NAME} to your Applications folder?")
    alert.setInformativeText_(f"{APP_NAME} needs to be in Applications to keep itself up to date.")
    alert.addButtonWithTitle_("Move to Applications")
    alert.addButtonWithTitle_("Not Now")
    if alert.runModal() != AppKit.NSAlertFirstButtonReturn:
        return False

    manager = NSFileManager.defaultManager()
    for folder in (Path("/Applications"), Path.home() / "Applications"):
        destination = folder / source.name
        try:
            folder.mkdir(parents=True, exist_ok=True)
            if destination.exists():
                ok, _, error = manager.trashItemAtURL_resultingItemURL_error_(
                    NSURL.fileURLWithPath_(str(destination)), None, None)
                if not ok:
                    raise OSError(str(error))
            subprocess.run(["/usr/bin/ditto", str(source), str(destination)], check=True, capture_output=True)
        except (OSError, subprocess.CalledProcessError) as e:
            log.warning("Couldn't move %s to %s: %s", APP_NAME, folder, e)
            continue
        # The user already chose to open this download; don't make them approve the copy again.
        subprocess.run(["/usr/bin/xattr", "-dr", "com.apple.quarantine", str(destination)], capture_output=True)
        log.info("Moved %s to %s; relaunching", APP_NAME, destination)
        relaunch(destination)
        return True

    failure = AppKit.NSAlert.alloc().init()
    failure.setMessageText_(f"{APP_NAME} couldn't move itself")
    failure.setInformativeText_(
        f"Quit {APP_NAME}, drag it into your Applications folder in Finder, then open it from there.")
    failure.runModal()
    return False


def relaunch(app):
    """Opens `app` once this process has exited. The caller quits right after.

    Waiting for the exit matters: until then macOS still counts this copy as the running
    app, and opening it again would only bring this one forward. See RELAUNCH_SCRIPT.
    """
    subprocess.Popen(["/bin/sh", "-c", RELAUNCH_SCRIPT, "sh", str(os.getpid()), executable(app), str(app)],
                     stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                     start_new_session=True)


def executable(app):
    """The app's main executable, as `ps` shows it once it runs."""
    try:
        with open(Path(app) / "Contents" / "Info.plist", "rb") as f:
            name = plistlib.load(f).get("CFBundleExecutable")
    except (OSError, plistlib.InvalidFileException):
        name = None
    return str(Path(app) / "Contents" / "MacOS" / (name or Path(app).stem))
