# Running it on a Mac without Terminal

This folder is ready to run on an Apple silicon Mac. Nothing to install: libusb
ships alongside the tool.

1. Fetch this repository with git (GitHub Desktop is fine). Copies fetched with
   git aren't quarantined, so macOS lets them run.
2. Set up the camera: Wi-Fi and Bluetooth off, a card in, switched on, plugged
   straight into the Mac with a data cable. Quit Image Capture, Photos and EOS
   Utility if any of them opened.
3. Double-click **Read Shutter Count.command**. A Terminal window shows the
   count. Press Return to close it.

If macOS asks whether Terminal may access the folder this lives in, allow it:
that's where the readings are saved.

## What it saves

Each run writes two files to `readings/` (not committed to git):

- `<date>.txt`: what was printed, the macOS version, the USB devices seen, and
  debug logs.
- `<date>-trace.json`: every message exchanged with the camera. It includes the
  camera's USB serial number.

## What's here

| File | What it is |
|---|---|
| `Read Shutter Count.command` | The double-click launcher |
| `openshuttercounter` | The tool, built from this repository by `build.sh` |
| `libusb-1.0.dylib` | libusb 1.0.30 from the [libusb-package](https://github.com/pyocd/libusb-package) wheel on PyPI, checked against a pinned SHA-256 |
| `build.sh` | Rebuilds both binaries (macOS or Linux, needs Go) |

The tool stops `ptpcamerad`, the macOS service that grabs cameras as they're
plugged in, if it's holding the camera. macOS starts it again afterwards.
