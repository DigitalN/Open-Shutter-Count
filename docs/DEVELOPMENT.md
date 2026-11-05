# Developing Open Shutter Count

Building the app, testing it, and making a release. For using it, see the
[README](../README.md); for the command line tool, [USAGE.md](USAGE.md). The agreed plan
the app was built to is [APP-PLAN.md](APP-PLAN.md).

## How it fits together

- `app/`: the Mac app, Python. A pywebview window (`index.html`) over a wizard
  (`wizard.py`): update, set up, wait for the camera, read, result.
  - `updater.py` and `launch.py`: the update check at every launch, and the offer to move
    into Applications. Adapted from Mac Engine Room's.
  - `camera.py`: runs the `openshuttercounter` tool inside the app (`list --json` while
    waiting, `read --json --serial --trace` to read) and says what the result screen
    shows. The serial number is for the screen only; `report.py` never uses it.
  - `report.py`: the log Save Log writes, and the Open an Issue link.
  - `cleanup.py`: what makes sure nothing is left behind.
  - `version.py`: the version, for the app and the tool.
- `tool/`: the `openshuttercounter` command line tool, a Go module of its own.
  - `cmd/`, `internal/` and `pkg/`: its Go code. The reading rule is in
    `pkg/manufacturers/canon/reader.go`.
  - `mac/openshuttercounter` and `mac/libusb-1.0.dylib`: the tool and libusb, built on
    Linux or a Mac by `mac/build.sh` and committed, so the Mac needs only Python. The
    app bundle has them side by side in `Contents/MacOS`.
- `docs/`: these pages, and `THIRD-PARTY-NOTICES`, the licenses of what the app bundles.

## Build from source

