#!/bin/bash
# Builds Open Shutter Count.app and prints its path. Used by package.sh.
#
#   app/scripts/build.sh            sign with your Apple Development certificate
#   app/scripts/build.sh --adhoc    sign locally only (that copy doesn't check for updates)
#
# Installed copies only accept updates signed by the same developer team, so releases
# must be signed with a certificate from the same Apple account every time (team
# XT4LT7MS6Y, the same as Mac Engine Room's).
#
# Adapted from Mac Engine Room's scripts/build.sh (MIT; see THIRD-PARTY-NOTICES).

set -euo pipefail
cd "$(dirname "$0")/../.."

# Build outside the repo: folders synced by iCloud Drive (like ~/Documents) add
# Finder metadata to app bundles, which codesign rejects.
WORK="$HOME/Library/Caches/OpenShutterCount-build"
APP="$WORK/dist/Open Shutter Count.app"
LOG="$WORK/build.log"

if [[ "${1:-}" == "--adhoc" ]]; then
    IDENTITY="-"
    echo "Signing locally (ad hoc); this copy won't check for updates." >&2
else
    # The first *valid* Apple Development identity (expired certificates stay in the keychain).
    IDENTITY=$(security find-identity -v -p codesigning 2>/dev/null | awk '/"Apple Development/ {print $2; exit}')
    if [[ -z "$IDENTITY" ]]; then
        cat >&2 <<'MSG'
No Apple Development certificate found, so signing locally (ad hoc): this copy works
but won't check for updates. A free Apple ID is enough to get one: Xcode → Settings →
Apple Accounts → your "(Personal Team)" → Manage Certificates… → + → Apple Development.
MSG
        IDENTITY="-"
    fi
fi

# One version for the app and the tool inside it. The tool is built by tool/mac/build.sh
# (on Linux or a Mac) and committed, so this Mac needs only Python.
VERSION=$(sed -n 's/^VERSION = "\(.*\)"$/\1/p' app/version.py)
TOOL_VERSION=$(tool/mac/openshuttercount --version 2>/dev/null | awk '{print $NF}')
if [[ "$TOOL_VERSION" != "$VERSION" ]]; then
    echo "tool/mac/openshuttercount is version '${TOOL_VERSION}', and app/version.py says $VERSION." >&2
    echo "Rebuild it with: bash tool/mac/build.sh (then commit it)." >&2
    exit 1
fi

rm -rf "$WORK"
mkdir -p "$WORK"

echo "Drawing the icon…" >&2
python3 app/scripts/make_icon.py "$WORK/OpenShutterCount.iconset"
iconutil -c icns "$WORK/OpenShutterCount.iconset" -o "$WORK/OpenShutterCount.icns"

echo "Building…" >&2
if ! OPENSHUTTERCOUNT_ICON="$WORK/OpenShutterCount.icns" python3 -m PyInstaller app/OpenShutterCount.spec \
    --noconfirm --clean --distpath "$WORK/dist" --workpath "$WORK/build" >"$LOG" 2>&1; then
    tail -20 "$LOG" >&2
    echo "Build failed; full log: $LOG" >&2
    exit 1
fi

# The tool and libusb side by side: the tool looks for libusb next to itself first.
cp tool/mac/openshuttercount tool/mac/libusb-1.0.dylib "$APP/Contents/MacOS/"
chmod 755 "$APP/Contents/MacOS/openshuttercount"

xattr -cr "$APP"
# --deep signs everything PyInstaller bundled, the tool and libusb included.
if ! codesign --force --deep -s "$IDENTITY" "$APP" >>"$LOG" 2>&1 \
    || ! codesign --verify --deep --strict "$APP" >>"$LOG" 2>&1; then
    tail -5 "$LOG" >&2
    echo "Signing failed; full log: $LOG" >&2
    exit 1
fi

if ! "$APP/Contents/MacOS/openshuttercount" --version >/dev/null 2>&1; then
    echo "The tool inside the app doesn't run." >&2
    exit 1
fi

TEAM=$(codesign -d --verbose=2 "$APP" 2>&1 | sed -n 's/^TeamIdentifier=//p')
echo "Built $VERSION, signed (team: $TEAM)." >&2

echo "$APP"
