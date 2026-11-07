package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"

	"github.com/DigitalN/Open-Shutter-Count/tool/internal/app"
	"github.com/DigitalN/Open-Shutter-Count/tool/internal/logs"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/manufacturers"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/manufacturers/canon"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/ptp"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/trace"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/usb"
)

// registry lists the manufacturers this build can read.
//
// Adding another one is a matter of writing a package under
// pkg/manufacturers/ and naming it here; nothing else in the CLI is
// manufacturer aware. opts go to the Canon reader after the rest, such as
// canon.WithTestedList.
func registry(logger *zap.Logger, factoryOnly bool, opts ...canon.Option) *manufacturers.Registry {
	return manufacturers.NewRegistry(
		canon.New(append([]canon.Option{canon.WithLogger(logger), canon.WithFactoryOnly(factoryOnly)}, opts...)...),
	)
}

// testedList is the list --tested-list names, or the built-in one. It never
// fails the command: a list that can't be used gives the built-in one, and
// the report says why.
func testedList(cmd *cli.Command) (canon.TestedList, canon.TestedListReport) {
	return canon.LoadTestedList(cmd.String("tested-list"))
}

// logTestedList says which tested list was used, where the output doesn't:
// as a warning if a given one, or any of its entries, couldn't be used.
func logTestedList(ctx context.Context, report canon.TestedListReport) {
	logger := logs.LoggerFromContext(ctx)

	say := logger.Info
	if report.Reason != "" || len(report.Skipped) > 0 {
		say = logger.Warn
	}

	for _, step := range report.Steps() {
		say(step)
	}
}

// testedListOutput is which tested list a command used, in its JSON.
type testedListOutput struct {
	// Source is "given" or "built in".
	Source string `json:"source"`

	// Entries is how many entries counted.
	Entries int `json:"entries"`

	// Reason says why a given list wasn't used.
	Reason string `json:"reason,omitempty"`
}

func describeTestedList(report canon.TestedListReport) *testedListOutput {
	return &testedListOutput{Source: report.Source, Entries: report.Entries, Reason: report.Reason}
}

func configure(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	logger, encoding, err := logs.NewLogger(cmd.String("log-level"), cmd.String("log-format"))
	if err != nil {
		return ctx, err
	}

	return logs.StoreLoggerInContext(ctx, logger, encoding), nil
}

// Execute configures logging and then runs the command.
func Execute(f func(ctx context.Context, cmd *cli.Command) error) func(context.Context, *cli.Command) error {
	return func(ctx context.Context, cmd *cli.Command) error {
		var err error

		if ctx, err = configure(ctx, cmd); err != nil {
			return err
		}

		return f(ctx, cmd)
	}
}

// selectDevice finds the camera to talk to.
//
// With no --device the choice has to be unambiguous: guessing between two
// connected bodies would mean reporting one camera's shutter count under
// another's name.
func selectDevice(reg *manufacturers.Registry, selector string) (usb.DeviceInfo, error) {
	devices, err := usb.ListVendors(reg.VendorIDs()...)
	if err != nil {
		return usb.DeviceInfo{}, fmt.Errorf("enumerating USB devices: %w", err)
	}

	if selector != "" {
		for _, d := range devices {
			if matchesSelector(d, selector) {
				return d, nil
			}
		}

		return usb.DeviceInfo{}, fmt.Errorf("no connected camera matches %q", selector)
	}

	switch len(devices) {
	case 0:
		return usb.DeviceInfo{}, noDeviceError{}
	case 1:
		return devices[0], nil
	default:
		names := make([]string, len(devices))
		for i, d := range devices {
			names[i] = d.Path
		}

		return usb.DeviceInfo{}, fmt.Errorf(
			"%w: %d are connected; pick one with --device: %s",
			errSeveralCameras, len(devices), strings.Join(names, ", "),
		)
	}
}

