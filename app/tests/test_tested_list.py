"""The list of tested cameras, fetched at launch: what it fetches, what it falls back to,
how a read passes it on, and the log's line saying which list counted."""

import json
import threading
import time
from pathlib import Path
from types import SimpleNamespace
from urllib.parse import parse_qs, urlparse

import pytest

import camera
import launch
import report
import tested_list
from tested_list import ENVIRONMENT, Fetched, TestedList, log_line
from test_wizard import Clock, FakeCamera, FakeUpdater, Inline
from updater import Outcome

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
TESTED_JSON = (REPOSITORY_ROOT / tested_list.PATH).read_text(encoding="utf-8")
RELEASE = Path("/Applications/Open Shutter Count.app")


def signed(app):
    return "XT4LT7MS6Y"


def ad_hoc(app):
    return None


needs_curl = pytest.mark.skipif(not Path("/usr/bin/curl").exists(), reason="needs /usr/bin/curl")


def stand_in(tmp_path, text, name="tested.json"):
    path = tmp_path / name
    path.write_text(text, encoding="utf-8")
    return path.as_uri()


def fetch_from(url, app=RELEASE, team=signed, run=None):
    kwargs = {"run": run} if run else {}
    return TestedList(app, environ={ENVIRONMENT: url}, team=team, **kwargs).wait()


def curl(stdout="", stderr="", returncode=0, calls=None, raises=None):
    """A stand-in for subprocess.run running /usr/bin/curl."""
    def run(arguments, **kwargs):
        if calls is not None:
            calls.append(arguments)
        if raises:
            raise raises
        return SimpleNamespace(stdout=stdout, stderr=stderr, returncode=returncode)
    return run


# ── the fetch ──

@needs_curl
def test_a_good_list(tmp_path):
    url = stand_in(tmp_path, TESTED_JSON)
    fetched = fetch_from(url)
    assert fetched == Fetched(url, text=TESTED_JSON, entries=TESTED_JSON.count('"model"'))


@needs_curl
@pytest.mark.parametrize("text, reason", [
    ("{", "it isn't JSON (Expecting property name enclosed in double quotes: line 1 column 2 (char 1))"),
    ("", "it isn't JSON (Expecting value: line 1 column 1 (char 0))"),
    ('{"tested": []}', 'it isn\'t a JSON object with a non-empty "tested" list'),
    ('{"tested": "EOS R8"}', 'it isn\'t a JSON object with a non-empty "tested" list'),
    ('{"cameras": [{"model": "EOS R8"}]}', 'it isn\'t a JSON object with a non-empty "tested" list'),
    ('[{"model": "EOS R8"}]', 'it isn\'t a JSON object with a non-empty "tested" list'),
])
def test_a_file_that_isnt_a_list_falls_back(tmp_path, text, reason):
    url = stand_in(tmp_path, text)
    assert fetch_from(url) == Fetched(url, reason=f"couldn't get {url}'s: {reason}")


@needs_curl
def test_a_missing_file_falls_back(tmp_path):
    url = (tmp_path / "nowhere.json").as_uri()
    fetched = fetch_from(url)
    assert fetched.text is None
    assert fetched.reason.startswith(f"couldn't get {url}'s: curl: (37)")


@pytest.mark.parametrize("run, reason", [
    (curl(stderr="curl: (6) Could not resolve host: raw.githubusercontent.com", returncode=6),
     "curl: (6) Could not resolve host: raw.githubusercontent.com"),
    (curl(stderr="curl: (28) Operation timed out after 15001 milliseconds", returncode=28),
     "curl: (28) Operation timed out after 15001 milliseconds"),
    (curl(returncode=7), "curl's exit status was 7"),
    (curl(raises=FileNotFoundError("no /usr/bin/curl")), "curl didn't run (no /usr/bin/curl)"),
    (curl(stdout="404: Not Found\n404"), "it answered with an error (404)"),
    (curl(stdout="<html>rate limited</html>\n429"), "it answered with an error (429)"),
])
def test_a_curl_failure_falls_back(run, reason):
    fetched = TestedList(RELEASE, environ={}, team=signed, run=run).wait()
    assert fetched == Fetched("GitHub", reason=f"couldn't get GitHub's: {reason}")


