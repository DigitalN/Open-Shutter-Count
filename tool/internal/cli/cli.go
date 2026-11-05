package cli

import (
	"github.com/urfave/cli/v3"

	"github.com/DigitalN/openshuttercounter/tool/internal/app"
	"github.com/DigitalN/openshuttercounter/tool/internal/cmd"
)

// NewApp configures the CLI application.
func NewApp() *cli.Command {
	return &cli.Command{
		Name:                  app.Name,
		Version:               app.Version,
		Usage:                 "read the shutter actuation count of a camera",
		HideHelpCommand:       true,
		EnableShellCompletion: true,

		Description: `Reads the shutter actuation counters straight off a Canon camera over USB.
Every body is first asked the standard PTP counter property, which older ones
answer. Then, by what the camera lists: Canon's factory channel on EF DSLRs
that list it (the EOS 5D Mark IV's way), or Canon's event channel, the way EOS
Utility talks to the camera, on RF bodies and others that list it (the EOS
R8's way). Anything else can't be read yet, and is sent nothing more.

Only the EOS R8 and the EOS 5D Mark IV are tested. A reading from any other
body says it's not fully tested.

Running it with no command reads the connected camera.

Running with --trace produces a file that is enough for somebody else to work
out a camera it can't read, or check one it can. It leaves out the camera's
serial number. See docs/CONTRIBUTING.md.

This tool only ever reads. It will not send any command that writes to the
camera, and refuses to build one; see docs/PROTOCOL.md for why that matters.`,

		Flags: append([]cli.Flag{
			&cli.StringFlag{
				Name:    "log-level",
				Sources: cli.EnvVars("OPENSHUTTERCOUNTER_LOG_LEVEL"),
				Usage:   "log `level` (debug,info,warn,error,fatal,panic)",
				Value:   "info",
			},
			&cli.StringFlag{
				Name:    "log-format",
				Sources: cli.EnvVars("OPENSHUTTERCOUNTER_LOG_FORMAT"),
				Usage:   "log `format` (json,text)",
				Value:   "text",
			},
		}, readFlags...),

		Action: cmd.Execute(cmd.Read),

		Commands: []*cli.Command{
			{
				Name:  "probe",
				Usage: "save everything the camera reports through standard PTP read requests",
				Description: `Asks the camera what it supports (GetDeviceInfo), then reads every property
it lists with GetDevicePropDesc or GetDevicePropValue, and saves it all to a
file. Without --events it sends nothing else: no vendor or factory commands.

With --events, on a Canon body, it also reads Canon's event channel the way
EOS Utility and libgphoto2 do: SetRemoteMode(1), SetEventMode(1), GetEvent
until the camera has nothing more to report, and RequestDevicePropValue(0xD167)
if that property wasn't reported. Then SetRemoteMode(0), SetRemoteMode(1) and
SetEventMode(0), as libgphoto2 does when it stops tethering. Each is sent only
if the camera lists it.

Two probes taken an exact number of photos apart, compared with 'compare',
show whether anything is the shutter count.`,
				Action: cmd.Execute(cmd.Probe),
				Flags: []cli.Flag{
					deviceFlag,
					traceFlag,
					noteFlag,
					&cli.BoolFlag{
						Name:  "events",
						Usage: "also read a Canon body's event channel (sends Canon's tethering commands)",
					},
					&cli.StringFlag{
						Name:     "out",
						Aliases:  []string{"o"},
						Usage:    "the `file` to save the probe in",
						Required: true,
					},
				},
			},
			{
				Name:      "compare",
				Usage:     "show what changed between two probes, and what went up by the number of photos taken",
				ArgsUsage: "<before.json> <after.json>",
				Action:    cmd.Execute(cmd.Compare),
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "frames",
						Usage: "how many `photos` were taken with the shutter moving (on an EOS R8, Elec. 1st-curtain)",
						Value: 20,
					},
					&cli.IntFlag{
						Name:  "electronic",
						Usage: "how many `photos` were taken with the electronic shutter",
					},
				},
			},
			{
				Name:   "read",
				Usage:  "read the actuation counters of the connected camera",
				Action: cmd.Execute(cmd.Read),
				Flags:  readFlags,
			},
			{
				Name:   "list",
				Usage:  "list the cameras currently connected",
				Action: cmd.Execute(cmd.List),
				Flags: []cli.Flag{
					allDevicesFlag,
					jsonFlag,
				},
			},
			{
				Name:   "models",
				Usage:  "list the cameras this build knows how to read",
				Action: cmd.Execute(cmd.Models),
				Flags: []cli.Flag{
					jsonFlag,
				},
			},
			{
				Name:      "decode",
				Usage:     "print the conversation held in a trace file",
				ArgsUsage: "<trace.json>",
				Description: `Prints the PTP conversation a trace file contains, with each manufacturer's
commands decoded.

Traces written by openshuttercounter itself are the usual input. USB captures
in pcap or pcapng format are read too, for anyone who already has one.

Comparing two traces taken a known number of frames apart is how a camera gets
confirmed, and how an unrecognised one gets worked out.`,
				Action: cmd.Execute(cmd.Decode),
				Flags: []cli.Flag{
					asciiFlag,
					onlyFlag,
				},
			},
		},
	}
}