// matchesSelector accepts a device path, a bus:address pair or a product ID.
func matchesSelector(d usb.DeviceInfo, selector string) bool {
	switch {
	case d.Path == selector:
		return true
	case fmt.Sprintf("%d:%d", d.Bus, d.Address) == selector:
		return true
	case fmt.Sprintf("%03d:%03d", d.Bus, d.Address) == selector:
		return true
	case fmt.Sprintf("%04x:%04x", d.VendorID, d.ProductID) == strings.ToLower(selector):
		return true
	default:
		return false
	}
}

// errSeveralCameras means more than one camera is connected and none was
// picked.
var errSeveralCameras = errors.New("more than one camera")

// noDeviceError carries the guidance shown when nothing is plugged in. It is a
// type rather than errors.New because the text is several sentences of help,
// which is exactly what a one line error message is not meant to be.
type noDeviceError struct{}

func (noDeviceError) Error() string { return "no camera found. " + noDeviceHint }

// hintedError is an error with what to do about it, which the app shows on
// its own. Error gives both.
type hintedError struct {
	err  error
	hint string
}

func (e hintedError) Error() string { return e.err.Error() + "\n\n" + e.hint }
func (e hintedError) Unwrap() error { return e.err }

func withHint(err error, hint string) error {
	if hint == "" {
		return err
	}

	return hintedError{err: err, hint: hint}
}

const noDeviceHint = `Check that:
  - the camera is switched on and connected directly, not through a hub
  - the cable is a data cable rather than charge only
  - built-in Wi-Fi is fully disabled: on many bodies it disables the USB port
  - there is a card in the camera
  - USB 2 ports are noticeably more reliable than USB 3 with these bodies

If your camera is from a manufacturer this build does not know yet, it will
not be listed at all. Adding one is welcome; see docs/CONTRIBUTING.md.`

const noStillImageHint = `The camera is connected, but its USB connection is in a mode that can't be
read, such as webcam. No commands were sent to it. Switch it to photo transfer,
then unplug it and plug it back in. On Canon mirrorless bodies that is the
Setup menu > Choose USB connection app > Photo Import/Remote Control (on the
EOS R8, page 4 of the Setup menu).`

// What openDevice opens the camera, stops macOS's camera service and asks what
// has the camera with. Tests swap them for stand-ins, since neither a camera
// nor ptpcamerad is there.
var (
	openCamera       = usb.Open
	stopCameraDaemon = usb.ReleaseSystemCameraDaemon
	cameraOwners     = usb.DeviceOwners
)

// openDevice claims the camera, working around whatever is holding it.
func openDevice(ctx context.Context, info usb.DeviceInfo) (usb.Device, error) {
	logger := logs.LoggerFromContext(ctx)

	device, err := openCamera(info)
	if err == nil {
		return device, nil
	}

	if errors.Is(err, usb.ErrNoStillImage) {
		return nil, withHint(err, noStillImageHint)
	}

	if !errors.Is(err, usb.ErrBusy) {
		return nil, err
	}

	// macOS hands every camera to ptpcamerad the moment it appears, and
	// launchd starts it again as soon as an app asks for a camera. So stop
	// it, then try to open the camera every few milliseconds until it comes
	// back. launchd brings it back for good once we are done.
	//
	// The first try asks it to exit (SIGTERM). macOS 26.6.2 can ignore that
	// and keep the camera (issue #34), so every try after the first kills it
	// (SIGKILL). A camera that opens after the first try never sees a kill.
	logger.Info("the camera is held by another process, stopping ptpcamerad and retrying")

	for attempt := 1; attempt <= darwinOpenAttempts; attempt++ {
		force := attempt > 1
		if attempt == 2 {
			logger.Info("ptpcamerad didn't let go, killing it and retrying")
		}

		if stopErr := stopCameraDaemon(ctx, force); stopErr != nil {
			// killall exits 1 when no ptpcamerad is running, which is no loss.
			logger.Debug("could not stop ptpcamerad", zap.Bool("force", force), zap.Error(stopErr))
		}

		device, err = openWhileFree(ctx, info)
		if err == nil {
			return device, nil
		}

		logger.Debug("the camera is still busy", zap.Int("attempt", attempt), zap.Error(err))

		if !errors.Is(err, usb.ErrBusy) {
			return nil, err
		}
	}

	owners, summary, ownersErr := cameraOwners(ctx, info)
	logger.Debug("what has the camera open",
		zap.Stringers("owners", owners), zap.String("ioreg", summary), zap.Error(ownersErr))

	return nil, withHint(err, darwinBusyHint(owners))
}

