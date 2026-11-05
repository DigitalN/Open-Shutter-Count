# openshuttercounter (DigitalN fork)

An independent project that started from Maxime Visonneau's
openshuttercounter. Don't point anything at mvisonneau's repositories or
accounts; LICENSE keeps his copyright line because MIT requires it. The goal here is reading the shutter count of the maintainer's Canon EOS R8
on their Mac (MacBookPro18,1, M1 Pro, macOS 27.2) without using Terminal. That's
the Open Shutter Count app.

Read [docs/R8-PROGRESS.md](../docs/R8-PROGRESS.md) first: what's been done, what
the R8 did, and what's next. The app was built to
[docs/APP-PLAN.md](../docs/APP-PLAN.md); ask before departing from it.
[docs/DEVELOPMENT.md](../docs/DEVELOPMENT.md) has how it fits together, the
release checklist and how updates work.

## Layout

- The top level is kept for what a user needs: the README, LICENSE and
  THIRD-PARTY-NOTICES, plus what Go and the tooling need there.
- `app/`: the Mac app (Python, pywebview). `version.py` is the one version, for
  the app and the tool. `scripts/` builds, packages and draws the icon;
  `tests/` and `preview/` are below.
- `mac/`: the tool and libusb the app bundles, `launcher.sh` and
  `Find Shutter Counter.command` (for working out new cameras).
- `docs/`: CONTRIBUTING.md, DEVELOPMENT.md, PROTOCOL.md (5D Mark IV factory
  channel), R8-PROGRESS.md, APP-PLAN.md, USAGE.md.

## Releases

Built, signed and packaged on the maintainer's Mac with
`app/scripts/package.sh` (or `Package a Release.command`), from `main` only,
signed with the same Apple Development certificate as Mac Engine Room (team
`XT4LT7MS6Y`). A release is `v<version>` with the DMG as its only file. Versions
start at 0.5.0 and go up by 0.0.1. Installed copies update from the latest
release at every launch, so a new release is also how a fix gets tested on the
camera.

Claude opens every pull request into `main` and merges it once CI is green. For
a release, Claude works through the checklist in docs/DEVELOPMENT.md; its top
says which steps are Claude's and which need the maintainer's Mac. Claude does
its steps, then hands the maintainer the Mac steps with the version and the
release notes written out.

## How testing works

- Claude can't reach the Mac or the camera. The maintainer makes a release and
  opens the app, and sends the log Save Log wrote (no serial number in it) or
  what the screen said. `Find Shutter Counter.command` still saves `.txt` logs
  in `mac/readings/` for working out a camera; probes (`.json`) are ignored
  because they hold the camera's serial number. Earlier readings were taken out
  of the repo and its history for privacy; docs/R8-PROGRESS.md quotes what they
  showed.
- After any Go change, rebuild the Mac binary with `bash mac/build.sh` (works
  on Linux; reproducible; the Go linker ad-hoc signs it; it takes the version
  from `app/version.py`) and commit `mac/openshuttercounter` with the change.
- After changing what `read --json` or `list --json` prints, or what a read
  sends, regenerate the app's fixtures:
  `UPDATE_FIXTURES=1 go test ./internal/cmd -run TestAppFixtures`.
- Checks before pushing: `go vet ./...` for linux, darwin and windows;
  `go test ./...`; the repo's linter for each OS:
  `go build -o .bin/golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint`
  then `GOOS=<os> .bin/golangci-lint run ./...`, then `rm -rf .bin`; and
  `python3 -m pytest app/tests`.
- The page can be checked on Linux with `python3 app/preview/serve.py` and a
  browser (scenarios at the top of `app/preview/stub.js`). The rest of the app
  (AppKit, the updater, signing) is checked by the maintainer's release run.

## Camera safety

- Never send a command that writes to the camera, and never sweep opcodes.
  `pkg/manufacturers/canon` refuses the known write commands; keep it that way.
- The reading rule (`canon.Reader.Read`), by what the camera lists in
  GetDeviceInfo: 0xD1AC on any Canon body; then the factory channel on the 5D
  Mark IV (by USB product ID) and on bodies that list 0x9052 and 0x9053 (EF
  DSLRs, at the 5D Mark IV's addresses); otherwise the event channel on bodies
  that list it (RF bodies, the 90D, newer EOS M). A body that lists both is
  tried on the event channel if the factory channel gives nothing. Anything
  else gets "can't be read yet" and is sent nothing more. The maintainer chose
  this EF/RF split on 2026-11-04 (APP-PLAN.md).
- The R8 hung on `MonOpen` (the factory channel) and needed a power cycle, and
  a 90D did the same upstream. Neither lists 0x9052/0x9053, so the factory
  channel is only sent to bodies that list it, and the 5D Mark IV.
- The R8 doesn't list 0x9052/0x9053 in GetDeviceInfo. Don't send an opcode a
  camera doesn't list.
- Standard PTP reads are fine. The maintainer approved (2026-11-04) Canon's
  tethering commands for reading the event channel, and only these:
  SetRemoteMode 0x9114 (params 1, then 0 and 1 on the way out), SetEventMode
  0x9115 (1, then 0), GetEvent 0x9116, and RequestDevicePropValue 0x9127 for
  0xD167 only. They're in `pkg/manufacturers/canon/events.go`, sent by
  `probe --events` and by a read of a body that lists them. The maintainer
  approved extending them from the R8 to RF bodies, then to any Canon body that
  lists them (the 90D, EOS M), on 2026-11-04, and each is sent only if the
  camera lists it. Ask before sending anything else,
  including 0x9108 GetDeviceInfoEx.
- No guessing in what the tool prints, except where the guess is labelled as
  not fully tested: a value isn't named (mechanical, electronic, total) as fact
  until a test proves what it counts. Only the EOS R8 and the EOS 5D Mark IV are
  tested; a reading from any other body says it's not fully tested.
- No serial number in anything the tool or the app writes: traces blank it, and
  Canon's owner, nickname, artist and copyright properties too (`pkg/trace`,
  `canon.ScrubEvents`).
- When the camera stops answering, the maintainer switches it off and on (battery out
  if it won't switch off).
