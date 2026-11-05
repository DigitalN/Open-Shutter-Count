# Open Shutter Count

A small Mac app that reads your Canon camera's shutter count over USB, step by step.
Free and open source.

**[Download the latest version](https://github.com/DigitalN/openshuttercounter/releases/latest)**
(for Macs with Apple silicon)

- **Read off the camera itself.** A photo only carries the total. The mechanical count
  is kept inside the camera, and this reads it from there.
- **Read-only.** It never changes a setting, a file or a count on the camera.
- **Walks you through it.** It checks the camera is set up, waits for it, reads it, and
  says what it found, or what to do if it couldn't.

## Cameras

| Camera | What it reads |
|---|---|
| Canon EOS R8 | Mechanical, electronic and all photos, each to within a thousand: the camera only keeps them that way. Tested. |
| Canon EOS 5D Mark IV | Exact shutter and mirror counts. Tested. |
| Other EF DSLRs that list Canon's factory channel (7D Mark II, 5DS R, 80D, 77D and so on) | Read the way the 5D Mark IV is, and labelled not fully tested |
| RF bodies, and newer bodies that report the R8's counter (90D, EOS M50 Mark II, M6 Mark II) | Read the way the R8 is, and labelled not fully tested |
| Older Canon bodies that answer the standard shutter count | Read, and labelled not fully tested |
| Anything else | Can't be read yet. Save the log and open an issue, and it can be worked out. |

## Install

1. **Download** `Open-Shutter-Count-….dmg` from the
   [latest release](https://github.com/DigitalN/openshuttercounter/releases/latest) and
   open it.
2. **Drag Open Shutter Count into Applications**, as the arrow shows.
3. **Open it** from Applications.

The first time you open it, macOS may say it can't verify Open Shutter Count. Click
**Done**, then go to **System Settings › Privacy & Security**, scroll down, and click
**Open Anyway**. You only do this once: Open Shutter Count isn't distributed through
Apple, so Apple hasn't checked it. All of its code is in this repository.

It keeps itself up to date: each time it opens, it installs a new version if there is
one, and reopens.

## Reading a camera

1. Open the app. It checks for an update first.
2. Set up the camera as the checklist says: USB connection set to **Photo Import/Remote
   Control** (on the EOS R8: MENU › Setup (wrench) › page 4 › Choose USB connection
   app), Wi-Fi and Bluetooth off, a card in, plugged straight into the Mac with a data
   cable, and Image Capture, Photos and EOS Utility closed.
3. Switch the camera on and plug it in. It's found by itself. On a Mac laptop, click
   **Allow** if macOS asks whether the accessory can connect.
4. It reads the camera and shows the counts.

If it couldn't read your camera, or the numbers look wrong, click **Save Log**, then
**Open an Issue** and attach the log.

## Privacy

- **The only connection is the update check**: each time it opens, it asks GitHub for
  the latest release. It only installs updates signed by the same developer. No
  analytics, crash reports or web server.
- **The log is only made when you click Save Log**, and only goes where you save it. It
  has the camera's model, firmware and what it answered, your Mac's model and macOS
  version, and nothing that identifies you or your camera: no serial number, owner or
  copyright name.
- **Nothing is left behind**: no log file, no preferences, no saved window, and the
  update download is deleted. macOS keeps its own records, such as the accessory
  approval and Open Anyway.

## Command line

The `openshuttercounter` tool inside the app also runs on its own on a Mac. See
[docs/USAGE.md](docs/USAGE.md).

## How it works

Canon keeps the counts in different places on different bodies:
[docs/PROTOCOL.md](docs/PROTOCOL.md) covers the factory channel the 5D Mark IV uses, and
[docs/R8-PROGRESS.md](docs/R8-PROGRESS.md) covers the event channel the R8 uses. Building
the app and making a release: [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

## Credit

Started from Maxime Visonneau's openshuttercounter, which worked out Canon's factory
channel. The work it built on is credited in
[docs/PROTOCOL.md](docs/PROTOCOL.md#prior-work). The app's updater, look and packaging
come from [Mac Engine Room](https://github.com/DigitalN/mac-engine-room). What's
bundled with it, and their licenses, are in [THIRD-PARTY-NOTICES](THIRD-PARTY-NOTICES).

## License

MIT. See [LICENSE](LICENSE).