// openWhileFree tries to open the camera every darwinPollInterval for up to
// darwinRetryDelay, to catch it between ptpcamerad exiting and coming back.
// If an app is using the camera, launchd may start ptpcamerad again at once, or
// the app holds the camera itself, and then this can't help.
func openWhileFree(ctx context.Context, info usb.DeviceInfo) (usb.Device, error) {
	deadline := time.Now().Add(darwinRetryDelay)

	for {
		device, err := openCamera(info)
		if err == nil || !errors.Is(err, usb.ErrBusy) || time.Now().After(deadline) {
			return device, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(darwinPollInterval):
		}
	}
}

// How often opening a busy camera is retried on macOS after stopping
// ptpcamerad, for how long each time, and how often within that. The timings
// are variables so tests can shorten them.
const darwinOpenAttempts = 5

var (
	darwinRetryDelay   = 500 * time.Millisecond
	darwinPollInterval = 100 * time.Millisecond
)

// macOSServices are macOS's own processes that show up holding a camera.
// They aren't something to quit.
var macOSServices = map[string]bool{
	"ptpcamerad": true, // the camera service, serving apps that use cameras
	"accessoryd": true, // accessory approval; takes the R8's vendor interface
}

// darwinBusyHint says what to do about whatever has the camera.
func darwinBusyHint(owners []usb.Owner) string {
	var apps []string

	for _, o := range owners {
		if !macOSServices[o.Name] {
			apps = append(apps, o.Name)
		}
	}

	const quit = `Quit it completely: it may still be running with no window open, so check
the menu bar, or find it in Activity Monitor. Then unplug the camera, plug it
back in, and try again.`

	switch len(apps) {
	case 0:
		return `macOS's camera service (ptpcamerad) has the camera, and won't let go while
an app is using it: Photos, Image Capture, Preview, Lightroom, EOS Utility or
another camera app. ` + quit
	case 1:
		return apps[0] + " has the camera open. " + quit
	default:
		return strings.Join(apps, ", ") + " have the camera open. " + quit
	}
}

// session is an open conversation with a camera, plus the reader that knows
// how to ask it for a count.
type session struct {
	device   *ptp.Device
	info     usb.DeviceInfo
	reader   manufacturers.Reader
	recorder *trace.Recorder

	// steps says what was sent to the camera and how it answered.
	steps []string

	tracePath string
	close     func()
}

