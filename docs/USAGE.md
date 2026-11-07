# Using openshuttercount from the command line

The [Open Shutter Count app](../README.md) is easier. This page is for the tool
inside it, on its own, on a Mac.

## Get it

The build in `tool/mac/` of this repository, with the copy of libusb beside it, is the
one the app bundles. Or, with Go installed:

```bash
go install github.com/DigitalN/Open-Shutter-Count/tool/cmd/openshuttercount@latest
```

Releases only carry the app.

## Commands

```bash
openshuttercount                          # read the connected camera
openshuttercount --raw                    # also show the raw replies
openshuttercount --serial                 # also show the serial number on the body
openshuttercount --json                   # machine readable output
openshuttercount --trace session.json     # record the whole conversation
openshuttercount --tested-list list.json  # say tested by another list (below)
openshuttercount list                     # what is connected
openshuttercount models                   # cameras this build knows
openshuttercount decode session.json      # print a recorded conversation
openshuttercount probe --out before.json  # save everything the camera reports
openshuttercount compare before.json after.json --frames 20
```

`decode` reads traces written by openshuttercount, logs saved by the app (the
trace is at the end), and USB captures in pcap or pcapng format. Traces leave
out the camera's serial number, and on Canon bodies its owner, artist and
copyright names. `probe --events` also reads a Canon body's event channel,
and `compare` shows what went up by the number of photos taken between two
probes. That's how a new camera gets worked out; see
[CONTRIBUTING.md](CONTRIBUTING.md).

## Setting up the camera

- USB connection set to photo transfer. On Canon mirrorless bodies: Setup menu
  > Choose USB connection app > Photo Import/Remote Control.
- Wi-Fi and Bluetooth off. On many bodies Wi-Fi disables the USB port.
- A card in the camera.
- A data cable, plugged in directly rather than through a hub. USB 2 ports
  have been more reliable than USB 3.

USB goes through libusb, loaded at runtime. The copy in `tool/mac/` ships with it;
otherwise `brew install libusb`. macOS gives every camera to `ptpcamerad`;
the tool stops it when the camera is busy, force-stops it if it doesn't let go,
retries, and macOS restarts it afterwards.

## What it sends

Every Canon body is asked the standard shutter count property (0xD1AC), which
bodies up to about DIGIC 5 answer. After that, what's sent depends on what the
camera lists in GetDeviceInfo:

- the factory channel (`MonOpen`, `MonReadAndGetData`, `MonClose`), on the EOS
  5D Mark IV and on any body that lists 0x9052 and 0x9053: EF DSLRs up to about
  DIGIC 7. Bodies other than the 5D Mark IV are read at its addresses;
- otherwise Canon's event channel, with the tethering commands EOS Utility
  sends (SetRemoteMode, SetEventMode, GetEvent, and RequestDevicePropValue for
  0xD167 and then 0xD1AF, the serial number on the body, if the camera didn't
  report them), on any body that lists them: RF bodies, the 90D, newer EOS M. 0xD167
  is read the way the R8 lays it out. A body that lists both is tried here if
  the factory channel gives nothing;
- a body that lists neither is sent nothing more, and reported as one that
  can't be read yet.

The R8 and the 90D don't list the factory channel, and both hung when it was
sent anyway, so it's never sent to a body that doesn't list it.

## Which readings say tested

A reading says it's tested when the tested list has the camera's model and the way it
was read: `method` in `read --json`, one of `ptp-property-0xd1ac`,
`canon-factory-channel` or `canon-event-channel-0xd167`. Any other reading says it's not
fully tested. The tool has the list built in, from
[`tool/pkg/manufacturers/canon/tested.json`](../tool/pkg/manufacturers/canon/tested.json).
The list never changes what's sent to the camera: only that label, and whether a failed
read is one that can't be read yet.

`--tested-list <file>`, on a read and on `models`, uses the list in that file instead,
laid out like `tested.json`. The app passes the copy it fetches from GitHub this way. A
usable list replaces the built-in one entirely, so it can take cameras off as well as
add them. It never makes the command fail:

- a file that can't be read, doesn't parse, or has no usable entry is ignored, and the
  built-in list is used;
- an entry with no model or an unknown method is skipped, and fields the tool doesn't
  know are ignored;
- the steps say what happened (`models` prints them; a read without `--json` logs
  them), and `read --json` says which list it used in `tested_list`: `source` is
  `given` or `built in`, `entries` is how many entries counted, and `reason` says why a
  given list wasn't used.

## Safety

This tool only reads. The factory channel also carries commands
that write calibration data and the counters themselves; the code
[refuses to build them](../tool/pkg/manufacturers/canon/factory.go), and a test
checks that. More in [PROTOCOL.md](PROTOCOL.md#safety).

## Repository layout

| Path | Contents |
|---|---|
| `app/` | The Open Shutter Count app; [DEVELOPMENT.md](DEVELOPMENT.md) |
| `tool/` | This tool, a Go module of its own; the paths below are inside it |
| `mac/` | The Mac binary and libusb, which the app bundles, and `Find Shutter Count.command` |
| `cmd/`, `internal/` | The CLI |
| `pkg/manufacturers` | What a camera maker has to provide to be readable |
| `pkg/manufacturers/canon` | Canon's factory and event channels, decoding, model list, and `tested.json` |
| `pkg/ptp` | PTP over USB, enough of it to send vendor opcodes |
| `pkg/usb` | USB on macOS, through libusb |
| `pkg/trace`, `pkg/pcap` | Recording conversations, and reading USB captures |
| `docs/` | This, the protocol reference, how to add a camera, and the third-party notices |
