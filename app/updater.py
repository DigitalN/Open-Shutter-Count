"""Keeps Open Shutter Count up to date from its GitHub releases, before anything else.

Adapted from Mac Engine Room's updater.py (MIT; see THIRD-PARTY-NOTICES). What's
different: it checks at every launch, before the camera can be read; a newer release is
installed straight away and the app reopens into it; and a copy that can't update itself
blocks reading instead of carrying on. Nothing is remembered between launches.

A release counts when it's the repository's latest (not a draft or pre-release), its tag
is the version (`v0.5.0`), and the `.dmg` from `app/scripts/package.sh` is attached.

This is the app's only network access. It goes through /usr/bin/curl, which uses the
system's certificates (the bundled Python has none of its own).

An app downloaded this way isn't marked as coming from the internet, so macOS doesn't ask
to approve it again. The signature check is what makes that safe: only an app signed with
a certificate from the same team, for the same bundle identifier, can replace this one.
"""

import json
import logging
import os
import plistlib
import re
import shutil
import subprocess
import time
from pathlib import Path

from launch import APP_NAME, REPOSITORY, in_applications_folder, relaunch

log = logging.getLogger("openshuttercount")

FEED_URL = f"https://api.github.com/repos/{REPOSITORY}/releases/latest"
CHECK_TIMEOUT = 15  # seconds; the window says it's checking for this long at most
DOWNLOAD_TIMEOUT = 600

# What a check came to. The wizard shows each.
SKIPPED = "skipped"  # a development copy: from source, or signed ad hoc
CURRENT = "current"  # nothing newer
FAILED = "failed"  # the check or the update didn't work; reading goes ahead
BLOCKED = "blocked"  # there's a newer release and this copy can't install it
INSTALLED = "installed"  # the new version is in place and reopening


class UpdateError(Exception):
    pass


class Outcome:
    def __init__(self, state, version=None, reason=None):
        self.state, self.version, self.reason = state, version, reason

    def __repr__(self):
        return f"Outcome({self.state!r}, {self.version!r}, {self.reason!r})"

    def __eq__(self, other):
        return isinstance(other, Outcome) and (self.state, self.version, self.reason) == (
            other.state, other.version, other.reason)

    def to_dict(self):
        return {"state": self.state, "version": self.version, "reason": self.reason}


# ── pure helpers (unit-tested) ──────────────────────────────

def version_key(version):
    """(0, 5, 10) for '0.5.10' or 'v0.5.10', so 0.5.10 sorts after 0.5.9 and 0.5 equals 0.5.0."""
    parts = [int(m.group()) if (m := re.match(r"\d+", p)) else 0 for p in str(version).lstrip("v").split(".")]
    while len(parts) > 1 and parts[-1] == 0:
        parts.pop()
    return tuple(parts)


def is_newer(version, other):
    return version_key(version) > version_key(other)


def parse_release(data):
    """(version, disk image URL) from GitHub's latest-release response."""
    if not isinstance(data, dict) or not isinstance(data.get("tag_name"), str):
        raise UpdateError("GitHub's answer didn't describe a release.")
    tag = data["tag_name"]
    version = tag[1:] if tag.startswith("v") else tag
    for asset in data.get("assets") or []:
        if str(asset.get("name", "")).lower().endswith(".dmg") and asset.get("browser_download_url"):
            return version, asset["browser_download_url"]
    raise UpdateError("The latest release has no disk image attached.")


def team_identifier(codesign_details):
    """The team from `codesign -d --verbose=2` output, or None if it isn't team-signed."""
    match = re.search(r"^TeamIdentifier=(\S+)$", codesign_details, re.MULTILINE)
    return match.group(1) if match and match.group(1) != "not" else None


def signing_requirement(bundle_id, team):
    return (f'=identifier "{bundle_id}" and anchor apple generic'
            f' and certificate leaf[subject.OU] = "{team}"')


def decide(current, release, cant_update_reason):
    """What to do about the latest release: CURRENT, BLOCKED or "update".

    A newer release is never optional. If this copy can't install it, reading is blocked."""
    if release is None or not is_newer(release[0], current):
        return CURRENT
    if cant_update_reason:
        return BLOCKED
    return "update"


def why_cant_update(app, writable=os.access):
    """Why this copy can't replace itself, or None."""
    if not in_applications_folder(app):
        return f"{APP_NAME} isn't in your Applications folder"
    if not (writable(app, os.W_OK) and writable(Path(app).parent, os.W_OK)):
        return f"{APP_NAME} can't replace itself in {Path(app).parent}"
    return None


# ── command line tools ──────────────────────────────────────

