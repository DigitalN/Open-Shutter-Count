package cmd

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/urfave/cli/v3"

	"github.com/DigitalN/openshuttercounter/pkg/manufacturers"
	"github.com/DigitalN/openshuttercounter/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/pkg/usb"
)

// ErrReported means the command failed and has already said why, in its JSON
// output. Nothing more should be printed; the exit status should say it
// failed.
var ErrReported = errors.New("the failure was reported in the output")

// readOutput is what `read --json` prints, failed or not. The camera's serial
// number isn't in it.
type readOutput struct {
	Device   *deviceOutput   `json:"device,omitempty"`
	Camera   *cameraOutput   `json:"camera,omitempty"`
	Reading  *readingOutput  `json:"reading,omitempty"`
	Counters []counterOutput `json:"counters,omitempty"`

	// Steps says what was sent to the camera and what it answered.
	Steps []string `json:"steps,omitempty"`

	Error *errorOutput `json:"error,omitempty"`
}

type deviceOutput struct {
	VendorID  string `json:"vendor_id"`
	ProductID string `json:"product_id"`
	Product   string `json:"product,omitempty"`
	Path      string `json:"path"`
}

// cameraOutput is what the camera said about itself in GetDeviceInfo.
type cameraOutput struct {
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
	Firmware     string   `json:"firmware,omitempty"`
	Operations   []string `json:"operations"`
}

type readingOutput struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Confirmed    bool   `json:"confirmed"`
	Method       string `json:"method"`
	RatedLife    uint32 `json:"rated_life,omitempty"`
	Note         string `json:"note,omitempty"`
}

type counterOutput struct {
	Label    string `json:"label"`
	Location string `json:"location,omitempty"`
	Value    uint32 `json:"value"`
	Range    bool   `json:"range,omitempty"`
	Min      uint32 `json:"min,omitempty"`
	Raw      string `json:"raw,omitempty"`
	Error    string `json:"error,omitempty"`
}

// errorOutput says why a read failed. Kind is one of the errorKinds.
type errorOutput struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// What kind of failure a read was, for the app to say what to do about it.
const (
	kindNoCamera       = "no_camera"
	kindSeveralCameras = "several_cameras"
	kindWrongUSBMode   = "wrong_usb_mode"
	kindBusy           = "busy"
	kindNotSupported   = "not_supported"
	kindNoAnswer       = "no_answer"
	kindFailed         = "failed"
)

// Read reports the actuation counters of the connected camera.
func Read(ctx context.Context, cmd *cli.Command) error {
	session, err := openSession(ctx, cmd)
	if err != nil {
		out := readOutput{}
		if session != nil {
			out.Device, out.Steps = describeUSB(session.info), session.steps
		}

		return fail(cmd, out, err)
	}

	defer session.close()

	camera, readErr := session.deviceInfo()

	var reading manufacturers.Reading
	if readErr == nil {
		reading, readErr = session.reader.Read(ctx, session.device, session.info, camera)
	}

	// The trace is worth keeping even when the read failed. A trace of a
	// camera that did not answer is exactly what makes a bug report useful.
	if err := session.saveTrace(ctx); err != nil {
		return err
	}

	out := buildOutput(session.info, camera, reading, cmd.Bool("raw"))
	out.Steps = slices.Concat(session.steps, reading.Steps)

	if readErr != nil {
		out.Reading, out.Counters = nil, nil

		return fail(cmd, out, readErr)
	}

	return render(cmd, out)
}

// fail reports a failed read. With --json it goes in the output, which then
// says what kind of failure it was.
func fail(cmd *cli.Command, out readOutput, err error) error {
	if !cmd.Bool("json") {
		return err
	}

	out.Error = &errorOutput{Kind: errorKind(err), Message: err.Error()}

	if hinted, ok := errors.AsType[hintedError](err); ok {
		out.Error.Message, out.Error.Hint = hinted.err.Error(), hinted.hint
	}

	if out.Error.Kind == kindNoCamera {
		out.Error.Message, out.Error.Hint = "no camera found", noDeviceHint
	}

	if err := printJSON(out); err != nil {
		return err
	}

	return ErrReported
}

