#!/bin/bash
# Fails if the project's old name has come back. `make check` runs it.
#
# The app is Open Shutter Count. Where spaces can't go:
#   openshuttercount     files, commands, identifiers
#   Open-Shutter-Count   the GitHub repository, its URLs, the DMG's file name
#
# The old name is Maxime Visonneau's project's, so it stays in the credit. The only
# lines allowed to hold it are lines that also name Visonneau, and this file. Word
# anything new to fit that rule. Run it on its own with: bash .github/check-name.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

self=".github/check-name.sh"

# The old name in any case (OpenShutterCounter too), the old name spelt with spaces,
# and the old script's file name.
old='openshuttercounter|open shutter counter|shutter counter\.command'

# Tracked text files (-I skips binaries), a line at a time.
lines="$(git grep -n -I -i -E "$old" -- . ":(exclude)$self" | grep -v -i -E '^[^:]+:[0-9]+:.*visonneau' || true)"

# Tracked file names too, which catches a binary or script that came back under it.
names="$(git ls-files | grep -i -E "$old" || true)"

if [ -z "$lines" ] && [ -z "$names" ]; then
    exit 0
fi

{
    echo "check-name: the old project name is in the tree:"
    echo
    [ -z "$lines" ] || echo "$lines" | sed 's/^/  /'
    [ -z "$names" ] || echo "$names" | sed 's/$/  (file name)/; s/^/  /'
    echo
    echo "The app is Open Shutter Count. Where spaces can't go, use"
    echo "  openshuttercount    for files, commands and identifiers"
    echo "  Open-Shutter-Count  for the GitHub repository, its URLs and the DMG's file name"
    echo "Only a line that also names Maxime Visonneau (the credit) may keep the old name."
} >&2

exit 1
