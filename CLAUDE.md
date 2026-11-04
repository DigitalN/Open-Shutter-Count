# openshuttercounter (DigitalN fork)

A fork of mvisonneau/openshuttercounter, which the original author has stopped
working on. The goal here is reading the shutter count of the maintainer's Canon EOS R8
on their Mac (MacBookPro18,1, M1 Pro, macOS 27.2) without using Terminal.

Read [docs/R8-PROGRESS.md](docs/R8-PROGRESS.md) first: what's been done, what
the R8 did, and what's next.

## How testing works

- Claude can't reach the Mac or the camera. The maintainer runs a launcher in `mac/`
  by double-clicking, and commits and pushes what it saved in `mac/readings/`.
  Readings from before that are in `Logs/`.
- After any Go change, rebuild the Mac binary with `bash mac/build.sh` (works
  on Linux; reproducible; the Go linker ad-hoc signs it) and commit
  `mac/openshuttercounter` with the change. The maintainer then pulls and reruns.
- Checks before pushing: `go vet ./...` for linux, darwin and windows;
  `go test ./...`; and the repo's linter for each OS:
  `go build -o .bin/golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint`
  then `GOOS=<os> .bin/golangci-lint run ./...`, then `rm -rf .bin`.

## Camera safety

- Never send a command that writes to the camera, and never sweep opcodes.
  `pkg/manufacturers/canon` refuses the known write commands; keep it that way.
- The R8 hung on `MonOpen` (the factory channel) and needed a power cycle. It's
  in `noFactoryChannel` so the tool won't send it again.
- The R8 doesn't list 0x9052/0x9053 in GetDeviceInfo. Don't send an opcode a
  camera doesn't list.
- Standard PTP reads are fine. The maintainer approved (2026-11-04) Canon's
  tethering commands for reading the event channel, and only these:
  SetRemoteMode 0x9114 (params 1, then 0 and 1 on the way out), SetEventMode
  0x9115 (1, then 0), GetEvent 0x9116, and RequestDevicePropValue 0x9127 for
  0xD167 only. They're in `pkg/manufacturers/canon/events.go`, sent by
  `probe --events` and by a read of a body in `eventModels` (the R8). Ask before sending anything else, including 0x9108
  GetDeviceInfoEx.
- No guessing in what the tool prints: a value isn't named (mechanical,
  electronic, total) until a test proves what it counts.
- When the camera stops answering, the maintainer switches it off and on (battery out
  if it won't switch off).
