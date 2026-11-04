# openshuttercounter

Read your camera's shutter count over USB. Free and open source.

- **Read off the camera itself.** A photo only carries the total. The
  mechanical count is kept inside the camera, and this reads it from there.
- **Read-only.** It never changes a setting, a file or a count on the camera.
- **Local.** Nothing to install, no account, and nothing leaves your computer.

## Cameras

| Camera | What it reads |
|---|---|
| Canon EOS R8 | Mechanical, electronic and total, each to within a thousand: the camera only keeps them that way |
| Canon EOS 5D Mark IV | Exact shutter and mirror counts |
| Other cameras | Try it. If yours can't be read yet, [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) says how to help |

## Use it on a Mac

1. Click **Code > Download ZIP** on this page, and open the ZIP. Needs an
   Apple silicon Mac.
2. Double-click **Read Shutter Count.command**. It walks you through setting
   up the camera first.
3. The first time, macOS may refuse to open it. Go to **System Settings >
   Privacy & Security**, click **Open Anyway**, and double-click it again.

Windows and Linux: see [docs/USAGE.md](docs/USAGE.md).

## How it works

Canon keeps the counts in different places on different bodies:
[docs/PROTOCOL.md](docs/PROTOCOL.md) covers the factory channel the 5D Mark IV
uses, and [docs/R8-PROGRESS.md](docs/R8-PROGRESS.md) covers the event channel the
R8 uses.

## Credit

A fork of [mvisonneau/openshuttercounter](https://github.com/mvisonneau/openshuttercounter),
which worked out Canon's factory channel. The work it built on is credited in
[docs/PROTOCOL.md](docs/PROTOCOL.md#prior-work).

## License

MIT. See [LICENSE](LICENSE).