def _run(arguments, **kwargs):
    return subprocess.run(arguments, capture_output=True, **kwargs)


def _curl(url, output, max_time, accept=None):
    """Fetches `url` into `output` ('-' for stdout). Returns (HTTP status, stdout); 0 for file:// URLs."""
    arguments = ["/usr/bin/curl", "--silent", "--show-error", "--location", "--max-time", str(max_time),
                 "--output", str(output), "--write-out", "\n%{http_code}"]
    if accept:
        arguments += ["--header", f"Accept: {accept}"]
    result = _run([*arguments, url], text=True)
    if result.returncode != 0:
        raise UpdateError(f"Couldn't reach GitHub ({result.stderr.strip() or result.returncode}).")
    body, _, code = result.stdout.rpartition("\n")
    return int(code or 0), body


def own_team(app):
    result = _run(["/usr/bin/codesign", "-d", "--verbose=2", str(app)], text=True)
    return team_identifier(result.stderr)


def info_value(app, key):
    """Reads Info.plist directly: NSBundle caches by path, which would go stale here."""
    try:
        with open(Path(app) / "Contents" / "Info.plist", "rb") as f:
            return plistlib.load(f).get(key)
    except (OSError, plistlib.InvalidFileException):
        return None


def _attach(image, attempts=3):
    """Mounts the disk image without showing it in Finder and returns where. It's mounted
    in /Volumes like any other disk image: mounted anywhere else, macOS's file access
    protection blocks reading it.

    The first attach of a freshly written image sometimes fails with "Resource temporarily
    unavailable" and leaves it attached with no volume, which also makes the next attach
    fail. So detach whatever is left and try again."""
    for attempt in range(attempts):
        result = _run(["/usr/bin/hdiutil", "attach", str(image), "-nobrowse", "-readonly", "-noautoopen", "-plist"])
        if result.returncode == 0:
            entities = plistlib.loads(result.stdout).get("system-entities", [])
            mount = next((e["mount-point"] for e in entities if e.get("mount-point")), None)
            if mount:
                return mount
        _detach_image(image)
        if attempt < attempts - 1:
            time.sleep(3)
    raise UpdateError(f"hdiutil couldn't open the disk image ({result.returncode}).")


def _detach_image(image):
    try:
        images = plistlib.loads(_run(["/usr/bin/hdiutil", "info", "-plist"]).stdout).get("images", [])
    except (plistlib.InvalidFileException, ValueError):
        return
    for entry in images:
        if Path(entry.get("image-path", "")).resolve() != Path(image).resolve():
            continue
        devices = [e["dev-entry"] for e in entry.get("system-entities", []) if e.get("dev-entry")]
        if devices:
            _run(["/usr/bin/hdiutil", "detach", min(devices, key=len), "-force"])  # the whole disk


def copy_app_from_image(image, destination, bundle_id):
    """Copies the app out of the disk image, then ejects it. It's found by bundle
    identifier, not name."""
    mount = _attach(image)
    try:
        apps = [p for p in Path(mount).iterdir() if p.suffix == ".app" and not p.is_symlink()]
        app = next((p for p in apps if info_value(p, "CFBundleIdentifier") == bundle_id), None)
        if app is None:
            raise UpdateError(f"The latest release's disk image doesn't contain {APP_NAME}.")
        copied = _run(["/usr/bin/ditto", str(app), str(destination)])
        if copied.returncode != 0:
            raise UpdateError(f"ditto failed ({copied.returncode}).")
    finally:
        _run(["/usr/bin/hdiutil", "detach", mount, "-force"])


def verify(app, team, bundle_id):
    """Checks that the whole app is intact and signed by this team for this bundle
    identifier, and returns its version."""
    result = _run(["/usr/bin/codesign", "--verify", "--deep", "--strict",
                   "-R", signing_requirement(bundle_id, team), str(app)], text=True)
    if result.returncode != 0:
        log.warning("Rejected update: %s", result.stderr.strip())
        raise UpdateError(f"The downloaded app was changed or isn't signed by the {APP_NAME} developer.")
    version = info_value(app, "CFBundleShortVersionString")
    if not version:
        raise UpdateError(f"The latest release's disk image doesn't contain {APP_NAME}.")
    return version


