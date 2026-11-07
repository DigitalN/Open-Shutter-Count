package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/DigitalN/openshuttercounter/tool/internal/logs"
	"github.com/DigitalN/openshuttercounter/tool/pkg/manufacturers"
	"github.com/DigitalN/openshuttercounter/tool/pkg/manufacturers/canon"
	"github.com/DigitalN/openshuttercounter/tool/pkg/usb"
)

// List reports the cameras currently attached.
func List(ctx context.Context, cmd *cli.Command) error {
	reg := registry(logs.LoggerFromContext(ctx), false)

	var (
		devices []usb.DeviceInfo
		err     error
	)

	if cmd.Bool("all") {
		devices, err = usb.List()
	} else {
		devices, err = usb.ListVendors(reg.VendorIDs()...)
	}

	if err != nil {
		return fmt.Errorf("enumerating USB devices: %w", err)
	}

	if cmd.Bool("json") {
		return printJSON(listOutput(reg, devices))
	}

	if len(devices) == 0 {
		fmt.Println("\nNo camera found.")
		fmt.Println()
		fmt.Println(noDeviceHint)

		return nil
	}

	fmt.Println()

	for _, d := range devices {
		fmt.Printf("  %04x:%04x  %-32s %s\n", d.VendorID, d.ProductID, describeDevice(reg, d), d.Path)
	}

	fmt.Println()

	return nil
}

// listedDevice is one device in `list --json`, named as describeDevice names
// it, with its serial number left out.
type listedDevice struct {
	usb.DeviceInfo

	Name string `json:"name"`
}

func listOutput(reg *manufacturers.Registry, devices []usb.DeviceInfo) []listedDevice {
	out := make([]listedDevice, 0, len(devices))

	for _, d := range devices {
		name := describeDevice(reg, d)
		d.Serial = ""
		out = append(out, listedDevice{DeviceInfo: d, Name: name})
	}

	return out
}

// describeDevice names a device as helpfully as it can: what the camera calls
// itself, else what the registry knows it as, else an honest shrug.
func describeDevice(reg *manufacturers.Registry, d usb.DeviceInfo) string {
	if d.Product != "" {
		return d.Product
	}

	reader, err := reg.For(d)
	if err != nil {
		return "unrecognised device"
	}

	for _, m := range reader.Models() {
		if m.ProductID == d.ProductID {
			return reader.Name() + " " + m.Name
		}
	}

	return reader.Name() + " camera"
}

// Models lists the cameras this build knows how to read. Which say confirmed
// is up to the tested list: the one --tested-list names, or the one built in.
func Models(ctx context.Context, cmd *cli.Command) error {
	tested, listed := testedList(cmd)
	reg := registry(logs.LoggerFromContext(ctx), false, canon.WithTestedList(tested))

	type manufacturerOutput struct {
		Manufacturer string                `json:"manufacturer"`
		VendorID     string                `json:"vendor_id"`
		Models       []manufacturers.Model `json:"models"`
	}

	out := make([]manufacturerOutput, 0, len(reg.Readers()))
	for _, reader := range reg.Readers() {
		out = append(out, manufacturerOutput{
			Manufacturer: reader.Name(),
			VendorID:     fmt.Sprintf("%04x", reader.VendorID()),
			Models:       reader.Models(),
		})
	}

	if cmd.Bool("json") {
		// The JSON stays a list of manufacturers; the log says which tested
		// list was used.
		logTestedList(ctx, listed)

		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")

		return encoder.Encode(out)
	}

	for _, m := range out {
		fmt.Printf("\n%s  (USB vendor %s)\n\n", m.Manufacturer, m.VendorID)

		for _, model := range m.Models {
			status := "not confirmed by anyone yet"
			if model.Confirmed {
				status = "confirmed"
			}

			fmt.Printf("  %-20s USB %s:%04x  (%s)\n", model.Name, m.VendorID, model.ProductID, status)

			for _, c := range model.Counters {
				fmt.Printf("      %-24s %s\n", c.Location, c.Label)
			}
		}
	}

	if steps := listed.Steps(); len(steps) > 0 {
		fmt.Println()

		for _, step := range steps {
			fmt.Println("  " + strings.ToUpper(step[:1]) + step[1:])
		}
	}

	fmt.Print(modelsFooter)

	return nil
}

const modelsFooter = `
  Older bodies need no entry here: they still answer the standard PTP shutter
  counter property, which is tried first on every camera. Other bodies are read
  the way the 5D Mark IV or the R8 is, by what they list, and the reading says
  it's not fully tested unless the tested list has the body, read that way.

  Whether it worked or not, a trace makes the answer useful to everyone else:

    openshuttercounter --trace mycamera.json

  docs/CONTRIBUTING.md explains what to do with it.

`
