"""The list of tested cameras, fetched from GitHub once per launch.

A reading says "tested" when the list has the camera's model and the way it was read;
any other says it's not fully tested. The openshuttercounter tool has the list built in
(tool/pkg/manufacturers/canon/tested.json in the repository). This fetches the same file
from the repository's main branch and hands it to the tool's read with --tested-list, so
a camera can be marked tested without a release.

The list only changes that label and the failure headline, never what's sent to the
camera, which is why it needn't be signed. Whatever goes wrong here (offline, GitHub
down, a file that isn't a list) the read goes ahead without --tested-list, on the tool's
built-in list.

It goes through /usr/bin/curl like the update check, with the same 15-second limit, from
raw.githubusercontent.com: not GitHub's API, which the update check uses and which allows
60 requests an hour from one address. Nothing is saved between launches.

Copies run from source or signed ad hoc skip it, as they skip the update check.
OPENSHUTTERCOUNT_TESTED_LIST (a URL; file:// works) makes any copy fetch from there
instead, development copies included.
"""

import json
import logging
import os
import subprocess
import threading
from dataclasses import dataclass

from launch import REPOSITORY
from updater import CHECK_TIMEOUT, own_team

log = logging.getLogger("openshuttercount")

# The file's path in the repository is permanent: installed copies fetch it by this URL.
PATH = "tool/pkg/manufacturers/canon/tested.json"
URL = f"https://raw.githubusercontent.com/{REPOSITORY}/main/{PATH}"
ENVIRONMENT = "OPENSHUTTERCOUNT_TESTED_LIST"
TIMEOUT = CHECK_TIMEOUT  # seconds, as the update check
MAX_SIZE = 1 << 20  # bytes; the tool won't read a bigger list either
GITHUB = "GitHub"
DEVELOPMENT_COPY = "development copy"


class FetchError(Exception):
    pass


@dataclass(frozen=True)
class Fetched:
    """What the fetch came to. `origin` is where the list comes from: GitHub, or the URL
    OPENSHUTTERCOUNT_TESTED_LIST gave. `text` is the list to pass to the tool, with
    `entries` in it, or None, with `reason` saying why."""

    origin: str
    text: str | None = None
    entries: int = 0
    reason: str | None = None


def parse(body):
    """How many entries a fetched list has. The app only checks that it's a JSON object
    whose `tested` is a non-empty list; which entries are usable is the tool's to decide.
    Raises ValueError for anything else."""
    try:
        data = json.loads(body)
    except ValueError as e:
        raise ValueError(f"it isn't JSON ({e})") from None
    if not isinstance(data, dict) or not isinstance(data.get("tested"), list) or not data["tested"]:
        raise ValueError('it isn\'t a JSON object with a non-empty "tested" list')
    return len(data["tested"])


def get(url, run=subprocess.run):
    """The body at `url`, through /usr/bin/curl, which uses the system's certificates."""
    arguments = ["/usr/bin/curl", "--silent", "--show-error", "--location", "--max-time", str(TIMEOUT),
                 "--max-filesize", str(MAX_SIZE), "--write-out", "\n%{http_code}", url]
    try:
        result = run(arguments, capture_output=True, text=True, timeout=TIMEOUT + 5)
    except (OSError, subprocess.SubprocessError, UnicodeDecodeError) as e:
        raise FetchError(f"curl didn't run ({e})") from None
    if result.returncode != 0:
        raise FetchError((result.stderr or "").strip() or f"curl's exit status was {result.returncode}")
    body, _, code = (result.stdout or "").rpartition("\n")
    if code.strip() not in ("200", "000", "0", ""):  # file:// URLs have no status
        raise FetchError(f"it answered with an error ({code.strip()})")
    return body


class TestedList:
    """Fetches the list once, on a thread of its own. `wait` gives what it came to.

    `app` is the running app bundle (None from source); `team` and `run` are replaceable in
    tests."""

    __test__ = False  # not a pytest test class, despite the name

    def __init__(self, app, environ=None, team=own_team, run=subprocess.run):
        environ = os.environ if environ is None else environ
        self._app = app
        self._url = environ.get(ENVIRONMENT) or None
        self._team, self._run = team, run
        self._lock = threading.Lock()
        self._thread = None
        self._done = threading.Event()
        self._result = None

    @property
    def origin(self):
        return self._url or GITHUB

    def start(self):
        """Starts fetching, once; later calls do nothing."""
        with self._lock:
            if self._thread is not None:
                return
            self._thread = threading.Thread(target=self._fetch, name="tested-list", daemon=True)
        self._thread.start()

    def wait(self):
        """What the fetch came to, waiting for it if it's still going. That can't be longer
        than its limit: past that, the read goes ahead on the tool's built-in list."""
        self.start()
        if not self._done.wait(TIMEOUT):
            return Fetched(self.origin, reason=f"{self.origin}'s took more than {TIMEOUT} seconds")
        return self._result

    def _fetch(self):
        try:
            result = self._fetched()
        except Exception as e:  # nothing here may stop anyone reading
            log.exception("Fetching the tested list failed")
            result = Fetched(self.origin, reason=f"couldn't get {self.origin}'s: {e}")
        if result.text is None:
            log.info("Tested list: built in (%s)", result.reason)
        else:
            log.info("Tested list: from %s (%d entries)", result.origin, result.entries)
        self._result = result
        self._done.set()

    def _fetched(self):
        if self._url is None and (self._app is None or self._team(self._app) is None):
            return Fetched(GITHUB, reason=DEVELOPMENT_COPY)
        try:
            body = get(self._url or URL, self._run)
            entries = parse(body)
        except (FetchError, ValueError) as e:
            return Fetched(self.origin, reason=f"couldn't get {self.origin}'s: {e}")
        return Fetched(self.origin, text=body, entries=entries)


def log_line(fetched, tool_report=None):
    """The log's one line saying which tested list counted. For a read it follows what the
    tool reports it used (`tool_report`, the "tested_list" `read --json` printed); with no
    read, what the app fetched (`fetched`, or None if nothing was fetched)."""
    origin = fetched.origin if fetched is not None else GITHUB
    why = f" ({fetched.reason})" if fetched is not None and fetched.reason else ""

    if isinstance(tool_report, dict) and tool_report.get("source"):
        if tool_report["source"] == "given":
            return f"Tested list: from {origin} ({tool_report.get('entries', 0)} entries)"
        if tool_report.get("reason"):
            return f"Tested list: built in ({origin}'s couldn't be used: {tool_report['reason']})"
        return f"Tested list: built in{why}"

    if fetched is not None and fetched.text is not None:
        return f"Tested list: from {origin} ({fetched.entries} entries)"
    return f"Tested list: built in{why}"