func errorKind(err error) string {
	switch {
	case errors.As(err, new(noDeviceError)):
		return kindNoCamera
	case errors.Is(err, errSeveralCameras):
		return kindSeveralCameras
	case errors.Is(err, usb.ErrNoStillImage):
		return kindWrongUSBMode
	case errors.Is(err, usb.ErrBusy):
		return kindBusy
	case errors.Is(err, manufacturers.ErrNotSupported):
		return kindNotSupported
	case errors.Is(err, usb.ErrTimeout):
		return kindNoAnswer
	default:
		return kindFailed
	}
}

func describeUSB(info usb.DeviceInfo) *deviceOutput {
	return &deviceOutput{
		VendorID:  fmt.Sprintf("%04x", info.VendorID),
		ProductID: fmt.Sprintf("%04x", info.ProductID),
		Product:   info.Product,
		Path:      info.Path,
	}
}

func buildOutput(info usb.DeviceInfo, camera ptp.DeviceInfo, reading manufacturers.Reading, withRaw bool) readOutput {
	out := readOutput{
		Device: describeUSB(info),
		Reading: &readingOutput{
			Manufacturer: reading.Manufacturer,
			Model:        reading.Model,
			Confirmed:    reading.Confirmed,
			Method:       reading.Method,
			RatedLife:    reading.RatedLife,
			Note:         reading.Note,
		},
		Counters: make([]counterOutput, 0, len(reading.Counters)),
	}

	if camera.Model != "" || len(camera.Operations) > 0 {
		out.Camera = &cameraOutput{
			Manufacturer: camera.Manufacturer,
			Model:        camera.Model,
			Firmware:     camera.DeviceVersion,
			Operations:   make([]string, 0, len(camera.Operations)),
		}

		for _, op := range camera.Operations {
			out.Camera.Operations = append(out.Camera.Operations, fmt.Sprintf("%#04x", op))
		}
	}

	for _, c := range reading.Counters {
		counter := counterOutput{
			Label:    c.Label,
			Location: c.Location,
			Value:    c.Value,
			Range:    c.Range,
			Min:      c.Min,
		}

		if withRaw && len(c.Raw) > 0 {
			counter.Raw = hex.EncodeToString(c.Raw)
		}

		if c.Err != nil {
			counter.Error = c.Err.Error()
		}

		out.Counters = append(out.Counters, counter)
	}

	return out
}

func printJSON(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	return encoder.Encode(v)
}

func render(cmd *cli.Command, out readOutput) error {
	if cmd.Bool("json") {
		return printJSON(out)
	}

	fmt.Printf("\n  %s %s  (USB %s:%s)\n\n",
		out.Reading.Manufacturer, out.Reading.Model, out.Device.VendorID, out.Device.ProductID)

	for _, c := range out.Counters {
		value := humanInt(c.Value)
		if c.Range {
			value = humanInt(c.Min) + " to " + humanInt(c.Value)
		}

		if c.Error != "" {
			value = "unavailable"
		}

		where := ""
		if c.Location != "" {
			where = "   (" + c.Location + ")"
		}

		fmt.Printf("  %-22s %16s%s\n", c.Label, value, where)

		if c.Raw != "" {
			fmt.Printf("  %-22s raw: %s\n", "", spacedHex(c.Raw))
		}

		if c.Error != "" {
			fmt.Printf("  %-22s %s\n", "", c.Error)
		}
	}

	if len(out.Counters) > 0 && out.Reading.RatedLife > 0 && out.Counters[0].Error == "" && !out.Counters[0].Range {
		used := float64(out.Counters[0].Value) / float64(out.Reading.RatedLife) * 100

		fmt.Printf("\n  Rated life %s actuations - %.1f%% used\n", humanInt(out.Reading.RatedLife), used)
	}

	if out.Reading.Note != "" {
		fmt.Printf("\n%s\n", out.Reading.Note)
	}

	if !out.Reading.Confirmed {
		fmt.Printf(untestedNotice, out.Reading.Model)
	}

	fmt.Println()

	return nil
}

const untestedNotice = `
  Not fully tested on the %s. If these numbers look wrong, please send
  a trace, and say how many photos the camera has taken if you know:

    openshuttercounter --trace mycamera.json

  docs/CONTRIBUTING.md has the details.
`

// spacedHex makes a raw reply readable.
func spacedHex(s string) string {
	out := make([]byte, 0, len(s)+len(s)/2)

	for i := 0; i < len(s); i += 2 {
		if i > 0 {
			out = append(out, ' ')
		}

		out = append(out, s[i], s[i+1])
	}

	return string(out)
}