def test_anything_else_going_wrong_falls_back():
    def broken(app):
        raise RuntimeError("codesign broke")

    fetched = TestedList(RELEASE, environ={}, team=broken).wait()
    assert fetched == Fetched("GitHub", reason="couldn't get GitHub's: codesign broke")


def test_a_release_copy_fetches_from_github_and_not_its_api():
    calls = []
    run = curl(stdout=TESTED_JSON + "\n200", calls=calls)
    fetched = TestedList(RELEASE, environ={}, team=signed, run=run).wait()

    assert fetched.text == TESTED_JSON and fetched.origin == "GitHub"
    assert calls[0][0] == "/usr/bin/curl"
    assert calls[0][-1] == tested_list.URL == (
        f"https://raw.githubusercontent.com/{launch.REPOSITORY}/main/tool/pkg/manufacturers/canon/tested.json")
    assert "api.github.com" not in tested_list.URL
    assert calls[0][calls[0].index("--max-time") + 1] == "15", "the update check's limit"


def test_it_fetches_once_per_launch():
    calls = []
    listed = TestedList(RELEASE, environ={}, team=signed, run=curl(stdout=TESTED_JSON + "\n200", calls=calls))
    listed.start()
    listed.start()
    first, again = listed.wait(), listed.wait()
    assert first is again
    assert len(calls) == 1


# ── development copies ──

@pytest.mark.parametrize("app, team", [(None, signed), (RELEASE, ad_hoc)], ids=["from source", "signed ad hoc"])
def test_development_copies_skip_the_fetch(app, team):
    calls = []
    fetched = TestedList(app, environ={}, team=team, run=curl(calls=calls)).wait()
    assert fetched == Fetched("GitHub", reason="development copy")
    assert calls == []


@needs_curl
@pytest.mark.parametrize("app, team", [(None, signed), (RELEASE, ad_hoc), (RELEASE, signed)],
                         ids=["from source", "signed ad hoc", "a release"])
def test_the_environment_variable_makes_any_copy_fetch_from_there(tmp_path, app, team):
    url = stand_in(tmp_path, TESTED_JSON, name="stand-in.json")
    fetched = fetch_from(url, app=app, team=team)
    assert fetched.origin == url and fetched.text == TESTED_JSON


def test_the_environment_variable_replaces_the_url():
    calls = []
    fetch_from("https://example.test/list.json", run=curl(stdout=TESTED_JSON + "\n200", calls=calls))
    assert calls[0][-1] == "https://example.test/list.json"


# ── a read ──

class Ready:
    """A tested list that's already been fetched."""

    def __init__(self, fetched):
        self.fetched = fetched

    def start(self):
        pass

    def wait(self):
        return self.fetched


def tool(fixture, calls, seen=None):
    """A stand-in for the tool: records each call and what --tested-list held."""
    def run(arguments, **kwargs):
        calls.append(arguments)
        if seen is not None and "--tested-list" in arguments:
            path = Path(arguments[arguments.index("--tested-list") + 1])
            seen.append((path, path.read_text(encoding="utf-8")))
        return SimpleNamespace(stdout=json.dumps(fixture("read-r8.json")), stderr="", returncode=0)
    return run


def test_a_fetched_list_is_passed_and_gone_after_the_read(fixture):
    calls, seen = [], []
    cam = camera.Camera("t", run=tool(fixture, calls, seen), note="Open Shutter Count 0.5.5",
                        tested_list=Ready(Fetched("GitHub", text=TESTED_JSON, entries=8)))
    cam.read()

    assert "--tested-list" in calls[0]
    path, text = seen[0]
    assert text == TESTED_JSON
    assert path.parent == Path(calls[0][calls[0].index("--trace") + 1]).parent, "beside the trace"
    assert not path.exists() and not path.parent.exists(), "deleted with the read's folder"
    assert calls[0][-2:] == ["--note", "Open Shutter Count 0.5.5"]


