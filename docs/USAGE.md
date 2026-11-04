# Using openshuttercounter from the command line

On an Apple silicon Mac, double-clicking `Read Shutter Count.command` is
easier. This page is for everything else.

## Get it

Each [release](https://github.com/DigitalN/openshuttercounter/releases) has
command line builds for Linux (amd64, arm64), Windows (amd64) and Apple
silicon Macs. Or, with Go installed:

```bash
go install github.com/DigitalN/openshuttercounter/cmd/openshuttercounter@latest
```

## Commands

```bash
openshuttercounter                          # read the connected camera
openshuttercounter --raw                    # also show the raw replies
openshuttercounter --json                   # machine readable output
openshuttercounter --trace session.json     # record the whole conversation
openshuttercounter list                     # what is connected
openshuttercounter models                   # cameras this build knows
openshuttercounter decode session.json      # print a recorded conversation
openshuttercounter probe --out before.json  # save everything the camera reports
openshuttercounter compare before.json after.json --frames 20
```

`decode` reads traces written by openshuttercounter, and USB captures in pcap
or pcapng format. `probe --events` also reads a Canon body's event channel,
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

### Linux

Reading a camera without root needs a udev rule, for example in
`/etc/udev/rules.d/99-canon-ptp.rules`:

```
SUBSYSTEM=="usb", ATTR{idVendor}=="04a9", MODE="0660", TAG+="uaccess"
```

Then `sudo udevadm control --reload-rules && sudo udevadm trigger`, and replug
the camera. If you get *device is claimed by another process*, that's gvfs:

```bash
gio mount -u gphoto2://
systemctl --user mask gvfs-gphoto2-volume-monitor
```

### macOS

USB goes through libusb, loaded at runtime. The copy in `mac/` ships with it;
otherwise `brew install libusb`. macOS gives every camera to `ptpcamerad`;
the tool stops it and retries when the camera is busy, and macOS restarts it
afterwards.

### Windows

No driver replacement is needed: the tool opens the still image device
interface the stock driver already exposes. Close anything else that might
have the camera open. Not yet tried against a camera.

## Safety

This tool only reads. On Canon bodies it sends standard PTP reads, the factory
channel's `MonOpen`, `MonReadAndGetData` and `MonClose`, and, on the EOS R8,
Canon's tethering commands for the event channel (SetRemoteMode, SetEventMode,
GetEvent, RequestDevicePropValue). The factory channel also carries commands
that write calibration data and the counters themselves; the code
[refuses to build them](../pkg/manufacturers/canon/factory.go), and a test
checks that. More in [PROTOCOL.md](PROTOCOL.md#safety).

## Repository layout

| Path | Contents |
|---|---|
| `Read Shutter Count.command` | The Mac launcher |
| `mac/` | The Mac binary, libusb, and the launchers' shared script |
| `cmd/`, `internal/` | The CLI |
| `pkg/manufacturers` | What a camera maker has to provide to be readable |
| `pkg/manufacturers/canon` | Canon's factory and event channels, decoding, model list |
| `pkg/ptp` | PTP over USB, enough of it to send vendor opcodes |
| `pkg/usb` | The three USB backends |
| `pkg/trace`, `pkg/pcap` | Recording conversations, and reading USB captures |
| `docs/` | This, the protocol reference, and how to add a camera |
