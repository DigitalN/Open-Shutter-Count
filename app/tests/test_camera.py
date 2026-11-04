"""Running the tool, and what the result screen makes of what it says."""

import json
import subprocess
from types import SimpleNamespace

import pytest

import camera
from camera import Camera, ToolError, count_text, parse_read, summarize


def fake_run(stdout="", stderr="", returncode=0, calls=None, raises=None):
    def run(arguments, **kwargs):
        if calls is not None:
            calls.append(arguments)
        if raises:
            raise raises
        return SimpleNamespace(stdout=stdout, stderr=stderr, returncode=returncode)
    return run


def test_list_runs_the_tool_and_sends_nothing_else(fixture):
    calls = []
    devices = Camera("/x/openshuttercounter", run=fake_run(json.dumps(fixture("list-r8.json")), calls=calls)).list()
    assert calls == [["/x/openshuttercounter", "list", "--json"]]
    assert devices[0]["name"] == "Canon EOS R8"
    assert "serial" not in devices[0]


def test_an_empty_list_is_no_camera():
    assert Camera("t", run=fake_run("null\n")).list() == []
    assert Camera("t", run=fake_run("[]\n")).list() == []


def test_a_tool_that_fails_to_list_says_so():
    with pytest.raises(ToolError, match="libusb"):
        Camera("t", run=fake_run("libusb-1.0 could not be loaded", returncode=1)).list()
    with pytest.raises(ToolError, match="didn't run"):
        Camera("t", run=fake_run(raises=FileNotFoundError("no such file"))).list()


def test_read_passes_a_trace_and_gives_it_back(fixture):
    calls = []

    def run(arguments, **kwargs):
        calls.append(arguments)
        trace = arguments[arguments.index("--trace") + 1]
        with open(trace, "w", encoding="utf-8") as f:
            f.write(fixture("trace-r8.json"))
        return SimpleNamespace(stdout=json.dumps(fixture("read-r8.json")), stderr="", returncode=0)

    output, messages, trace = Camera("t", run=run, note="Open Shutter Count 0.5.0").read()
    assert calls[0][:4] == ["t", "read", "--json", "--raw"]
    assert calls[0][-2:] == ["--note", "Open Shutter Count 0.5.0"]
    assert output["reading"]["model"] == "EOS R8"
    assert '"entries"' in trace


def test_a_read_that_hangs_is_a_camera_that_stopped_answering():
    output, _, _ = Camera("t", run=fake_run(raises=subprocess.TimeoutExpired("t", 120))).read()
    assert output["error"]["kind"] == camera.NO_ANSWER


def test_what_the_tool_printed_when_it_failed(fixture):
    busy = parse_read(json.dumps(fixture("read-busy.json")), "", 1)
    assert busy["error"]["kind"] == camera.BUSY
    assert parse_read("panic: oops", "", 2)["error"] == {"kind": camera.FAILED, "message": "panic: oops"}
    assert parse_read("", "", 1)["error"]["message"] == "exit status 1"


def test_counts():
    assert count_text({"value": 2000, "min": 1001, "range": True}) == "1,001 to 2,000"
    assert count_text({"value": 999, "range": True}) == "0 to 999"
    assert count_text({"value": 26374}) == "26,374"
    assert count_text({"value": 0, "range": True}) == "0"
    assert count_text({"value": 1, "error": "MPU timeout"}) == "couldn't be read"


def test_an_r8_reads_as_tested(fixture):
    s = summarize(fixture("read-r8.json"))
    assert s["ok"] and s["tested"]
    assert s["headline"] == "Read the EOS R8"
    assert s["detail"] == "Tested on the EOS R8."
    assert [(c["label"], c["text"]) for c in s["counters"]] == [
        ("Mechanical shutter", "1,001 to 2,000"), ("Electronic shutter", "19,001 to 20,999"),
        ("All photos", "21,001 to 22,000")]
    assert s["note"].startswith("The camera keeps these counts in thousands")


def test_another_rf_body_says_its_not_fully_tested(fixture):
    s = summarize(fixture("read-r6.json"))
    assert s["ok"] and not s["tested"]
    assert s["detail"] == "Not fully tested on the EOS R6; if these numbers look wrong, please send the log."


def test_a_camera_that_cant_be_read_yet(fixture):
    s = summarize(fixture("read-90d.json"))
    assert not s["ok"]
    assert s["headline"] == "Can't read the EOS 90D yet"
    assert "save the log" in s["detail"]


def test_another_app_holding_the_camera_shows_the_tools_hint(fixture):
    s = summarize(fixture("read-busy.json"))
    assert s["headline"] == "Another app is using the camera"
    assert s["hint"].startswith("EOS Utility has the camera open.")
    assert "\n" not in s["hint"], "the terminal's line breaks are taken out"


def test_the_wrong_usb_mode_names_the_setting(fixture):
    s = summarize(fixture("read-wrong-usb-mode.json"))
    assert s["kind"] == camera.WRONG_USB_MODE
    assert "Choose USB connection app > Photo Import/Remote Control" in s["detail"]
    assert "page 4" in s["detail"]


def test_no_camera_and_no_answer(fixture):
    s = summarize(fixture("read-no-camera.json"))
    assert s["headline"] == "No camera found"
    assert "another make" in s["detail"]
    assert s["hint"].startswith("- Switch the camera on")
    assert "docs/" not in s["hint"]
    assert summarize(fixture("read-no-answer.json"))["headline"] == "The camera stopped answering"


def test_unwrap():
    assert camera.unwrap("Quit it completely: it may\nstill be running.\n\nThen try again.") == (
        "Quit it completely: it may still be running.\n\nThen try again.")
    assert camera.unwrap("Check that:\n  - one\n    more\n  - two") == "Check that:\n- one more\n- two"


def test_the_camera_is_named_from_the_device_list_before_its_read(fixture):
    assert camera.camera_name({}, fixture("list-r8.json")) == "EOS R8"
    assert camera.camera_name({}, [{"name": "Canon camera"}]) == ""
