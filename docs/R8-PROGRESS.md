# EOS R8 progress

Work is on branch `claude/happy-knuth-9mtrso`. The maintainer's local copy is
`~/Documents/GitHub/openshuttercounter`. Readings they've sent are in `Logs/`.

## What the maintainer wants

The R8's mechanical and electronic shutter counts, each on its own, exact if
at all possible. Nothing is named until a test proves what it counts. A photo's
EXIF count was ruled out: it's mechanical and electronic together, and free
tools already read it.

## Status (2026-11-04)

Built, not yet run on the camera: `probe --events` reads Canon's event
channel, and `Find Shutter Counter.command` compares two such reads around
20 Elec. 1st-curtain and 7 Electronic photos. Next step is the maintainer
running it.

## What the R8 has shown

- USB mode must be Photo Import/Remote Control: MENU > Setup (wrench) > page 4
  > Choose USB connection app. In its other mode it enumerates as `04a9:32a6`
  with no still image interface, only a vendor one (0xff/0xf0, bulk
  0x84/0x05); PTP sent there got no answer.
- In the right mode: `04a9:330c`, "Canon Digital Camera". Interface 0 still
  image (bulk in 0x81, bulk out 0x02, interrupt 0x83); interface 1 vendor
  specific 0xff/0xf0 (bulk 0x84/0x05).
- GetDeviceInfo (`Logs/2026-11-04_13-38-11-probe-before.json`): firmware
  3-1.5.0, vendor extension ID 6 (not Canon's 0x0b), 152 operations, 5
  properties.
  - Lists 0x9114 SetRemoteMode, 0x9115 SetEventMode, 0x9116 GetEvent and 0x9127
    RequestDevicePropValue.
  - Doesn't list 0x9052/0x9053, the factory channel. That's why `MonOpen` got
    no answer and hung the camera until a power cycle. The 90D (DIGIC 8) did
    the same in upstream issue #4.
  - Standard properties: 0xD402, 0xD407, 0xD406, 0xD303, 0x5001. None moved
    across 20 photos (`Logs/2026-11-04_13-38-11-probe.txt`).
- GetDevicePropValue(0xD1AC): 0x2005 OperationNotSupported.
- Shutter modes: Elec. 1st-curtain and Electronic only.
- Earlier problems were a charge-only USB-C cable, and macOS's accessory
  approval on Apple silicon laptops (a declined device charges but can't be
  seen).

## What's known from elsewhere

- R-series bodies report their count in steps of 1,000. The ShutterCount
  developer said the EOS R gives "a value measured in thousands", and
  camerashuttercount's R8 page shows "≤26,000 ... Reported in 1,000-shot
  steps". So a counter that only moves every 1,000 shots won't show up in a
  20-photo diff.
- libgphoto2's EOS R and M50 Mark II dumps (`camlibs/ptp2/cameras/`) carry
  property 0xD167, EOS_ShutterReleaseCounter, in the event channel's first
  batch: 16 bytes, four LE u32 words. EOS R: 16, 1, 4000, 35000 and
  16, 1, 5000, 86000. M50 Mark II: 16, 1, 1000, 1000. What words 3 and 4
  count is not established.
- exiftool reads `ShutterCount` from CameraInfo offset 0x0d29 for the R6 Mark
  II, R8 and R50, noted as electronic plus mechanical.
- Canon service reads exact counts. No public tool was found that reads an
  exact mechanical count from an R8 over USB.

## Next

1. Maintainer runs `Find Shutter Counter.command` and sends the
   `*-probe*` files.
2. If any event record field went up by exactly 20 or 7: that's an exact
   counter. Confirm with a second run at different numbers (e.g. 13 and 4),
   then make `Read Shutter Count` show it.
3. If not: work out which 0xD167 word counts what, without guessing or
   wearing the shutter. One idea, not yet put to the maintainer: Electronic
   photos until a word steps up (at most 1,000, no shutter wear). The word
   that doesn't move counts the shutter; whether the other is electronic or
   total needs more.

## Loose ends

- The Terminal window closing after Return hasn't been confirmed working.
- No pull request has been opened, and nothing has been reported upstream.