// openSession picks a camera, claims it and starts a PTP session.
//
// When --trace is given, every container exchanged from this point on is
// recorded, including the ones sent while working out what the camera is.
// The trace leaves out the camera's serial number, and anything else known
// to identify it or its owner.
//
// If a camera was picked but couldn't be opened, the session returned with
// the error says which camera it was, and has nothing to close. readerOpts go
// to the Canon reader.
func openSession(ctx context.Context, cmd *cli.Command, readerOpts ...canon.Option) (*session, error) {
	logger := logs.LoggerFromContext(ctx)
	reg := registry(logger, cmd.Bool("factory-only"), readerOpts...)

	info, err := selectDevice(reg, cmd.String("device"))
	if err != nil {
		return nil, err
	}

	failed := &session{info: info, close: func() {}}

	reader, err := reg.For(info)
	if err != nil {
		return failed, err
	}

	logger.Debug("opening device",
		zap.String("path", info.Path),
		zap.String("device", info.String()),
		zap.String("manufacturer", reader.Name()),
	)

	transport, err := openDevice(ctx, info)
	if err != nil {
		return failed, err
	}

	opts := []ptp.Option{ptp.WithLogger(logger)}

	s := &session{info: info, reader: reader, tracePath: cmd.String("trace")}

	if s.tracePath != "" {
		s.recorder = trace.NewRecorder(app.Version, canon.ScrubEvents, trace.ScrubProperties(canon.IdentifyingProperties...))
		s.recorder.Annotate(cmd.String("note"))
		opts = append(opts, ptp.WithRecorder(s.recorder))
	}

	s.device = ptp.New(transport, opts...)

	s.close = func() {
		if err := s.device.CloseSession(); err != nil {
			logger.Debug("closing the PTP session", zap.Error(err))
		}

		if err := transport.Close(); err != nil {
			logger.Debug("closing the USB device", zap.Error(err))
		}
	}

	// The identity is only fully known once the interface is open, since
	// some backends read the descriptors on the way in.
	s.info = s.device.Info()
	if s.recorder != nil {
		s.recorder.Describe(s.info)
	}

	logger.Debug("claimed the camera",
		zap.String("manufacturer", s.info.Manufacturer),
		zap.String("product", s.info.Product),
		zap.String("interfaces", s.info.Interfaces),
	)

	if err := openPTPSession(logger, s.device, transport); err != nil {
		s.steps = append(s.steps, "OpenSession: "+err.Error())

		// A trace of a camera that never answered is still worth having.
		if traceErr := s.saveTrace(ctx); traceErr != nil {
			logger.Debug("saving the trace", zap.Error(traceErr))
		}

		s.close()

		failed.steps = s.steps

		return failed, err
	}

	s.steps = append(s.steps, "OpenSession: OK")

	return s, nil
}

// deviceInfo asks the camera what it is and what it supports.
func (s *session) deviceInfo() (ptp.DeviceInfo, error) {
	info, _, err := s.device.GetDeviceInfo()
	if err != nil {
		s.steps = append(s.steps, "GetDeviceInfo: "+err.Error())

		return info, err
	}

	s.steps = append(s.steps, fmt.Sprintf("GetDeviceInfo: OK, %s, firmware %s, %d operations",
		info.Model, info.DeviceVersion, len(info.Operations)))

	return info, nil
}

// openPTPSession opens the PTP session, resetting the camera and trying once
// more if it doesn't answer. A camera taken over from another process can
// still be in that process's session or transaction and ignore a new one.
func openPTPSession(logger *zap.Logger, device *ptp.Device, transport usb.Device) error {
	err := device.OpenSession()
	if err == nil || !errors.Is(err, usb.ErrTimeout) {
		return err
	}

	logger.Warn("the camera didn't answer; resetting it and trying again", zap.Error(err))

	if resetter, ok := transport.(usb.Resetter); ok {
		if resetErr := resetter.ResetPTP(); resetErr != nil {
			logger.Debug("PTP device reset", zap.Error(resetErr))
		}
	}

	if clearErr := device.ClearHalt(); clearErr != nil {
		logger.Debug("clearing the endpoints", zap.Error(clearErr))
	}

	if err = device.OpenSession(); err != nil {
		return fmt.Errorf("the camera didn't answer, even after a reset: %w", err)
	}

	return nil
}

// saveTrace writes the recording out, if one was asked for.
func (s *session) saveTrace(ctx context.Context) error {
	if s.recorder == nil {
		return nil
	}

	if err := s.recorder.WriteFile(s.tracePath); err != nil {
		return err
	}

	logs.LoggerFromContext(ctx).Info("trace written",
		zap.String("path", s.tracePath),
		zap.Int("entries", len(s.recorder.File().Entries)),
	)

	return nil
}

// humanInt groups a number in threes, so a shutter count reads as 26,374.
func humanInt(v uint32) string {
	digits := strconv.FormatUint(uint64(v), 10)

	var out strings.Builder

	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}

		out.WriteRune(r)
	}

	return out.String()
}
