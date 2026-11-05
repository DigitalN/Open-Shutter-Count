"""The log Save Log writes, and the issue Open an Issue fills in."""

import re
from datetime import datetime, timedelta, timezone
from pathlib import Path
from urllib.parse import parse_qs, quote, urlparse

from camera import summarize
from conftest import SECRETS
from report import CUT, MAX_URL, TRACE_BEGIN, TRACE_END, build_log, file_name, issue_url

MAC = {"model": "MacBookPro18,1", "macos": "27.2", "build": "27C55", "arch": "arm64"}
READ_AT = datetime(2026, 11, 4, 15, 39, 2, tzinfo=timezone(timedelta(hours=-7), "MST"))


def log_for(fixture, name, trace="", **kwargs):
    output = fixture(name)
    return build_log(app_name="Open Shutter Count", version="0.5.0",
                     repository_url="https://github.com/DigitalN/openshuttercounter", read_at=READ_AT, mac=MAC,
                     summary=summarize(output), output=output, trace=trace, **kwargs)


def test_the_log_holds_what_the_plan_lists(fixture):
    log = log_for(fixture, "read-r8.json", trace=fixture("trace-r8.json"), tool_messages="trace written")
    for wanted in [
        "Open Shutter Count 0.5.0", "https://github.com/DigitalN/openshuttercounter",
        "Read on 2026-11-04 at 15:39:02 MST (UTC-0700)",
        "Mac: MacBookPro18,1, macOS 27.2 (27C55), arm64",
        "Camera: Canon EOS R8", "Firmware: 3-1.5.0", 'USB: 04a9:330c "Canon Digital Camera"',
        "Commands it lists (9):", "0x9116",
        "GetDevicePropValue(0xd1ac): OperationNotSupported", "SetEventMode(0): OK",
        "Result: Success! We've read your shutter count.", "Tested; read with canon-event-channel-0xd167",
        "Mechanical shutter", "1,001 to 2,000", "raw 1000000001000000d0070000f0550000",
        "What the tool said:", "trace written",
    ]:
        assert wanted in log, wanted
    assert log.rstrip().endswith(TRACE_END)
    assert log.index(TRACE_BEGIN) > log.index("Result:")


def test_nothing_in_the_log_identifies_the_camera(fixture):
    for name, trace in [("read-r8.json", "trace-r8.json"), ("read-80d.json", "trace-80d.json"),
                        ("read-powershot.json", "trace-powershot.json")]:
        log = log_for(fixture, name, trace=fixture(trace))
        for secret in SECRETS:
            assert secret not in log, (name, secret)
        # The steps say whether the camera reported its serial number, and nothing more.
        assert "serial" not in log.lower().replace("0xd1af (serial number): reported", "")


def test_a_failed_read_says_why(fixture):
    log = log_for(fixture, "read-powershot.json", trace=fixture("trace-powershot.json"))
    assert "Result: Can't read the Canon PowerShot G7 X Mark III yet" in log
    assert "Kind: not_supported" in log
    assert "Message: this camera can't be read yet" in log


def test_no_camera_lists_the_usb_devices(fixture):
    log = log_for(fixture, "read-no-camera.json", usb_devices=[
        {"vendor_id": 0x05ac, "product_id": 0x8104, "name": "unrecognised device"}])
    assert "Camera: none found" in log
    assert "05ac:8104  unrecognised device" in log
    assert "none: nothing was sent to a camera" in log
    assert "Hint: Check that:" in log  # the tool's own words


def test_the_go_tool_finds_the_trace_with_the_same_markers():
    source = (Path(__file__).resolve().parents[2] / "tool" / "pkg" / "trace" / "trace.go").read_text(encoding="utf-8")
    assert f'LogBegin = "{TRACE_BEGIN}"' in source
    assert f'LogEnd   = "{TRACE_END}"' in source


def test_the_file_name():
    assert file_name(READ_AT) == "Open Shutter Count log 2026-11-04 15-39.txt"


