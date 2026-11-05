# EOS R8 progress

The maintainer's local copy is `~/Documents/GitHub/openshuttercounter`. Readings
now come from the Open Shutter Count app: its saved log has no serial number in
it. `Find Shutter Counter.command` still saves `.txt` logs in `mac/readings/`
(probes hold the camera's serial number, so they're ignored). The readings and the test photo cited
below were taken out of the repo and its history on 2026-11-04 for privacy;
the figures quoted here are what they showed.

## What the maintainer wants

The R8's mechanical and electronic shutter counts, each on its own, exact if
at all possible. Nothing is named until a test proves what it counts. A photo's
EXIF count was ruled out: it's mechanical and electronic together, and free
tools already read it.

## Status (2026-11-05)

Readable. The app reads 0xD167 off the event channel and shows
each count as the thousand it's in: word 3 mechanical, word 4 every photo,
electronic the difference. The maintainer accepted that (Q18).

As of the test's last photo (`_MG_1326.CR3`), and assuming word 3 rounds
up as word 4 does: mechanical 1,001 to 1,020, electronic 20,250 to 20,269,
total exactly 21,270. The window is narrow because word 3 stepped during the
20 Elec. 1st-curtain test photos. A plain read can't know that, so it shows
1,001 to 2,000.

Making the mechanical count exact would mean up to about 1,000 Elec.
1st-curtain photos to reach word 3's next step. The maintainer said not for
now (Q19).

How word 3 and word 4 were identified:
- Word 4 is every photo. A photo's total (mechanical plus electronic) is
  21,270. If word 4 (22000) counted one shutter only, it and word 3 (2000)
  would add to more than 22,000 under any rounding. Only rounding up turns
  21,270 into 22,000.
- Word 3 is mechanical. The alternative, electronic, would put about 20,000
  photos on the mechanical shutter, and the maintainer has shot mostly
  Electronic, using the shutter only when the camera was new.
- camerashuttercount's R8 page reads them the same way.

## What the R8 has shown

- USB mode must be Photo Import/Remote Control: MENU > Setup (wrench) > page 4
  > Choose USB connection app. In its other mode it enumerates as `04a9:32a6`
  with no still image interface, only a vendor one (0xff/0xf0, bulk
  0x84/0x05); PTP sent there got no answer.
- In the right mode: `04a9:330c`, "Canon Digital Camera". Interface 0 still
  image (bulk in 0x81, bulk out 0x02, interrupt 0x83); interface 1 vendor
  specific 0xff/0xf0 (bulk 0x84/0x05).
