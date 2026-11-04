"""The updater's decisions: versions, GitHub's release format, signing, and when to update."""

import os

import pytest

from launch import in_applications_folder
from updater import (BLOCKED, CURRENT, UpdateError, decide, is_newer, parse_release, signing_requirement,
                     team_identifier, why_cant_update)


def test_versions_compare_number_by_number():
    assert is_newer("0.5.10", "0.5.9")
    assert is_newer("0.5.1", "0.5.0")
    assert is_newer("v0.6.0", "0.5.99")
    assert not is_newer("0.5.0", "0.5")  # same version, different spelling
    assert not is_newer("0.5.0", "0.5.1")


def test_release_gives_version_and_disk_image():
    release = {"tag_name": "v0.5.1", "draft": False, "assets": [
        {"name": "notes.txt", "browser_download_url": "https://example.invalid/notes.txt"},
        {"name": "Open-Shutter-Count-0.5.1.dmg", "browser_download_url": "https://example.invalid/Open-Shutter-Count-0.5.1.dmg"},
    ]}
    assert parse_release(release) == ("0.5.1", "https://example.invalid/Open-Shutter-Count-0.5.1.dmg")


def test_release_without_disk_image_or_tag_is_refused():
    with pytest.raises(UpdateError, match="no disk image"):
        parse_release({"tag_name": "v0.5.1", "assets": [{"name": "x.zip", "browser_download_url": "x"}]})
    with pytest.raises(UpdateError):
        parse_release({"message": "Not Found"})


def test_team_comes_from_codesign_details():
    details = ("Executable=/Applications/Open Shutter Count.app/Contents/MacOS/Open Shutter Count\n"
               "Identifier=com.digitaln.openshuttercount\nTeamIdentifier=XT4LT7MS6Y\n")
    assert team_identifier(details) == "XT4LT7MS6Y"
    assert team_identifier("Identifier=com.digitaln.openshuttercount\nTeamIdentifier=not set\n") is None
    assert team_identifier("code object is not signed at all") is None


def test_signing_requirement_pins_identifier_and_team():
    requirement = signing_requirement("com.digitaln.openshuttercount", "XT4LT7MS6Y")
    assert requirement.startswith('=identifier "com.digitaln.openshuttercount" and anchor apple generic')
    assert requirement.endswith('certificate leaf[subject.OU] = "XT4LT7MS6Y"')


def test_a_newer_release_is_always_installed():
    assert decide("0.5.0", ("0.5.1", "url"), None) == "update"


def test_a_copy_that_cant_update_itself_is_blocked_only_when_theres_something_newer():
    reason = "Open Shutter Count isn't in your Applications folder"
    assert decide("0.5.0", ("0.5.1", "url"), reason) == BLOCKED
    assert decide("0.5.1", ("0.5.1", "url"), reason) == CURRENT
    assert decide("0.5.0", None, reason) == CURRENT  # no releases yet


def test_why_a_copy_cant_update(tmp_path):
    assert "Applications folder" in why_cant_update(tmp_path / "Open Shutter Count.app")
    app = "/Applications/Open Shutter Count.app"
    assert why_cant_update(app, writable=lambda path, mode: True) is None
    assert "can't replace itself in /Applications" in why_cant_update(app, writable=lambda path, mode: False)
    assert os.W_OK  # the mode it asks about


def test_applications_folder(tmp_path):
    assert in_applications_folder("/Applications/Open Shutter Count.app")
    assert not in_applications_folder("/Applications Old/Open Shutter Count.app")
    assert not in_applications_folder(tmp_path / "Open Shutter Count.app")
    assert not in_applications_folder("/private/var/folders/xy/T/AppTranslocation/1234/d/Open Shutter Count.app")
