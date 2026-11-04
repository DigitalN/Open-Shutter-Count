# EOS R8 progress

Work so far is on branch `claude/loving-goldberg-zwh734`, which the maintainer's
local copy (`~/Documents/GitHub/openshuttercounter`) has checked out.

## Done

**Security review of the upstream code.** Nothing nefarious. No network code
(net/http is linked in only through zap's unused level handler). It writes only
files the user names, and the only program it starts is `killall ptpcamerad`. No root,
no install, no login items. Five dependencies end up in the binary (purego,
pkg/errors, urfave/cli v3, zap, multierr), all verified against Go's checksum
database. The large `go.mod` is the linter and release tooling, not compiled
in. govulncheck couldn't run (vuln.go.dev blocked from the cloud session).

**A double-click Mac copy in `mac/`.**
- `openshuttercounter`: darwin/arm64, built by `mac/build.sh`.
- `libusb-1.0.dylib`: libusb 1.0.30 from the libusb-package wheel on PyPI,
  pinned by SHA-256. The tool now looks for libusb next to itself first.
- `Read Shutter Count.command` and `Find Shutter Counter.command`, sharing
  `launcher.sh`. Each saves a log (with debug output and the USB devices seen)
  and traces in `mac/readings/` (gitignored), and closes its Terminal window
  after Return.

**Code changes.**
- macOS: retries opening a camera ptpcamerad holds (5 times, 300 ms apart).
- macOS: clears both endpoints after claiming; if OpenSession times out, sends
  the PTP class Device Reset request (0x66), clears endpoints, retries once.
  Timeouts are `usb.ErrTimeout`.
- macOS: uses only a still image (class 6) interface. With none, sends nothing
  and lists what the device offers (`usb.ErrNoStillImage`), with a hint about
  the camera's USB mode. Interfaces are logged and saved in traces.
- The trace is saved even when the session never opens.
- Canon: bodies in `noFactoryChannel` (the R8, 0x330c) never get `MonOpen`.
- New `probe` and `compare` commands, standard PTP reads only: GetDeviceInfo,
  then GetDevicePropDesc or GetDevicePropValue for each listed property.
  `compare` flags anything that went up by exactly the number of photos taken.
  Dataset parsers in `pkg/ptp/dataset.go`, with tests.

## What the R8 did

- USB mode must be Photo Import/Remote Control: MENU > Setup (wrench) > page 4
  > Choose USB connection app. In its other mode the R8 enumerates as
  `04a9:32a6` with no still image interface, only a vendor one (0xff/0xf0,
  bulk 0x84/0x05); PTP sent there got no answer.
- In the right mode: `04a9:330c`, product string "Canon Digital Camera".
  Interface 0 still image (bulk in 0x81, bulk out 0x02, interrupt 0x83);
  interface 1 vendor specific 0xff/0xf0 (bulk 0x84/0x05).
- OpenSession (transaction ID 1): OK.
- GetDevicePropValue(0xD1AC): 0x2005 OperationNotSupported. That code may mean
  GetDevicePropValue itself isn't supported; the probe's GetDeviceInfo will say.
- `MonOpen` over 0x9052 (params 0,0; data `MonOpen\0`, argc 0, 24 zero
  bytes): no reply in 10 s, then the camera refused even CloseSession until it
  was switched off and on.
- Shutter modes: Elec. 1st-curtain and Electronic only. Test photos must use
  Elec. 1st-curtain.
- Earlier problems were a charge-only USB-C cable, and macOS's accessory
  approval on Apple silicon laptops (a declined device charges but can't be seen).

## Next

1. Maintainer runs `mac/Find Shutter Counter.command` (20 photos between probes)
   and uploads the `*-probe*` files. Read which operations and properties the
   R8 lists, and whether anything moved by exactly 20.
2. If a property moved by exactly 20: add a reading method for it, confirm
   with a second run at a different count (e.g. 7), and make
   `Read Shutter Count.command` print it.
3. If nothing moved: stage 3 is Canon's tethering event table (SetRemoteMode
   0x9114, SetEventMode 0x9115, GetEvent 0x9116), diffed the same way. Those
   aren't read-only, so ask the maintainer first. `GetDeviceInfoEx` (0x9108) is a
   read and could go in the same stage.
4. If that finds nothing too, stop: the count isn't reachable this way. The
   ShutterCount Mac app reportedly read an R8 in January 2025, and Canon service
   can read it.

## Loose ends

- Xcode opens after runs, per the maintainer. Nothing in the launchers or the tool
  opens files or apps; not yet explained. Ask when exactly it happens.
- The Terminal window closing after Return hasn't been confirmed working.
- No pull request has been opened, and nothing has been reported upstream.