class Updater:
    """Checks for, and installs, a newer release. `check` blocks; run it off the main thread.

    `on_status(state, version)` is told when a download starts. `quit_app` is called once
    the new version is in place and set to reopen."""

    def __init__(self, app, version, on_status=lambda state, version: None, quit_app=lambda: None):
        self._app = app
        # What's actually installed; the build copies version.VERSION into Info.plist.
        self._current = (info_value(app, "CFBundleShortVersionString") if app else None) or version
        self._bundle_id = info_value(app, "CFBundleIdentifier") if app else None
        self._on_status = on_status
        self._quit_app = quit_app
        # For testing, OPENSHUTTERCOUNT_UPDATE_FEED can point somewhere else (a file:// URL
        # works); the signature check still applies to whatever it finds there.
        self._feed = os.environ.get("OPENSHUTTERCOUNT_UPDATE_FEED") or FEED_URL

    @property
    def current(self):
        return self._current

    def check(self):
        """Checks GitHub's latest release and, if it's newer, installs it. Returns an Outcome."""
        if self._app is None:
            return Outcome(SKIPPED, reason="running from source")
        team = own_team(self._app)
        if team is None:
            return Outcome(SKIPPED, reason="this copy is signed ad hoc, so an update couldn't be verified")
        try:
            release = self._latest_release()
        except UpdateError as e:
            log.error("Update check failed: %s", e)
            return Outcome(FAILED, reason=str(e))

        reason = why_cant_update(self._app)
        action = decide(self._current, release, reason)
        if action == CURRENT:
            log.info("Up to date at %s", self._current)
            return Outcome(CURRENT)
        version, url = release
        if action == BLOCKED:
            log.warning("%s is out, and this copy can't update itself: %s", version, reason)
            return Outcome(BLOCKED, version, reason)

        try:
            log.info("Downloading %s", version)
            self._on_status("downloading", version)
            self._install(self._download(url, team))
        except UpdateError as e:
            log.error("Update to %s failed: %s", version, e)
            return Outcome(FAILED, version, str(e))
        return Outcome(INSTALLED, version)

    def _latest_release(self):
        """(version, disk image URL), or None if the repository has no releases yet."""
        code, body = _curl(self._feed, "-", max_time=CHECK_TIMEOUT, accept="application/vnd.github+json")
        if code == 404:
            return None
        if code not in (200, 0):
            raise UpdateError(f"GitHub answered with an error ({code}).")
        try:
            return parse_release(json.loads(body))
        except ValueError:
            raise UpdateError("GitHub's answer didn't describe a release.") from None

    def _download(self, url, team):
        from Foundation import NSURL, NSFileManager, NSItemReplacementDirectory, NSUserDomainMask

        # A temporary folder on the same volume as the installed app, so the swap is a rename.
        folder_url, error = NSFileManager.defaultManager().URLForDirectory_inDomain_appropriateForURL_create_error_(
            NSItemReplacementDirectory, NSUserDomainMask, NSURL.fileURLWithPath_(str(self._app)), True, None)
        if folder_url is None:
            raise UpdateError(f"Couldn't make a folder for the download ({error}).")
        folder = Path(folder_url.path())
        try:
            image = folder / "update.dmg"
            code, _ = _curl(url, image, max_time=DOWNLOAD_TIMEOUT)
            if code not in (200, 0):
                raise UpdateError(f"GitHub answered with an error ({code}).")
            app = folder / self._app.name
            copy_app_from_image(image, app, self._bundle_id)
            image.unlink(missing_ok=True)
            version = verify(app, team, self._bundle_id)
            # The tag can be newer than the app inside if a release was put together wrong.
            if not is_newer(version, self._current):
                raise UpdateError(f"The latest release contains version {version}, which isn't newer than this one.")
            return version, app, folder
        except Exception:
            shutil.rmtree(folder, ignore_errors=True)
            raise

    def _install(self, staged):
        from Foundation import NSURL, NSFileManager, NSFileManagerItemReplacementUsingNewMetadataOnly

        version, app, folder = staged
        self._on_status("installing", version)
        # Swaps the two folders in one step, so there's never a moment without an app.
        # Only the new copy's metadata is kept, so nothing from the original download
        # (like the quarantine flag macOS checked on first launch) carries over.
        ok, _, error = NSFileManager.defaultManager().replaceItemAtURL_withItemAtURL_backupItemName_options_resultingItemURL_error_(
            NSURL.fileURLWithPath_(str(self._app)), NSURL.fileURLWithPath_(str(app)), None,
            NSFileManagerItemReplacementUsingNewMetadataOnly, None, None)
        shutil.rmtree(folder, ignore_errors=True)
        if not ok:
            log.error("Couldn't install %s: %s", version, error)
            raise UpdateError(f"{APP_NAME} couldn't replace itself in {self._app.parent}.")
        log.info("Installed %s; relaunching", version)
        relaunch(self._app)
        self._quit_app()