REPOSITORY = "https://github.com/DigitalN/openshuttercounter"


def issue_fields(url):
    parts = urlparse(url)
    assert parts.path == "/DigitalN/openshuttercounter/issues/new"
    return {k: v[0] for k, v in parse_qs(parts.query).items()}


def test_the_issue_link_fills_in_the_new_camera_template(fixture):
    log = log_for(fixture, "read-powershot.json", trace=fixture("trace-powershot.json"))
    url = issue_url(REPOSITORY, summarize(fixture("read-powershot.json")), log)
    query = issue_fields(url)
    assert query["template"] == "new-camera.yml"
    assert query["camera"] == "Canon PowerShot G7 X Mark III"
    assert query["title"] == "Canon PowerShot G7 X Mark III: Can't read the Canon PowerShot G7 X Mark III yet"
    assert query["app_said"].startswith("Can't read the Canon PowerShot G7 X Mark III yet\n\nPlease open an issue")
    # The log, so there's nothing to attach: everything but the camera exchange.
    assert query["log"].startswith("Open Shutter Count 0.5.0\n")
    assert "Result: Can't read the Canon PowerShot G7 X Mark III yet" in query["log"]
    assert "Kind: not_supported" in query["log"]
    assert TRACE_BEGIN not in query["log"]
    assert query["log"].endswith("this camera can't be read yet: the PowerShot G7 X Mark III doesn't answer the "
                                 "standard shutter count property, and lists neither Canon's factory channel "
                                 "nor its event channel")


def test_an_issue_about_a_reading_says_the_numbers_look_wrong(fixture):
    log = log_for(fixture, "read-r6.json")
    query = issue_fields(issue_url(REPOSITORY, summarize(fixture("read-r6.json")), log))
    assert query["title"] == "Canon R6: the numbers look wrong"
    assert "Mechanical shutter" in query["log"]


def test_nothing_in_the_issue_identifies_the_camera(fixture):
    for name, trace in [("read-r8.json", "trace-r8.json"), ("read-80d.json", "trace-80d.json"),
                        ("read-powershot.json", "trace-powershot.json")]:
        url = issue_url(REPOSITORY, summarize(fixture(name)), log_for(fixture, name, trace=fixture(trace)))
        for secret in SECRETS:
            assert secret not in url and quote(secret) not in url, (name, secret)


def test_an_r8s_whole_log_fits_in_the_link(fixture):
    # A real R8 lists 152 commands; the fixture lists 9.
    output = fixture("read-r8.json")
    output["camera"]["operations"] = [f"0x{0x1001 + i:04x}" for i in range(152)]
    log = build_log(app_name="Open Shutter Count", version="0.5.0", repository_url=REPOSITORY, read_at=READ_AT,
                    mac=MAC, summary=summarize(output), output=output, trace=fixture("trace-r8.json"),
                    tool_messages="trace written")
    url = issue_url(REPOSITORY, summarize(output), log)
    assert len(url) <= MAX_URL
    assert CUT not in issue_fields(url)["log"]
    assert "0x1098" in issue_fields(url)["log"]


def test_a_log_too_long_for_the_link_is_cut_to_fit(fixture):
    log = log_for(fixture, "read-r8.json", tool_messages="\n".join(f"line {i} " + "x" * 60 for i in range(200)))
    url = issue_url(REPOSITORY, summarize(fixture("read-r8.json")), log)
    assert len(url) <= MAX_URL
    text = issue_fields(url)["log"]
    assert text.startswith("Open Shutter Count 0.5.0\n")
    assert text.endswith("\n" + CUT)
    assert "Result: Success! We've read your shutter count." in text


def test_the_issue_templates_fields_match_the_link():
    template = (Path(__file__).resolve().parents[2] / ".github" / "ISSUE_TEMPLATE" / "new-camera.yml").read_text()
    ids = set(re.findall(r"^\s+id: (\w+)$", template, re.MULTILINE))
    assert ids == {"camera", "app_said", "log"}
    assert "type: upload" not in template  # the log is filled in, not attached
