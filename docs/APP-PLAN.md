# Open Shutter Count app: the agreed plan

Agreed with the maintainer on 2026-11-04, and built the same day, apart from
step 5 (the maintainer's `v0.5.0` run). Ask before departing from any decision
below. "Where things are" describes the code before the app; DEVELOPMENT.md
describes it now.

## Goal

A small Mac app, **Open Shutter Count**, that walks someone through reading
their Canon's shutter count, and keeps itself up to date from GitHub releases
the way Mac Engine Room does. It replaces `Read Shutter Count.command`.

## Decisions

### Shape

- Mac Engine Room's Python app (pywebview window, PyInstaller bundle) with the
  Go tool and libusb inside it. The Go code stays; neither the camera code nor
  the updater is rewritten.
- Name **Open Shutter Count**, bundle ID `com.digitaln.openshuttercount`.
  The repo keeps its name. Apple silicon only.
- App source goes in a new `app/` folder.
- Mac Engine Room's look (fonts, colours, light and dark mode).
- Icon: a simple aperture, drawn by a script so it can be rebuilt.

### Launch and updates

- First launch: Mac Engine Room's offer to move into Applications.
- Checks for an update at **every** launch, before anything else. No
  once-a-day file, no wake observer, no settings toggle.
- A newer release is **forced**: it downloads, verifies, installs and
  relaunches before anyone can read.
- If the check or download fails (offline, GitHub down, signature check
  fails): reading is allowed with the current version, and the screen says the
  update didn't work.
- If there's a newer release and this copy can't update itself (not in
  Applications, can't replace itself): reading is blocked, with a "Download the
  new version" button that opens the releases page.
- Copies run from source or signed ad hoc skip the check (development).

### Wizard

1. Checking for updates, or updating.
2. Camera setup checklist: USB connection set to Photo Import/Remote Control
   (on the R8: MENU > Setup (wrench) > page 4 > Choose USB connection app),
   Wi-Fi and Bluetooth off, card in, data cable straight into the Mac, Image
   Capture, Photos and EOS Utility closed. (Changed by the maintainer on
   2026-11-05: a list to read, not boxes to tick, since Continue never needed
   them ticked; no line under the heading; the R8 is called the Canon R8.)
3. Waiting for the camera: checks every second and moves on by itself. If none
   is found: help for USB mode, macOS's accessory prompt, and cables. A Canon in
   the wrong USB mode (the R8 then shows up as `04a9:32a6` with no still image
   interface) gets a message naming the setting to change. When another app
   holds the camera, show the tool's `darwinBusyHint` message.
4. Reading.
5. Result: yes or no, and the counts as ranges. Only the EOS R8 and the EOS 5D
   Mark IV say "tested". Any other camera that reads says "not fully tested on
   the <model>; if these numbers look wrong, please send the log". Buttons:
   Save Log, Open an Issue, Read Again. (Changed by the maintainer on
   2026-11-05: a reading says "Success! We've read your shutter count.", with
   nothing under it for the R8 and the 5D Mark IV; other bodies still say
   they're not fully tested. The counts show without where each was read from
   or the note on rounding. Open an Issue fills in the log, so nothing is
   attached by hand, and the issue no longer asks for the shutter mode.)

Every outcome ends on the result screen, including no camera found and failed
reads.

### Reading rule (Go changes)

Changed by the maintainer on 2026-11-04, after the build: split EF bodies from
RF ones rather than singling out the 5D Mark IV. It's now decided by what the
camera lists, which libgphoto2's camera dumps show lines up with that split
while keeping the factory channel away from the 90D:

1. Any Canon body: 0xD1AC first.
2. A body that lists 0x9052 and 0x9053 (the 5D Mark IV, and EF DSLRs up to
   about DIGIC 7): the factory channel, at the 5D Mark IV's addresses.
3. A body that lists the event channel (RF bodies, the 90D, newer EOS M): 0xD167
   the R8's way. The maintainer approved this for non-RF bodies the same day.
4. Anything else: "can't read this camera yet, please send the log".

Only the R8 and the 5D Mark IV say tested. The rule as first agreed:

1. Any Canon body: the standard count property (0xD1AC) first.
2. EOS 5D Mark IV: the factory channel, as it works today.
3. RF bodies: the R8 way (0xD167 off the event channel), sent only if the camera
   lists those commands (`events.go` already checks). The maintainer approved
   extending the R8's tethering commands to RF bodies on 2026-11-04. Update
   CLAUDE.md's camera safety section to say so.
4. Anything else (other DSLRs such as the 90D, EOS M bodies, PowerShots, other
   brands): "can't read this camera yet, please send the log". The factory
   channel is **not** tried. EOS M stays here until a log shows the same 0xD167
   layout as the R8.

Spot an RF body by the model name in the camera's PTP device info
(`ptp.DeviceInfo.Model`), not the USB product string (the R8's is "Canon Digital
Camera"), and not a list of USB IDs, so future RF bodies work without an
update. A pattern like "EOS R" followed by a number, "P", or nothing; it must
not match "EOS Rebel" DSLRs. Check what the R8 actually reports first.

This relaxes CLAUDE.md's "no guessing in what the tool prints" to "a guess is
allowed when it's labelled as not fully tested". Update CLAUDE.md.

### The log

- Only written when someone clicks Save Log; saved as a `.txt` where they
  choose. Open an Issue fills all of it but the camera exchange into the new
  issue (GitHub won't take a link long enough for the exchange); Save Log still
  writes the whole thing.
- Holds: app name and version, repo URL, date and time of the reading, Mac model
  and macOS version, the camera's model, firmware and USB IDs, the commands it
  lists, each step and what it got back, the result, and the full exchange with
  the camera at the end.
- **No serial number**, anywhere, including the device info in the camera
  exchange.

### Nothing left behind

No app log, no preferences, a web view that stores nothing, window state not
saved, and the update download and any temporary files deleted. macOS keeps its
own records (the USB accessory approval, "Open Anyway"), which is accepted.

### Releases

- Built and signed on the maintainer's Mac with the same Apple Development
  certificate as Mac Engine Room (team `XT4LT7MS6Y`), with Mac Engine Room's
  package script: a DMG whose window shows the app, an arrow, and Applications.
- The DMG is the only file in a release.
- From main only. Versions start at `v0.5.0` and go up by 0.0.1 (`v0.5.10`
  after `v0.5.9`). One version for the app and the Go tool inside it.
- Fixes are tested by making a new release; there's no separate test-build
  launcher.

### Repo changes

- Delete `.github/workflows/release.yml`, `.github/release.sh` and
  `Read Shutter Count.command`.
- Keep `mac/Find Shutter Counter.command` and `mac/launcher.sh` (for working
  out new cameras), and keep committing `mac/openshuttercounter` (built on Linux
  by `mac/build.sh`), so the Mac needs only Python, not Go.
- Keep `test.yml` and add the Python tests to it. (Changed by the maintainer on
  2026-11-05: no CI and no Linux or Windows support, so releases follow Mac
  Engine Room's path. `test.yml` and the Linux and Windows USB backends are gone.)
- Add a "New camera" issue template (camera model, shutter mode it was in, what
  the app said, the attached log), like Mac Engine Room's sensor report.
- Add a third-party notices file: Python, pywebview, pyobjc, libusb (LGPL,
  shipped as its own dylib), and Mac Engine Room's MIT notice for the code
  copied from it.
- Rewrite the README (install steps and privacy, like Mac Engine Room's), add a
  development doc with the release checklist, and update CLAUDE.md (layout,
  releases, how testing works, camera safety) and R8-PROGRESS.

## Where things are

### Mac Engine Room

`https://github.com/DigitalN/mac-engine-room`, public (clone it read-only).
Copy from it:

- `Application Files/updater.py`: checks GitHub's latest release, downloads the
  `.dmg`, copies the app out by bundle ID, runs `codesign --verify --deep
  --strict` against `identifier "<bundle id>" and anchor apple generic and
  certificate leaf[subject.OU] = "<this copy's team>"`, swaps it in and
  relaunches. What changes here: when it runs (above), no `last-update-check`
  file, no settings, and the "can't update itself" case blocks instead of
  turning updates off.
- `Application Files/launch.py`: `in_applications_folder`, `offer_to_move`,
  `relaunch`, `REPOSITORY`.
- `Application Files/main.py`, `index.html`, `MacEngineRoom.spec`,
  `requirements.txt` (pinned: pywebview 6.1, pyobjc 12.1, PyInstaller 6.19.0).
- `scripts/build.sh` (signs with the first valid Apple Development identity;
  builds in `~/Library/Caches/...` because iCloud Drive's Finder metadata breaks
  codesign), `scripts/package.sh` and `scripts/make-dmg-background.swift`.
- `docs/DEVELOPMENT.md`: release checklist, how updates work, testing the
  updater.
- `tests/test_updater.py`, `tests/test_launch.py`.
- `tools/preview/`: previews the page in a browser with a stub, which is how
  the UI can be checked on Linux.
- `.github/ISSUE_TEMPLATE/sensor-report.yml`, `THIRD-PARTY-NOTICES`.

### This repo

- `pkg/manufacturers/canon/reader.go`, `Read()`: today R8 (by USB ID, in
  `eventModels`) goes to 0xD167; everything else tries 0xD1AC, then the
  factory channel, using a guessed memory map (`FallbackModel`) for unknown
  bodies. `noFactoryChannel` only shields the R8. Only `events.go` checks
  `info.Supports()`; the factory channel doesn't.
- `pkg/manufacturers/canon/models.go`: the 5D Mark IV is the only factory
  channel model, `Confirmed: true`.
- `pkg/ptp/dataset.go`: `DeviceInfo` has `Model` and `SerialNumber`.
- `internal/cmd/utils.go`: `darwinBusyHint`.
- `openshuttercounter --json` gives each count's range (`min` and `value`);
  `--trace` writes the camera exchange.
- `pkg/usb/usb_darwin.go` looks for `libusb-1.0.dylib` next to the executable
  first, so keep them side by side in the bundle.
- Check that `openshuttercounter list` sends nothing to the camera before
  polling with it every second.

## Order of work

1. Go: the reading rule, the serial left out of the trace, tests, then
   `bash mac/build.sh` and commit the binary.
2. App: copy Mac Engine Room's app, launcher and updater; change them as above;
   build the wizard; Python tests.
3. Build and package scripts, the icon script, the version source.
4. Repo changes and docs.
5. The maintainer builds and publishes `v0.5.0` on their Mac.

Claude can't run the Mac app. Check the Python with tests on Linux, the page
with Mac Engine Room's preview tool, and leave the rest to the maintainer's
`v0.5.0` run.
