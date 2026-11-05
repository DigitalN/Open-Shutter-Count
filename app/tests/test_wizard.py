"""The wizard, end to end, with a stand-in updater and camera. Its threads run inline."""

import json

import camera
import updater
import wizard as wizard_module
from conftest import SECRETS
from updater import Outcome


class Inline(wizard_module.Wizard):
    def _spawn(self, target, name):
        target()


class FakeUpdater:
    def __init__(self, outcome):
        self.outcome = outcome

    def check(self):
        return self.outcome


class FakeCamera:
    """Lists nothing `empty_polls` times, then the R8; reads what it's given."""

    def __init__(self, fixture, empty_polls=0, output="read-r8.json", trace="trace-r8.json", clock=None):
        self._fixture, self._empty, self._output, self._trace = fixture, empty_polls, output, trace
        self.reads = 0
        self.clock = clock

    def list(self, everything=False):
        if everything:
            return [{"vendor_id": 0x05ac, "product_id": 0x8104, "name": "unrecognised device"}]
        if self._empty is None or self._empty > 0:
            if self._empty:
                self._empty -= 1
            return []
        return self._fixture("list-r8.json")

    def read(self):
        self.reads += 1
        return self._fixture(self._output), "", self._fixture(self._trace)


class Clock:
    def __init__(self):
        self.t = 0.0

    def __call__(self):
        return self.t

    def sleep(self, seconds):
        self.t += seconds


def make(fixture, outcome=Outcome(updater.CURRENT), **camera_kwargs):
    clock = Clock()
    cam = FakeCamera(fixture, **camera_kwargs)
    w = Inline(FakeUpdater(outcome), cam, app_name="Open Shutter Count", version="0.5.0",
               repository_url="https://github.com/DigitalN/openshuttercounter",
               mac_info=lambda: {"model": "MacBookPro18,1", "macos": "27.2"}, clock=clock, sleep=clock.sleep)
    return w, cam, clock


def test_it_starts_by_checking_for_updates(fixture):
    w, _, _ = make(fixture)
    assert w.state()["step"] == "update"
    assert w.state()["update"]["state"] == "checking"
    w.start()
    assert w.state()["step"] == "setup"


def test_nothing_happens_to_the_camera_before_the_checklist(fixture):
    w, cam, _ = make(fixture)
    w.start()
    w.read_again()  # only from the result screen
    assert w.state()["step"] == "setup" and cam.reads == 0


def test_a_whole_read(fixture):
    w, cam, clock = make(fixture, empty_polls=2)
    w.start()
    w.setup_done()
    s = w.state()
    # Two looks a second apart, then a moment for the camera to start up.
    assert clock.t == 2 * wizard_module.POLL_INTERVAL + wizard_module.SETTLE
    assert s["step"] == "result"
    assert s["result"]["ok"] and s["result"]["tested"]
    assert s["can_save_log"]
    assert cam.reads == 1

    name, text = w.log()
    assert name.startswith("Open Shutter Count log ")
    assert "Result: Success! We've read your shutter count." in text
    for secret in SECRETS:
        assert secret not in text


def test_read_again_goes_back_to_waiting(fixture):
    w, cam, clock = make(fixture)
    w.start()
    w.setup_done()
    w.read_again()
    assert w.state()["step"] == "result"
    assert cam.reads == 2
    assert clock.t == 0, "a camera already plugged in is read straight away"


def test_waiting_offers_help_then_gives_up_on_the_result_screen(fixture):
    w, cam, clock = make(fixture, empty_polls=None)
    w.start()

    states = []
    real_sleep = w._sleep

    def watch(seconds):
        states.append(w.state())
        real_sleep(seconds)

    w._sleep = watch
    w.setup_done()

    assert not states[0]["waiting"]["help"]
    assert any(s["waiting"]["help"] for s in states)
    assert clock.t >= wizard_module.GIVE_UP_AFTER
    s = w.state()
    assert s["step"] == "result"
    assert s["result"]["kind"] == camera.NO_CAMERA
    assert "05ac:8104" in w.log()[1]
    assert cam.reads == 0


def test_stop_looking_ends_on_the_result_screen(fixture):
    w, _, _ = make(fixture)
    w.start()
    with w._lock:
        w._step = wizard_module.WAITING
    w.stop_looking()
    assert w.state()["result"]["headline"] == "No camera found"


def test_a_failed_update_check_doesnt_stop_anyone_reading(fixture):
    w, _, _ = make(fixture, outcome=Outcome(updater.FAILED, reason="Couldn't reach GitHub (Could not resolve host)."))
    w.start()
    s = w.state()
    assert s["step"] == "setup"
    assert s["update_note"] == ("Checking for updates didn't work (Couldn't reach GitHub (Could not resolve host)). "
                                "You're reading with version 0.5.0.")
    w.setup_done()
    assert "Update: Checking for updates didn't work" in w.log()[1]


def test_a_failed_update_says_which_version(fixture):
    w, _, _ = make(fixture, outcome=Outcome(updater.FAILED, "0.5.1", "The downloaded app was changed."))
    w.start()
    assert w.state()["update_note"].startswith("Updating to 0.5.1 didn't work (The downloaded app was changed)")


def test_a_newer_version_this_copy_cant_install_blocks_reading(fixture):
    w, cam, _ = make(fixture, outcome=Outcome(updater.BLOCKED, "0.5.1", "it isn't in your Applications folder"))
    w.start()
    w.setup_done()
    s = w.state()
    assert s["step"] == "update"
    assert s["update"] == {"state": "blocked", "version": "0.5.1", "reason": "it isn't in your Applications folder"}
    assert cam.reads == 0


def test_an_installed_update_stays_put_while_it_reopens(fixture):
    w, _, _ = make(fixture, outcome=Outcome(updater.INSTALLED, "0.5.1"))
    w.on_update_status("downloading", "0.5.1")
    assert w.state()["update"]["state"] == "downloading"
    w.start()
    assert w.state()["step"] == "update"


def test_an_updater_that_breaks_is_a_failed_check(fixture):
    class Broken:
        def check(self):
            raise RuntimeError("boom")

    w, _, _ = make(fixture)
    w._updater = Broken()
    w.start()
    assert w.state()["step"] == "setup"
    assert "boom" in w.state()["update_note"]


def test_a_tool_that_wont_list_ends_on_the_result_screen(fixture):
    class NoTool(FakeCamera):
        def list(self, everything=False):
            raise camera.ToolError("libusb-1.0 could not be loaded")

    w, _, _ = make(fixture)
    w._camera = NoTool(fixture)
    w.start()
    w.setup_done()
    r = w.state()["result"]
    assert r["headline"] == "Couldn't read the camera"
    assert "libusb" in r["message"]


def test_the_state_is_plain_data(fixture):
    w, _, _ = make(fixture)
    w.start()
    w.setup_done()
    json.dumps(w.state())  # the page gets it through pywebview
