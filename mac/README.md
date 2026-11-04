# Running it on a Mac without Terminal

This folder is ready to run on an Apple silicon Mac. Nothing to install: libusb
ships alongside the tool.

1. Fetch this repository with git (GitHub Desktop is fine). Copies fetched with
   git aren't quarantined, so macOS lets them run.
2. Set up the camera: Wi-Fi and Bluetooth off, a card in, switched on, plugged
   straight into the Mac with a data cable (many USB-C cables only charge).
   Quit Image Capture, Photos and EOS Utility if any of them opened.

   On the EOS R8, the USB connection must be set to photo transfer: MENU >
   Setup (wrench) > page 4 > **Choose USB connection app** > **Photo
   Import/Remote Control**.

   On an Apple silicon Mac, allow the camera when macOS asks whether to let
   the accessory connect. A declined camera still charges but can't be seen.
3. Double-click **Read Shutter Count.command**. A Terminal window shows the
   count. Press Return to close it.

If macOS asks whether Terminal may access the folder this lives in, allow it:
that's where the readings are saved.

## Finding a camera's counter

For a camera the tool can't read yet, double-click **Find Shutter Counter.command**.
It reads everything the camera reports: standard PTP read requests, plus, on a
Canon, the event channel EOS Utility uses (SetRemoteMode, SetEventMode,
GetEvent, RequestDevicePropValue). Then it asks you to unplug the camera and
take an exact number of photos with each shutter mode, reads everything again,
and shows what went up by exactly those numbers. The two reads and the
comparison are saved in `readings/`.

On the EOS R8: 20 photos with Shutter mode set to Elec. 1st-curtain, then 7
with Electronic. Different numbers are what let the comparison tell the two
shutters apart.

## EOS R8: not readable yet

The R8 connects and opens a session, but refuses the standard shutter counter
property, and doesn't list Canon's factory channel at all. When the factory
channel (`MonOpen`) was sent anyway, it stopped answering USB until it was
switched off. The tool now stops before the factory channel on an R8, so
`Read Shutter Count` can't hang the camera. See `docs/R8-PROGRESS.md`.

## What it saves

Each run writes two files to `readings/`, which git picks up so they can be
committed and pushed:

- `<date>.txt`: what was printed, the macOS version, the USB devices seen, and
  debug logs.
- `<date>-trace.json`: every message exchanged with the camera. It includes the
  camera's USB serial number.

## What's here

| File | What it is |
|---|---|
| `Read Shutter Count.command` | Reads the shutter count |
| `Find Shutter Counter.command` | Looks for where a camera keeps its count: standard reads and Canon's event channel |
| `launcher.sh` | What the two launchers share |
| `openshuttercounter` | The tool, built from this repository by `build.sh` |
| `libusb-1.0.dylib` | libusb 1.0.30 from the [libusb-package](https://github.com/pyocd/libusb-package) wheel on PyPI, checked against a pinned SHA-256 |
| `build.sh` | Rebuilds both binaries (macOS or Linux, needs Go) |

The tool stops `ptpcamerad`, the macOS service that grabs cameras as they're
plugged in, if it's holding the camera. macOS starts it again afterwards.
