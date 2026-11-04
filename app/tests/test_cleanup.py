"""Leaving nothing behind."""

from cleanup import NoStorageWebKit, leftovers, remove_leftovers, volatile_defaults

BUNDLE_ID = "com.digitaln.openshuttercount"


def test_what_macos_and_webkit_wrote_is_removed(tmp_path):
    library = tmp_path / "Library"
    for folder in ("Caches", "WebKit", "HTTPStorages", "Saved Application State", "Preferences"):
        (library / folder).mkdir(parents=True)
    (library / "Caches" / BUNDLE_ID / "WebKit").mkdir(parents=True)
    (library / "WebKit" / BUNDLE_ID).mkdir()
    (library / "Saved Application State" / f"{BUNDLE_ID}.savedState").mkdir()
    (library / "Preferences" / f"{BUNDLE_ID}.plist").write_bytes(b"bplist00")
    (library / "HTTPStorages" / f"{BUNDLE_ID}.binarycookies").write_bytes(b"")
    # Someone else's, which stays.
    (library / "Preferences" / "com.digitaln.macengineroom.plist").write_bytes(b"bplist00")
    (library / "Caches" / "com.apple.Safari").mkdir()

    removed = remove_leftovers(tmp_path, BUNDLE_ID)

    assert len(removed) == 5
    assert all(not p.exists() for p in leftovers(tmp_path, BUNDLE_ID))
    assert (library / "Preferences" / "com.digitaln.macengineroom.plist").exists()
    assert (library / "Caches" / "com.apple.Safari").exists()


def test_nothing_is_removed_without_a_bundle_id(tmp_path):
    (tmp_path / "Library" / "Caches").mkdir(parents=True)
    assert remove_leftovers(tmp_path, "") == []
    assert (tmp_path / "Library" / "Caches").exists()


def test_every_leftover_is_named_after_the_app():
    assert all(BUNDLE_ID in p.name for p in leftovers("/Users/someone", BUNDLE_ID))


def test_window_state_isnt_saved():
    assert volatile_defaults["ApplePersistenceIgnoreState"] is True
    assert volatile_defaults["NSQuitAlwaysKeepsWindows"] is False


class FakeConfiguration:
    made = []

    def __init__(self):
        self.store = None
        FakeConfiguration.made.append(self)

    @classmethod
    def alloc(cls):
        return cls()

    def init(self):
        return self

    def setWebsiteDataStore_(self, store):  # noqa: N802
        self.store = store


class FakeDataStore:
    @staticmethod
    def nonPersistentDataStore():  # noqa: N802
        return "in memory"

    @staticmethod
    def defaultDataStore():  # noqa: N802
        return "on disk"

    @staticmethod
    def allWebsiteDataTypes():  # noqa: N802
        return {"cookies"}


class FakeWebKit:
    WKWebViewConfiguration = FakeConfiguration
    WKWebsiteDataStore = FakeDataStore
    WKWebView = "the real web view"


def test_pywebviews_web_views_keep_everything_in_memory():
    webkit = NoStorageWebKit(FakeWebKit)

    # What pywebview's Cocoa backend does for each window.
    config = webkit.WKWebViewConfiguration.alloc().init()
    datastore = webkit.WKWebsiteDataStore.defaultDataStore()
    config.setWebsiteDataStore_(datastore)

    assert config.store == "in memory"
    assert datastore == "in memory"
    assert webkit.WKWebsiteDataStore.allWebsiteDataTypes() == {"cookies"}
    assert webkit.WKWebView == "the real web view"
    # One store for every window.
    assert webkit.WKWebViewConfiguration.alloc().init().store == "in memory"
