"""Leaving nothing behind.

Open Shutter Count keeps no log, no preferences and no window state, and its web view
stores nothing. Some of that takes doing:

- WebKit's default data store writes to disk, and pywebview's "private mode" still uses
  it (it only clears it at launch). `NoStorageWebKit` hands pywebview a data store that
  keeps everything in memory instead.
- AppKit saves window state for restoring it at the next launch; that's turned off for
  this launch only (`volatile_defaults`, which aren't written anywhere).
- macOS's save panel remembers the last folder in the app's preferences. Those, and
  anything else macOS or WebKit made under the app's bundle identifier, are removed when
  the app quits (`remove_leftovers`).

macOS keeps its own records, such as the USB accessory approval and "Open Anyway". Those
are its business, not the app's.
"""

import logging
import shutil
from pathlib import Path

log = logging.getLogger("openshuttercount")

# Registered for this launch only: the registration domain is never saved.
volatile_defaults = {"ApplePersistenceIgnoreState": True, "NSQuitAlwaysKeepsWindows": False}


def leftovers(home, bundle_id):
    """Everywhere macOS or WebKit may write for an app with this bundle identifier."""
    library = Path(home) / "Library"
    return [
        library / "Preferences" / f"{bundle_id}.plist",
        library / "Saved Application State" / f"{bundle_id}.savedState",
        library / "Caches" / bundle_id,
        library / "WebKit" / bundle_id,
        library / "HTTPStorages" / bundle_id,
        library / "HTTPStorages" / f"{bundle_id}.binarycookies",
        library / "Application Support" / bundle_id,
    ]


def remove_leftovers(home, bundle_id):
    """Removes whatever of `leftovers` exists. Returns what it removed."""
    if not bundle_id:
        return []
    removed = []
    for path in leftovers(home, bundle_id):
        try:
            if path.is_dir() and not path.is_symlink():
                shutil.rmtree(path)
            elif path.exists() or path.is_symlink():
                path.unlink()
            else:
                continue
        except OSError as e:
            log.warning("Couldn't remove %s: %s", path, e)
            continue
        removed.append(path)
    return removed


def forget_preferences(bundle_id):
    """Empties the app's preferences through cfprefsd, which would otherwise write its
    cached copy back after the file is deleted."""
    from Foundation import NSUserDefaults

    defaults = NSUserDefaults.standardUserDefaults()
    defaults.removePersistentDomainForName_(bundle_id)
    defaults.synchronize()


class NoStorageWebKit:
    """The WebKit module as pywebview's Cocoa backend sees it, except that every web view
    it makes uses one data store that keeps everything in memory.

    pywebview makes each web view's configuration with
    `WebKit.WKWebViewConfiguration.alloc().init()` and then asks
    `WebKit.WKWebsiteDataStore.defaultDataStore()`; both come back here. Everything else
    is the real module."""

    def __init__(self, webkit):
        self._webkit = webkit
        self._store = None

    def data_store(self):
        if self._store is None:
            self._store = self._webkit.WKWebsiteDataStore.nonPersistentDataStore()
        return self._store

    def __getattr__(self, name):
        if name == "WKWebViewConfiguration":
            return _Configuration(self)
        if name == "WKWebsiteDataStore":
            return _DataStore(self)
        return getattr(self._webkit, name)


class _Configuration:
    def __init__(self, shim):
        self._shim = shim

    def alloc(self):
        return self

    def init(self):
        config = self._shim._webkit.WKWebViewConfiguration.alloc().init()
        config.setWebsiteDataStore_(self._shim.data_store())
        return config


class _DataStore:
    def __init__(self, shim):
        self._shim = shim

    def defaultDataStore(self):  # noqa: N802 (WebKit's name)
        return self._shim.data_store()

    def __getattr__(self, name):
        return getattr(self._shim._webkit.WKWebsiteDataStore, name)
