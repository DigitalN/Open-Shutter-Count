#!/bin/bash
# Runs at the start of every Claude session, on the Mac or in the cloud.
set -euo pipefail

cd "${CLAUDE_PROJECT_DIR:-.}"

# Turn on .githooks/pre-push, which runs `make check` before every push.
git config core.hooksPath .githooks

# A cloud container has Go but not pytest, which `make check` needs. The rest
# of app/requirements.txt (pywebview, PyObjC, PyInstaller) is Mac only.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

pyhashes="$(python3 -I -c 'import sysconfig; print(sysconfig.get_path("purelib"))')/.session-start.sha256"
if ! sha256sum --check --status "$pyhashes" 2>/dev/null; then
  rm -f "$pyhashes"
  python3 -m pip install -q --root-user-action=ignore "$(grep '^pytest==' app/requirements.txt)"
  sha256sum .claude/hooks/session-start.sh app/requirements.txt > "$pyhashes" || rm -f "$pyhashes"
fi
