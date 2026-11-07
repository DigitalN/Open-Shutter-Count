# Mac support files

The tool and libusb, which the Open Shutter Count app bundles (see
`docs/DEVELOPMENT.md`), and a launcher for working out cameras that can't be read
yet.

| File | What it is |
|---|---|
| `openshuttercount` | The tool, for Apple silicon Macs, built by `build.sh` with `app/version.py`'s version |
| `libusb-1.0.dylib` | libusb 1.0.30 from the [libusb-package](https://github.com/pyocd/libusb-package) wheel on PyPI, checked against a pinned SHA-256 |
| `launcher.sh` | What `Find Shutter Count.command` uses: the camera setup check, logging, closing the window |
| `Find Shutter Count.command` | For a camera that can't be read yet: reads everything it reports, asks for an exact number of photos with each shutter mode, reads again, and shows what went up by those numbers |
| `build.sh` | Rebuilds both binaries (macOS or Linux, needs Go) |
| `readings/` | Created on the first run. Each run saves what it printed, with debug logs, and a trace of everything sent to and from the camera. Probes include the camera's serial number, so git ignores the `.json` files and only the `.txt` logs can be committed. |

The tool stops `ptpcamerad`, the macOS service that grabs cameras as they're
plugged in, if it's holding the camera, and force-stops it if it doesn't let go.
macOS starts it again afterwards.

On the EOS R8 the counts are read through Canon's event channel, the way EOS
Utility talks to the camera. The R8 keeps them in thousands, rounded up:

```
  Mechanical shutter       1,001 to 2,000   (0xd167 word 3)
  Electronic shutter     19,001 to 20,999   (word 4 less word 3)
  All photos             21,001 to 22,000   (0xd167 word 4)
```

The R8 doesn't list Canon's factory channel, and hung when it was sent anyway,
so the tool only sends it to the EOS 5D Mark IV. See `docs/R8-PROGRESS.md`.
