"""Open Shutter Count: reads a Canon camera's shutter count over USB, step by step.

The window is a pywebview page (index.html) that talks to Python over pywebview's
in-process bridge, so the app opens no network ports. Its only network request is the
update check at launch (updater.py). The camera is read by the openshuttercounter tool
in the app bundle (camera.py).

Nothing is left behind: no log file, no preferences, no window state, and a web view
that stores nothing (cleanup.py). Python's own messages go to stderr only.
"""

import logging
import os
import sys
from pathlib import Path

import camera
import cleanup
import launch
import report
from updater import Updater
from version import VERSION
from wizard import Wizard

APP_NAME = launch.APP_NAME
BUNDLE_ID = "com.digitaln.openshuttercount"
WINDOW_SIZE = (600, 700)
MIN_WINDOW_SIZE = (480, 560)

log = logging.getLogger("openshuttercount")


def _app_files_dir():
    """Directory holding index.html: the PyInstaller bundle when frozen, else this folder."""
    if getattr(sys, "frozen", False):
        return Path(sys._MEIPASS)
    return Path(__file__).resolve().parent


class Api:
    """Methods callable from the page as window.pywebview.api.<name>().

    pywebview exposes every public attribute recursively, so everything that is not
    meant for JavaScript lives behind a leading underscore.
    """

    def __init__(self, app):
        self._app = app

    def state(self):
        return self._app._wizard.state()

    def setup_done(self):
        self._app._wizard.setup_done()

    def back_to_setup(self):
        self._app._wizard.back_to_setup()

    def stop_looking(self):
        self._app._wizard.stop_looking()

    def read_again(self):
        self._app._wizard.read_again()

    def save_log(self):
        """Save Log: asks where, then writes the log there. Returns where, or None."""
        import webview

        saved = self._app._wizard.log()
        if saved is None:
            return None
        name, text = saved
        chosen = self._app._window.create_file_dialog(webview.FileDialog.SAVE, save_filename=name,
                                                      file_types=("Text files (*.txt)",))
        if not chosen:
            return None
        path = Path(chosen if isinstance(chosen, str) else chosen[0])
        try:
            path.write_text(text, encoding="utf-8")
        except OSError as e:
            log.warning("Couldn't save the log: %s", e)
            return {"error": str(e)}
        return {"path": str(path), "name": path.name}

    def open_issue(self):
        """Open an Issue: GitHub's new-issue page, with the log filled in."""
        wizard = self._app._wizard
        result = wizard.state().get("result") or {}
        saved = wizard.log()
        _open_url(report.issue_url(launch.REPOSITORY_URL, result, saved[1] if saved else ""))

    def download_update(self):
        _open_url(launch.RELEASES_URL)

    def quit(self):
        self._app.quit()

    def log_error(self, message):
        log.warning("Page error: %s", str(message)[:1000])


def _open_url(url):
    from AppKit import NSURL, NSWorkspace
    from PyObjCTools import AppHelper

    AppHelper.callAfter(NSWorkspace.sharedWorkspace().openURL_, NSURL.URLWithString_(url))


class OpenShutterCountApp:
    def __init__(self):
        self._app_path = launch.bundle_path()
        self._window = None
        self._updater = Updater(self._app_path, VERSION,
                                on_status=lambda state, version: self._wizard.on_update_status(state, version),
                                quit_app=self.quit)
        self._wizard = Wizard(self._updater, camera.Camera(camera.tool_path(), note=f"{APP_NAME} {VERSION}"),
                              app_name=APP_NAME, version=self._updater.current, repository_url=launch.REPOSITORY_URL)

    def quit(self):
        import AppKit
        from PyObjCTools import AppHelper

        AppHelper.callAfter(AppKit.NSApp.terminate_, None)

    def _leave_nothing_behind(self):
        bundle_id = _own_bundle_id()
        try:
            cleanup.forget_preferences(bundle_id)
        except Exception:
            log.exception("Couldn't empty the preferences")
        cleanup.remove_leftovers(Path.home(), bundle_id)

    def _install_delegate_methods(self):
        """Clean up as the app quits, however it quits: pywebview's app delegate lets
        ⌘Q terminate the app without returning from webview.start()."""
        import objc
        from webview.platforms import cocoa

        app = self

        def applicationWillTerminate_(self, notification):  # noqa: N802 (AppKit's name)
            app._leave_nothing_behind()

        def applicationShouldTerminateAfterLastWindowClosed_(self, sender):  # noqa: N802
            return True

        objc.classAddMethods(cocoa.BrowserView.AppDelegate, [
            objc.selector(applicationWillTerminate_, selector=b"applicationWillTerminate:", signature=b"v@:@"),
            objc.selector(applicationShouldTerminateAfterLastWindowClosed_,
                          selector=b"applicationShouldTerminateAfterLastWindowClosed:", signature=b"Z@:@"),
        ])

    def _on_loaded(self):
        from PyObjCTools import AppHelper

        def not_restorable():
            if self._window is not None and self._window.native is not None:
                self._window.native.setRestorable_(False)
                launch.activate_app()

        AppHelper.callAfter(not_restorable)

    def run(self):
        import AppKit
        import webview
        import webview.platforms.cocoa as cocoa  # sets up NSApp, so the move offer can show
        from Foundation import NSUserDefaults

        NSUserDefaults.standardUserDefaults().registerDefaults_(cleanup.volatile_defaults)
        cocoa.WebKit = cleanup.NoStorageWebKit(cocoa.WebKit)

        if self._app_path and not launch.in_applications_folder(self._app_path):
            if launch.offer_to_move(self._app_path):
                return  # the copy in Applications starts once this one has quit

        AppKit.NSApp.setActivationPolicy_(AppKit.NSApplicationActivationPolicyRegular)
        self._install_delegate_methods()

        # Before anything else: the page shows the check, and nothing can be read until
        # it's done.
        self._wizard.start()

        html = (_app_files_dir() / "index.html").read_text(encoding="utf-8")
        self._window = webview.create_window(
            APP_NAME, html=html, js_api=Api(self), width=WINDOW_SIZE[0], height=WINDOW_SIZE[1],
            min_size=MIN_WINDOW_SIZE, background_color=_background_color())
        self._window.events.loaded += self._on_loaded
        webview.start(private_mode=True, debug=False)
        self._leave_nothing_behind()
        os._exit(0)


def _own_bundle_id():
    """This copy's bundle identifier: a test copy has its own (docs/DEVELOPMENT.md)."""
    if getattr(sys, "frozen", False):
        from Foundation import NSBundle

        return NSBundle.mainBundle().bundleIdentifier() or BUNDLE_ID
    return BUNDLE_ID


def _background_color():
    import AppKit

    best = AppKit.NSApp.effectiveAppearance().bestMatchFromAppearancesWithNames_(
        [AppKit.NSAppearanceNameAqua, AppKit.NSAppearanceNameDarkAqua])
    return "#1c1c1e" if best == AppKit.NSAppearanceNameDarkAqua else "#f5f5f7"


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s  %(levelname)-8s  %(message)s",
                        handlers=[logging.StreamHandler()])  # stderr only: no log file
    logging.getLogger("pywebview").setLevel(logging.WARNING)
    try:
        OpenShutterCountApp().run()
    except Exception:
        log.exception("%s failed to start", APP_NAME)
        raise


if __name__ == "__main__":
    main()