@pytest.mark.parametrize("listed", [
    None,
    Ready(Fetched("GitHub", reason="development copy")),
    Ready(Fetched("GitHub", reason="couldn't get GitHub's: curl: (6) Could not resolve host")),
], ids=["no list", "a development copy", "a failed fetch"])
def test_no_list_is_passed_when_none_was_fetched(fixture, listed):
    calls = []
    camera.Camera("t", run=tool(fixture, calls), tested_list=listed).read()
    assert "--tested-list" not in calls[0]


def blocking_curl(started, release, calls=None):
    def run(arguments, **kwargs):
        if calls is not None:
            calls.append(arguments)
        started.set()
        release.wait(10)
        return SimpleNamespace(stdout=TESTED_JSON + "\n200", stderr="", returncode=0)
    return run


def test_a_read_waits_for_a_fetch_thats_still_going(fixture):
    started, release = threading.Event(), threading.Event()
    listed = TestedList(RELEASE, environ={}, team=signed, run=blocking_curl(started, release))
    listed.start()
    assert started.wait(5)

    calls = []
    cam = camera.Camera("t", run=tool(fixture, calls), tested_list=listed)
    reading = threading.Thread(target=cam.read)
    reading.start()
    time.sleep(0.3)
    assert calls == [] and reading.is_alive(), "the tool isn't run until the list is in"

    release.set()
    reading.join(5)
    assert not reading.is_alive()
    assert "--tested-list" in calls[0]


def test_read_again_reuses_the_list(fixture):
    fetches, calls = [], []
    listed = TestedList(RELEASE, environ={}, team=signed, run=curl(stdout=TESTED_JSON + "\n200", calls=fetches))
    cam = camera.Camera("t", run=tool(fixture, calls), tested_list=listed)
    cam.read()
    cam.read()
    assert len(fetches) == 1
    assert all("--tested-list" in c for c in calls)


def make(fixture, listed, outcome=Outcome("current"), **camera_kwargs):
    clock = Clock()
    w = Inline(FakeUpdater(outcome), FakeCamera(fixture, **camera_kwargs), app_name="Open Shutter Count",
               version="0.5.5", repository_url="https://github.com/DigitalN/Open-Shutter-Count",
               mac_info=lambda: {"model": "MacBookPro18,1", "macos": "27.2"}, clock=clock, sleep=clock.sleep,
               tested_list=listed)
    return w


def test_the_update_step_doesnt_wait_for_the_list(fixture):
    started, release = threading.Event(), threading.Event()
    listed = TestedList(RELEASE, environ={}, team=signed, run=blocking_curl(started, release))
    w = make(fixture, listed)
    w.start()
    assert started.wait(5), "it's fetched alongside the update check"
    assert w.state()["step"] == "setup", "the update check is done, and the list isn't in yet"
    release.set()
    assert listed.wait().text == TESTED_JSON


def test_the_list_isnt_shown_on_screen(fixture):
    w = make(fixture, Ready(Fetched("GitHub", text=TESTED_JSON, entries=8)))
    w.start()
    w.setup_done()
    assert "ested list" not in str(w.state())


# ── the log's line ──

GIVEN = {"source": "given", "entries": 8}
BUILT_IN = {"source": "built in", "entries": 8}
FROM_GITHUB = Fetched("GitHub", text=TESTED_JSON, entries=8)
OFFLINE = Fetched("GitHub", reason="couldn't get GitHub's: curl: (6) Could not resolve host: raw.githubusercontent.com")
DEVELOPMENT = Fetched("GitHub", reason="development copy")
STAND_IN = Fetched("file:///tmp/stand-in.json", text=TESTED_JSON, entries=3)


