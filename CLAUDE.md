# openshuttercounter (DigitalN fork)

A fork of mvisonneau/openshuttercounter, which the original author has stopped
working on. The goal here is reading the shutter count of the maintainer's Canon EOS R8
on their Mac (MacBookPro18,1, M1 Pro, macOS 27.2) without using Terminal.

Read [docs/R8-PROGRESS.md](docs/R8-PROGRESS.md) first: what's been done, what
the R8 did, and what's next.

## How testing works

- Claude can't reach the Mac or the camera. The maintainer runs a launcher in `mac/`
  by double-clicking, and uploads what it saved in `mac/readings/`.
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
- Standard PTP reads are fine. Ask the maintainer before sending anything else to
  the camera, including Canon's tethering commands (0x9114/0x9115/0x9116).
- When the camera stops answering, the maintainer switches it off and on (battery out
  if it won't switch off).