- GetDeviceInfo (probe of 2026-11-04 13:38): firmware
  3-1.5.0, vendor extension ID 6 (not Canon's 0x0b), 152 operations, 5
  properties.
  - Lists 0x9114 SetRemoteMode, 0x9115 SetEventMode, 0x9116 GetEvent and 0x9127
    RequestDevicePropValue.
  - Doesn't list 0x9052/0x9053, the factory channel. That's why `MonOpen` got
    no answer and hung the camera until a power cycle. The 90D (DIGIC 8) did
    the same in upstream issue #4.
  - Standard properties: 0xD402, 0xD407, 0xD406, 0xD303, 0x5001. None moved
    across 20 photos.
- GetDevicePropValue(0xD1AC): 0x2005 OperationNotSupported.
- `_MG_1326.CR3`, the test's last photo (camera clock 13:29:28, 4 s
  before the after probe; the camera's clock is UTC-7). Its main maker notes
  (CMT3) have no CameraInfo. Canon's timed metadata track (CTMD) does: record
  type 8 is a MakerNotes TIFF whose tag 0x000d CameraInfo is 4,608 bytes, and
  u32 LE at 0x0d29 is 21,270. camerashuttercount's result page shows the same
  21,270 for that photo and calls it the total, electronic included.
  - 0xD167 word 4 (22000) is that total rounded up to a thousand, and it didn't
    move while the total went from 21,243 to 21,270. Not proven until it steps
    to 23000 when the total passes 22,000.
  - So word 3 (2000) counts mechanical or electronic. If it rounds up like word
    4: mechanical 1,001 to 1,020 now (electronic 20,250 to 20,269), or
    electronic 1,001 to 1,007 (mechanical 20,263 to 20,269).
  - Nothing else in that CameraInfo matches either; one photo can't show a
    counter, so a diff across several photos would be needed.
- camerashuttercount's R8 page shows a sample as mechanical "≤26,000" and
  electronic "~6,000", in 1,000-shot steps: what word 3 as mechanical, rounded
  up, and word 4 minus word 3 would give. That's their reading, not proof.
- Event channel (2026-11-04_14-25-17): SetRemoteMode, SetEventMode and the
  exit sequence all answered OK. The first GetEvent gave 15,468 bytes, 324
  records; after the photos, 40,544 bytes, 909 records. The second GetEvent
  was empty both times.
  - 0xD167 before: 16, 1, 1000, 22000. After 20 Elec. 1st-curtain and 7
    Electronic photos: 16, 1, 2000, 22000. So word 3's counter crossed a
    multiple of 1,000 during those 27 photos and counts at least one kind of
    them; word 4 didn't move. Neither is identified yet.
  - Nothing in any record rose by exactly 20, 7 or 27, at any offset, width
    or byte order. So no exact counter in the event channel.
  - What changed: D113 CameraTime and D17C UTCTime (+46 s), D11B
    AvailableShots (3311 to 3287), D21A ShutterType (0 to 3: the camera was
    left on Electronic), D1DC StroboFiring, D238 (0x12 to 0x11), and three
    allowed-value lists. After the photos it also sent 27 ObjectAddedEx64
    (0xC1A7) records, one per photo, and 25 StorageInfoChanged.
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

1. Done (2026-11-04 15:39): `Read Shutter Count.command` read the R8 through
   0xD167: mechanical 1,001 to 2,000, electronic 19,001 to 20,999, all photos
   21,001 to 22,000. The raw bytes matched the event channel test.
2. Published (2026-11-05): the Open Shutter Count app, to [APP-PLAN.md](APP-PLAN.md),
   as `v0.5.0`. It replaces `Read Shutter Count.command`, which is gone. Before
   publishing, the updater was tested on the Mac against stand-in releases: it
   updated and reopened, refused a build changed after signing, one signed ad
   hoc, one with another bundle identifier and one older than its tag, and
   stopped at "9.9.9 is out" when it couldn't replace itself. The maintainer
   then read the R8 with it, and asked for simpler screens and an issue that
   fills in the log (APP-PLAN.md has the changes).
3. Open: an exact mechanical count (see Status); reporting this to
   libgphoto2; whether other R bodies lay 0xD167 out the same way.

## Loose ends

- The Terminal window closing after Return (`Find Shutter Counter.command`)
  hasn't been confirmed working.
- Other bodies that list the event channel (RF bodies, the 90D, newer EOS M)
  are now read the R8's way, labelled not fully tested. Their logs will show
  whether they lay 0xD167 out the same way. libgphoto2's EOS M100 dump has
  word 3 as 0xffffffff, which the decoder refuses, so the M100 ends as "can't
  read yet".
- No pull request has been opened, and nothing has been reported upstream.
- `mac/openshuttercounter` stays committed, so building the app on the Mac
  needs only Python (Q31).
- The maintainer is deleting the old branches `claude/loving-goldberg-zwh734`
  (it still has readings with the camera's serial) and
  `claude/quirky-bell-cl8670`, and detaching the GitHub fork if it shows one.
- On macOS, if another app (the Camera Shutter Count app, on the maintainer's
  Mac) is using the camera, ptpcamerad holds it and won't exit when stopped.
  The tool then names the app to quit; see `darwinBusyHint`.