@pytest.mark.parametrize("fetched, tool_report, line", [
    # A read: what the tool used.
    (FROM_GITHUB, GIVEN, "Tested list: from GitHub (8 entries)"),
    (FROM_GITHUB, {"source": "given", "entries": 7}, "Tested list: from GitHub (7 entries)"),
    (FROM_GITHUB, {**BUILT_IN, "reason": "it has no usable entry"},
     "Tested list: built in (GitHub's couldn't be used: it has no usable entry)"),
    (OFFLINE, BUILT_IN,
     "Tested list: built in (couldn't get GitHub's: curl: (6) Could not resolve host: raw.githubusercontent.com)"),
    (DEVELOPMENT, BUILT_IN, "Tested list: built in (development copy)"),
    (STAND_IN, {"source": "given", "entries": 3}, "Tested list: from file:///tmp/stand-in.json (3 entries)"),
    # No read: what the app fetched.
    (FROM_GITHUB, None, "Tested list: from GitHub (8 entries)"),
    (OFFLINE, None,
     "Tested list: built in (couldn't get GitHub's: curl: (6) Could not resolve host: raw.githubusercontent.com)"),
    (DEVELOPMENT, None, "Tested list: built in (development copy)"),
    (STAND_IN, None, "Tested list: from file:///tmp/stand-in.json (3 entries)"),
    (None, None, "Tested list: built in"),
])
def test_the_log_line(fetched, tool_report, line):
    assert log_line(fetched, tool_report) == line


class Printing(FakeCamera):
    """Reads what it's given, with the tool's "tested_list" set to `listed`."""

    def __init__(self, fixture, listed, **kwargs):
        super().__init__(fixture, **kwargs)
        self.listed = listed

    def read(self):
        output, messages, trace = super().read()
        output["tested_list"] = self.listed
        return output, messages, trace


def issue_log(w):
    url = report.issue_url("https://github.com/DigitalN/Open-Shutter-Count", w.state()["result"], w.log()[1])
    return parse_qs(urlparse(url).query)["log"][0]


@pytest.mark.parametrize("fetched, tool_report, line", [
    (FROM_GITHUB, GIVEN, "Tested list: from GitHub (8 entries)"),
    (OFFLINE, BUILT_IN, "Tested list: built in (couldn't get GitHub's: "),
    (DEVELOPMENT, BUILT_IN, "Tested list: built in (development copy)"),
    (STAND_IN, {"source": "given", "entries": 3}, "Tested list: from file:///tmp/stand-in.json (3 entries)"),
])
def test_a_reads_log_says_which_list_the_tool_used(fixture, fetched, tool_report, line):
    w = make(fixture, Ready(fetched))
    w._camera = Printing(fixture, tool_report)
    w.start()
    w.setup_done()
    assert w.state()["result"]["ok"]
    assert line in w.log()[1]
    assert line in issue_log(w), "it's in the part Open an Issue fills in"


@pytest.mark.parametrize("fetched, line", [
    (FROM_GITHUB, "Tested list: from GitHub (8 entries)"),
    (OFFLINE, "Tested list: built in (couldn't get GitHub's: "),
    (DEVELOPMENT, "Tested list: built in (development copy)"),
    (STAND_IN, "Tested list: from file:///tmp/stand-in.json (3 entries)"),
])
def test_no_camera_found_says_what_the_app_fetched(fixture, fetched, line):
    w = make(fixture, Ready(fetched), empty_polls=None)
    w.start()
    w.setup_done()
    assert w.state()["result"]["kind"] == camera.NO_CAMERA
    assert w.state()["can_save_log"]
    assert line in w.log()[1]
    assert line in issue_log(w)


def test_a_read_with_the_real_fixture_says_built_in(fixture):
    w = make(fixture, Ready(DEVELOPMENT))
    w.start()
    w.setup_done()
    assert "Tested list: built in (development copy)" in w.log()[1]


def test_the_line_is_near_the_top(fixture):
    w = make(fixture, Ready(FROM_GITHUB))
    w.start()
    w.setup_done()
    lines = w.log()[1].splitlines()
    position = next(i for i, line in enumerate(lines) if line.startswith("Tested list: "))
    assert position < next(i for i, line in enumerate(lines) if line.startswith("Camera: "))
