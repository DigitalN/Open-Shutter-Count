package cmd

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"

	"github.com/DigitalN/openshuttercounter/internal/app"
	"github.com/DigitalN/openshuttercounter/internal/logs"
	"github.com/DigitalN/openshuttercounter/pkg/manufacturers"
	"github.com/DigitalN/openshuttercounter/pkg/manufacturers/canon"
	"github.com/DigitalN/openshuttercounter/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/pkg/trace"
	"github.com/DigitalN/openshuttercounter/pkg/usb"
)

// registry lists the manufacturers this build can read.
//
// Adding another one is a matter of writing a package under
// pkg/manufacturers/ and naming it here; nothing else in the CLI is
// manufacturer aware.
func registry(logger *zap.Logger, factoryOnly bool) *manufacturers.Registry {
	return manufacturers.NewRegistry(
		canon.New(canon.WithLogger(logger), canon.WithFactoryOnly(factoryOnly)),
	)
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
			"%d cameras are connected; pick one with --device: %s",
			len(devices), strings.Join(names, ", "),
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

// noDeviceError carries the guidance shown when nothing is plugged in. It is a
// type rather than errors.New because the text is several sentences of help,
// which is exactly what a one line error message is not meant to be.
type noDeviceError struct{}

func (noDeviceError) Error() string { return noDeviceHint }

const noDeviceHint = `no camera found. Check that:
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

// openDevice claims the camera, working around whatever is holding it.
func openDevice(ctx context.Context, info usb.DeviceInfo) (usb.Device, error) {
	logger := logs.LoggerFromContext(ctx)

	device, err := usb.Open(info)
	if err == nil {
		return device, nil
	}

	if errors.Is(err, usb.ErrNoStillImage) {
		return nil, fmt.Errorf("%w\n\n%s", err, noStillImageHint)
	}

	if !errors.Is(err, usb.ErrBusy) {
		return nil, err
	}

	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("%w\n%s", err, busyHint())
	}

	// macOS hands every camera to ptpcamerad the moment it appears, and
	// launchd starts it again as soon as an app asks for a camera. So stop
	// it, then try to open the camera every few milliseconds until it comes
	// back. launchd brings it back for good once we are done.
	logger.Info("the camera is held by another process, stopping ptpcamerad and retrying")

	for attempt := 1; attempt <= darwinOpenAttempts; attempt++ {
		if stopErr := usb.ReleaseSystemCameraDaemon(ctx); stopErr != nil {
			logger.Debug("could not stop ptpcamerad", zap.Error(stopErr))
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

	owners, listing, ownersErr := usb.DeviceOwners(ctx, info)
	logger.Debug("what has the camera open",
		zap.Stringers("owners", owners), zap.String("ioreg", listing), zap.Error(ownersErr))

	return nil, fmt.Errorf("%w\n\n%s", err, darwinBusyHint(owners))
}

// openWhileFree tries to open the camera every darwinPollInterval for up to
// darwinRetryDelay, to catch it between ptpcamerad exiting and coming back.
func openWhileFree(ctx context.Context, info usb.DeviceInfo) (usb.Device, error) {
	deadline := time.Now().Add(darwinRetryDelay)

	for {
		device, err := usb.Open(info)
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
// ptpcamerad, for how long each time, and how often within that.
const (
	darwinOpenAttempts = 5
	darwinRetryDelay   = 500 * time.Millisecond
	darwinPollInterval = 20 * time.Millisecond
)

// darwinBusyHint says what to do about whatever has the camera.
func darwinBusyHint(owners []usb.Owner) string {
	var apps []string

	for _, o := range owners {
		if o.Name != "ptpcamerad" {
			apps = append(apps, o.Name)
		}
	}

	if len(apps) > 0 {
		return strings.Join(apps, " and ") + ` has the camera. Quit it, unplug the camera, plug it back
in, and try again.`
	}

	return `macOS's camera service (ptpcamerad) keeps taking the camera back, which
usually means an app is asking for it: Photos, Image Capture, Preview,
Lightroom, EOS Utility or another camera app. Quit them, unplug the camera,
plug it back in, and try again.`
}

func busyHint() string {
	switch runtime.GOOS {
	case "linux":
		return `On desktop Linux gvfs claims cameras on sight. Release it with:
  gio mount -u gphoto2://
  systemctl --user mask gvfs-gphoto2-volume-monitor`
	case "windows":
		return "Close any other software that may have opened the camera."
	default:
		return ""
	}
}

// session is an open conversation with a camera, plus the reader that knows
// how to ask it for a count.
type session struct {
	device   *ptp.Device
	info     usb.DeviceInfo
	reader   manufacturers.Reader
	recorder *trace.Recorder

	tracePath string
	close     func()
}

// openSession picks a camera, claims it and starts a PTP session.
//
// When --trace is given, every container exchanged from this point on is
// recorded, including the ones sent while working out what the camera is.
func openSession(ctx context.Context, cmd *cli.Command) (*session, error) {
	logger := logs.LoggerFromContext(ctx)
	reg := registry(logger, cmd.Bool("factory-only"))

	info, err := selectDevice(reg, cmd.String("device"))
	if err != nil {
		return nil, err
	}

	reader, err := reg.For(info)
	if err != nil {
		return nil, err
	}

	logger.Debug("opening device",
		zap.String("path", info.Path),
		zap.String("device", info.String()),
		zap.String("manufacturer", reader.Name()),
	)

	transport, err := openDevice(ctx, info)
	if err != nil {
		return nil, err
	}

	opts := []ptp.Option{ptp.WithLogger(logger)}

	s := &session{info: info, reader: reader, tracePath: cmd.String("trace")}

	if s.tracePath != "" {
		s.recorder = trace.NewRecorder(app.Version)
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
		// A trace of a camera that never answered is still worth having.
		if traceErr := s.saveTrace(ctx); traceErr != nil {
			logger.Debug("saving the trace", zap.Error(traceErr))
		}

		s.close()

		return nil, err
	}

	return s, nil
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