You need Python 3 from [python.org](https://www.python.org/downloads/) and the pinned
packages:

```bash
python3 -m pip install -r app/requirements.txt
```

Then:

```bash
app/scripts/build.sh
```

builds `Open Shutter Count.app` in `~/Library/Caches/OpenShutterCount-build/dist`, signed
with your **Apple Development** certificate, and prints its path. Folders synced by
iCloud Drive (such as `~/Documents`) add Finder metadata to app bundles, which codesign
rejects, so it builds outside the repository. Without a certificate it's signed ad hoc:
it works, but doesn't check for updates. A free Apple ID is enough to get one: Xcode ›
Settings › Apple Accounts › sign in › your **Personal Team** › Manage Certificates… ›
**+** › **Apple Development**.

To run from source without building (on a Mac, with the packages installed):

```bash
python3 app/main.py
```

A copy run from source doesn't check for updates, and uses `tool/mac/openshuttercounter`.

## Tests

```bash
make check
```

runs everything that has to pass before a push: `go vet`, the Go tests and the linter,
here and for the Mac, and the app's tests (`pip install pytest` off a Mac). It also
remakes `tool/mac/openshuttercounter` and the app's fixtures, which come out the same
every time, and fails if either was stale: commit what it remade, then run it again.

It needs Go (`brew install go` on a Mac), and runs Go 1.27.0 whatever's installed:
the Makefile and `tool/mac/build.sh` pin it, and Go fetches it the first time. The tool's
bytes depend on the Go version, and golangci-lint can't read what Go 1.27.3 compiles.

There's no CI, as with Mac Engine Room. `.github/hooks/pre-push` runs `make check` before
every push, and `Package a Release.command` runs the app's tests again before it builds.
Claude sessions turn the hook on when they start (`.claude/hooks/session-start.sh`);
anywhere else, run `git config core.hooksPath .github/hooks` once. The tool only reads
cameras on macOS; on any other machine it builds and its tests run, but it finds no
camera.

The app's tests read what the tool prints from `app/tests/fixtures`. Those files are made
by the Go tests, from the real code reading a scripted camera, so the two can't drift.
`make check` remakes them, or on their own:

```bash
cd tool && UPDATE_FIXTURES=1 go test ./internal/cmd -run TestAppFixtures
```

To look at the page without a Mac:

```bash
python3 app/preview/serve.py
```

serves it at `http://127.0.0.1:8766/` with a stand-in for the app. `?s=r8` and the other
scenarios at the top of `app/preview/stub.js` show each screen; add `&theme=dark` for
dark mode. The result screens come from the fixtures, through the app's own code.

## Release checklist

Releases are public, and installed copies update themselves from the latest one at
their next launch, so a mistake reaches everyone. A new release is also how a fix is
tested on the camera: there's no separate test build.

Claude does every step below, every time, in order, without being reminded. On the
maintainer's Mac that's all of it: building, signing and publishing need the certificate
in its keychain. A session in the cloud can't reach the Mac, so it stops before *Build
and check the disk image* and hands the rest over, with the version and the release
notes written out.

Run the steps one at a time, and read each result before a step that can't be undone
(merging, publishing, turning the previous release into a draft).

### Before building

- [ ] Everything going out is merged into `main`, and the Mac's `main` matches GitHub
  (`git pull`).
- [ ] `VERSION` in `app/version.py` is raised by 0.0.1 (`0.5.10` after `0.5.9`), on a
  `claude/release-<version>` branch whose pull request Claude opens and merges. It must
  be higher than the latest release, or installed copies won't update.
- [ ] `make check` passes on that branch. It remakes `tool/mac/openshuttercounter` with
  the new version, so commit that with the bump; `app/scripts/build.sh` refuses to build
  if the two differ.
- [ ] The signing certificate is valid and not about to expire:
  `security find-identity -v -p codesigning` lists an *Apple Development* identity from
  team `XT4LT7MS6Y`, the same as Mac Engine Room's. Installed copies refuse updates
  signed by any other team.
- [ ] The README matches what's shipping.

### Build and check the disk image

- [ ] Double-click `app/scripts/Package a Release.command` (or run
  `app/scripts/package.sh`). It runs the tests, refuses anything but a clean `main`
  that matches GitHub, and makes `dist/Open-Shutter-Count-<version>.dmg`. That's the
  only file a release has.
- [ ] Open the DMG: Open Shutter Count and Applications with the arrow between them, and
  the first-launch card at the bottom easy to read.
- [ ] The app inside has the new version, a valid signature, and the right team:
  ```bash
  M=$(hdiutil attach -nobrowse -readonly -noautoopen dist/Open-Shutter-Count-0.5.0.dmg | grep -o '/Volumes/.*$')
  defaults read "$M/Open Shutter Count.app/Contents/Info" CFBundleShortVersionString
  codesign --verify --deep --strict "$M/Open Shutter Count.app" && codesign -dvv "$M/Open Shutter Count.app" 2>&1 | grep TeamIdentifier
  "$M/Open Shutter Count.app/Contents/MacOS/openshuttercounter" --version
  hdiutil detach "$M"
  ```

### Test updating, if the updater, packaging or signing changed

- [ ] A test copy updates to a higher-version build from a stand-in release (see
  *Testing the updater* below), and reopens in it.
- [ ] It refuses a build changed after signing, one signed ad hoc or by another team,
  and a release whose tag is newer than the app inside it, and still lets you read.
- [ ] Afterwards, the test copy and the test disk images are deleted.

### Publish

- [ ] On GitHub: Releases › Draft a new release. Tag `v<version>` on `main`, title
  `Open Shutter Count <version>`, the DMG as the only file. Notes: what's new, then the
  README's Install section. Or:
  ```bash
  gh release create v0.5.0 dist/Open-Shutter-Count-0.5.0.dmg --title "Open Shutter Count 0.5.0" --notes-file notes.md
  ```
  Drafts and pre-releases are skipped by the updater.
- [ ] Turn the previous release into a draft, so the page lists only the current one
  (`gh release edit v0.5.0 --draft=true`).

### After publishing

- [ ] `https://github.com/DigitalN/openshuttercounter/releases/latest` goes to the new
  tag, and GitHub's API shows only the `.dmg`:
  ```bash
  curl -s https://api.github.com/repos/DigitalN/openshuttercounter/releases/latest | grep -E '"tag_name"|"name": "Open-Shutter-Count'
  ```
- [ ] The DMG downloaded from the page is identical to the one in `dist/`
  (`shasum -a 256`).
- [ ] An older installed copy updates when opened: it says *Updating to version …*,
  then reopens in the new version (the footer shows it).

### Signing

Releases are signed with a free Apple Development certificate. They aren't notarized by
Apple, so people click **Open Anyway** the first time. Development certificates last a
year, so package a new release with a fresh certificate from the same Apple account
before the old one expires.

## How updates work

`app/updater.py`, from Mac Engine Room's:

- **When:** at every launch, before anything else. Nothing is remembered between
  launches, and there's no setting.
- **What:** GitHub's
  [latest release](https://docs.github.com/en/rest/releases/releases#get-the-latest-release),
  through `/usr/bin/curl` (the bundled Python has no certificates of its own). Drafts
  and pre-releases don't count. If its tag is newer, it downloads the release's `.dmg`.
- **Checks:** copies the app with this bundle identifier
  (`com.digitaln.openshuttercount`) out of the disk image and runs
  `codesign --verify --deep --strict` with the requirement
  `identifier "com.digitaln.openshuttercount" and anchor apple generic and certificate leaf[subject.OU] = "<this copy's team>"`.
  The version inside must also be newer than the installed one.
- **Installing:** right away. It swaps the new app in with one `replaceItemAtURL` call,
  deletes the download, and reopens (`launch.RELAUNCH_SCRIPT`: waits for this copy to
  quit, then `open -n`, and again if it isn't running 5 seconds later).
- **If it can't:** a check or download that fails (offline, GitHub down, the signature
  check fails) lets you read with the current version, and the screens say the update
  didn't work. A newer release that this copy can't install (not in Applications, or
  can't replace itself) blocks reading, with a button that opens the releases page.
- **Off** for copies run from source or signed ad hoc. The footer says so.

## Testing the updater

Test with a separate copy that has its own bundle identifier, so it can't touch the
installed app and Launch Services doesn't mix the two up.

1. Build, then make the test copy and a higher-version update from it, each re-signed
   with your certificate after editing its `Info.plist`:
   ```bash
   APP=$(app/scripts/build.sh)
   ID=$(security find-identity -v -p codesigning | awk '/"Apple Development/ {print $2; exit}')
   mkdir -p /tmp/osc-update /tmp/osc-feed
   copy() {  # destination version
     ditto "$APP" "$1"
     /usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier com.digitaln.openshuttercount.test" \
       -c "Set :CFBundleShortVersionString $2" -c "Set :CFBundleVersion $2" "$1/Contents/Info.plist"
     codesign --force --deep -s "$ID" "$1"
   }
   copy "$HOME/Applications/Open Shutter Count Test.app" 0.0.1
   copy "/tmp/osc-update/Open Shutter Count.app" 9.9.9
   hdiutil create -volname "Open Shutter Count" -srcfolder /tmp/osc-update -format UDZO /tmp/osc-feed/Open-Shutter-Count-9.9.9.dmg
   ```
2. Write a stand-in for GitHub's latest-release response whose `.dmg` asset's
   `browser_download_url` is a `file://` URL, in `/tmp/osc-feed/latest.json`:
   ```json
   {"tag_name": "v9.9.9", "assets": [{"name": "Open-Shutter-Count-9.9.9.dmg",
     "browser_download_url": "file:///tmp/osc-feed/Open-Shutter-Count-9.9.9.dmg"}]}
   ```
3. Open the test copy against it:
   ```bash
   open -n --env OPENSHUTTERCOUNT_UPDATE_FEED=file:///tmp/osc-feed/latest.json \
     "$HOME/Applications/Open Shutter Count Test.app"
   ```
   It says *Updating to version 9.9.9…*, then reopens; the footer says 9.9.9.
4. To check that bad updates are refused, change the update's `Info.plist` after it's
   signed, re-sign it with `codesign --force --deep -s -`, or sign it with another bundle
   identifier. Each should end on the checklist with *Updating to 9.9.9 didn't work*,
   and leave the test copy alone. To check the block, run a test copy from outside
   Applications: it should stop at *Version 9.9.9 is out*.
5. Clean up: quit the test copy, then delete it, `/tmp/osc-update` and `/tmp/osc-feed`.

The updater mounts the disk image in `/Volumes`, hidden from Finder: mounted anywhere
else, macOS's file access protection stops the app from reading it.

## Nothing left behind

The app writes no log file and no preferences. `cleanup.py` makes sure of the rest:

- The web view uses WebKit's non-persistent data store. pywebview's own private mode
  uses the default store, which writes to disk, so `NoStorageWebKit` hands it the
  non-persistent one instead.
- Window state isn't saved: `ApplePersistenceIgnoreState`, registered for the launch
  only, and the window isn't restorable.
- When the app quits, it empties its preferences (where the save panel remembers its
  last folder) and removes anything under its bundle identifier in `~/Library`:
  `Preferences`, `Saved Application State`, `Caches`, `WebKit`, `HTTPStorages` and
  `Application Support`.
- The trace each read makes goes in a temporary folder that's deleted straight after;
  the update download is deleted once it's installed or refused.

To check on the Mac after a read and quit:

```bash
ls -d ~/Library/*/com.digitaln.openshuttercount* 2>/dev/null
```

should print nothing.
