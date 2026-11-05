#!/bin/bash
# Double-click this in Finder to build, sign and package a release of Open Shutter Count
# from main: it runs the tests, then app/scripts/package.sh, and shows the disk image in
# Finder. Publishing it is the release checklist in docs/DEVELOPMENT.md.

cd "$(dirname "$0")/../.." || exit 1

finish() {
    echo
    read -r -p "Press Return to close this window. "
    exit "$1"
}

echo "Running the tests…"
if ! python3 -m pytest -q app/tests; then
    echo "The tests failed, so nothing was packaged."
    finish 1
fi

echo
if ! DMG=$(app/scripts/package.sh); then
    echo "Packaging didn't work; what went wrong is above."
    finish 1
fi

echo "Made $DMG"
open -R "$DMG"
finish 0
