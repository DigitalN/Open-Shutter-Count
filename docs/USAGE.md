# Using openshuttercounter from the command line

The [Open Shutter Count app](../README.md) is easier. This page is for the tool
inside it, on its own, on a Mac.

## Get it

The build in `tool/mac/` of this repository, with the copy of libusb beside it, is the
one the app bundles. Or, with Go installed:

```bash
go install github.com/DigitalN/openshuttercounter/tool/cmd/openshuttercounter@latest
```

Releases only carry the app.

## Commands

```bash
openshuttercounter                          # read the connected camera
openshuttercounter --raw                    # also show the raw replies
openshuttercounter --serial                 # also show the serial number on the body
openshuttercounter --json                   # machine readable output
openshuttercounter --trace session.json     # record the whole conversation
openshuttercounter list                     # what is connected
openshuttercounter models                   # cameras this build knows
openshuttercounter decode session.json      # print a recorded conversation
openshuttercounter probe --out before.json  # save everything the camera reports
openshuttercounter compare before.json after.json --frames 20
```

`decode` reads traces written by openshuttercounter, logs saved by the app (the
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
the tool stops it and retries when the camera is busy, and macOS restarts it
afterwards.

## What it sends

Every Canon body is asked the standard shutter count property (0xD1AC), which
bodies up to about DIGIC 5 answer. After that, what's sent depends on what the
camera lists in GetDeviceInfo:

- the factory channel (`MonOpen`, `MonReadAndGetData`, `MonClose`), on the EOS
  5D Mark IV and on any body that lists 0x9052 and 0x9053: EF DSLRs up to about
  DIGIC 7. Bodies other than the 5D Mark IV are read at its addresses;
- otherwise Canon's event channel, with the tethering commands EOS Utility
  sends (SetRemoteMode, SetEventMode, GetEvent, and RequestDevicePropValue for
  0xD167), on any body that lists them: RF bodies, the 90D, newer EOS M. 0xD167
  is read the way the R8 lays it out. A body that lists both is tried here if
  the factory channel gives nothing;
- a body that lists neither is sent nothing more, and reported as one that
  can't be read yet.

The R8 and the 90D don't list the factory channel, and both hung when it was
sent anyway, so it's never sent to a body that doesn't list it.

Only the EOS R8 and the EOS 5D Mark IV are tested; any other reading says it's
not fully tested.

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
| `mac/` | The Mac binary and libusb, which the app bundles, and `Find Shutter Counter.command` |
| `cmd/`, `internal/` | The CLI |
| `pkg/manufacturers` | What a camera maker has to provide to be readable |
| `pkg/manufacturers/canon` | Canon's factory and event channels, decoding, model list |
| `pkg/ptp` | PTP over USB, enough of it to send vendor opcodes |
| `pkg/usb` | USB on macOS, through libusb |
| `pkg/trace`, `pkg/pcap` | Recording conversations, and reading USB captures |
| `docs/` | This, the protocol reference, how to add a camera, and the third-party notices |
