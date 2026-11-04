"""The log Save Log writes."""

import re
from datetime import datetime, timedelta, timezone
from pathlib import Path
from urllib.parse import parse_qs, urlparse

from camera import summarize
from conftest import SECRETS
from report import TRACE_BEGIN, TRACE_END, build_log, file_name, issue_url

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
        "Result: Read the EOS R8", "Tested; read with canon-event-channel-0xd167",
        "Mechanical shutter", "1,001 to 2,000", "raw 1000000001000000d0070000f0550000",
        "What the tool said:", "trace written",
    ]:
        assert wanted in log, wanted
    assert log.rstrip().endswith(TRACE_END)
    assert log.index(TRACE_BEGIN) > log.index("Result:")


def test_nothing_in_the_log_identifies_the_camera(fixture):
    for name, trace in [("read-r8.json", "trace-r8.json"), ("read-90d.json", "trace-90d.json")]:
        log = log_for(fixture, name, trace=fixture(trace))
        for secret in SECRETS:
            assert secret not in log, (name, secret)
        assert "serial" not in log.lower()


def test_a_failed_read_says_why(fixture):
    log = log_for(fixture, "read-90d.json", trace=fixture("trace-90d.json"))
    assert "Result: Can't read the EOS 90D yet" in log
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
    source = (Path(__file__).resolve().parents[2] / "pkg" / "trace" / "trace.go").read_text(encoding="utf-8")
    assert f'LogBegin = "{TRACE_BEGIN}"' in source
    assert f'LogEnd   = "{TRACE_END}"' in source


def test_the_file_name():
    assert file_name(READ_AT) == "Open Shutter Count log 2026-11-04 15-39.txt"


def test_the_issue_link_fills_in_the_new_camera_template(fixture):
    url = issue_url("https://github.com/DigitalN/openshuttercounter", summarize(fixture("read-90d.json")))
    parts = urlparse(url)
    assert parts.path == "/DigitalN/openshuttercounter/issues/new"
    query = {k: v[0] for k, v in parse_qs(parts.query).items()}
    assert query["template"] == "new-camera.yml"
    assert query["camera"] == "EOS 90D"
    assert query["title"] == "EOS 90D: Can't read the EOS 90D yet"
    assert query["app_said"].startswith("Can't read the EOS 90D yet\n\nPlease save the log")
    assert len(url) < 4000


def test_the_issue_templates_fields_match_the_link():
    template = (Path(__file__).resolve().parents[2] / ".github" / "ISSUE_TEMPLATE" / "new-camera.yml").read_text()
    ids = set(re.findall(r"^\s+id: (\w+)$", template, re.MULTILINE))
    assert {"camera", "app_said"} <= ids
