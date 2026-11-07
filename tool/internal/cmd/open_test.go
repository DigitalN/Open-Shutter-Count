package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/DigitalN/Open-Shutter-Count/tool/internal/logs"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/usb"
)

// fakeDevice stands in for an opened camera.
type fakeDevice struct{ usb.Device }

// fakeMac stands in for the camera and for ptpcamerad, which holds the camera
// until it has been stopped. Each stop it is sent is recorded: false for the
// polite one (SIGTERM), true for the forced one (SIGKILL).
type fakeMac struct {
	// stops is every call to stop the camera service, in order.
	stops []bool

	// freeAfterStops is how many stops the camera service survives before
	// letting go, so 1 is a service that exits on the first SIGTERM. Negative
	// means it never lets go.
	freeAfterStops int

	// freeAfterOpens is how many opens (after the stops are done) fail before
	// one works, to stand in for the camera freeing up a poll or two late.
	freeAfterOpens int

	// openErr is what a camera that isn't free answers. busy by default.
	openErr error

	// stopErr is what stopping the service answers, such as killall exiting 1
	// when no ptpcamerad was running.
	stopErr error

	opensSinceFree int
	opens          int
	device         usb.Device
}

func (m *fakeMac) open(_ usb.DeviceInfo) (usb.Device, error) {
	m.opens++

	if m.openErr != nil {
		return nil, m.openErr
	}

	if m.freeAfterStops < 0 || len(m.stops) < m.freeAfterStops {
		return nil, fmt.Errorf("libusb_claim_interface: %w", usb.ErrBusy)
	}

	m.opensSinceFree++
	if m.opensSinceFree <= m.freeAfterOpens {
		return nil, fmt.Errorf("libusb_claim_interface: %w", usb.ErrBusy)
	}

	return m.device, nil
}

func (m *fakeMac) stop(_ context.Context, force bool) error {
	m.stops = append(m.stops, force)

	return m.stopErr
}

// install swaps the fake in for the real camera, the real killall and ioreg,
// and shrinks the waits so a camera that never frees up fails in milliseconds.
// It returns the context to use, which logs to the returned logs.
func (m *fakeMac) install(t *testing.T) (context.Context, *observer.ObservedLogs) {
	t.Helper()

	m.device = &fakeDevice{}

	oldOpen, oldStop, oldOwners := openCamera, stopCameraDaemon, cameraOwners
	oldDelay, oldPoll := darwinRetryDelay, darwinPollInterval

	t.Cleanup(func() {
		openCamera, stopCameraDaemon, cameraOwners = oldOpen, oldStop, oldOwners
		darwinRetryDelay, darwinPollInterval = oldDelay, oldPoll
	})

	openCamera = m.open
	stopCameraDaemon = m.stop
	cameraOwners = func(context.Context, usb.DeviceInfo) ([]usb.Owner, string, error) {
		return []usb.Owner{{PID: 589, Name: "ptpcamerad", Exclusive: true}}, "", nil
	}
	darwinRetryDelay = 3 * time.Millisecond
	darwinPollInterval = time.Millisecond

	core, observed := observer.New(zap.InfoLevel)

	return logs.StoreLoggerInContext(t.Context(), zap.New(core), "console"), observed
}

func messages(observed *observer.ObservedLogs) []string {
	var out []string

	for _, entry := range observed.All() {
		out = append(out, entry.Message)
	}

	return out
}

const (
	stoppingMessage = "the camera is held by another process, stopping ptpcamerad and retrying"
	killingMessage  = "ptpcamerad didn't let go, killing it and retrying"
)

func TestOpenDeviceFreeCameraStopsNothing(t *testing.T) {
	mac := &fakeMac{}
	ctx, observed := mac.install(t)

	device, err := openDevice(ctx, usb.DeviceInfo{})
	require.NoError(t, err)

	assert.Same(t, mac.device, device)
	assert.Empty(t, mac.stops)
	assert.Empty(t, messages(observed))
}

func TestOpenDeviceFirstStopAsksPolitely(t *testing.T) {
	mac := &fakeMac{freeAfterStops: 1}
	ctx, _ := mac.install(t)

	device, err := openDevice(ctx, usb.DeviceInfo{})
	require.NoError(t, err)

	assert.Same(t, mac.device, device)
	assert.Equal(t, []bool{false}, mac.stops)
}

func TestOpenDeviceNeverKillsACameraThatFreesUpAfterTheFirstStop(t *testing.T) {
	// The camera is still busy for the first couple of polls after the
	// SIGTERM, then frees up within the same attempt.
	mac := &fakeMac{freeAfterStops: 1, freeAfterOpens: 2}
	ctx, observed := mac.install(t)

	// A delay long enough that the polls can't run out before it frees up.
	darwinRetryDelay = time.Second

	device, err := openDevice(ctx, usb.DeviceInfo{})
	require.NoError(t, err)

	assert.Same(t, mac.device, device)
	assert.Equal(t, []bool{false}, mac.stops, "a camera that frees up after the first stop is never killed")
	assert.Equal(t, []string{stoppingMessage}, messages(observed))
}

func TestOpenDeviceKillsAfterTheFirstTry(t *testing.T) {
	// Like the reporter's Mac: ptpcamerad ignores SIGTERM and lets go at
	// SIGKILL, which is the second stop.
	mac := &fakeMac{freeAfterStops: 2}
	ctx, observed := mac.install(t)

	device, err := openDevice(ctx, usb.DeviceInfo{})
	require.NoError(t, err)

	assert.Same(t, mac.device, device)
	assert.Equal(t, []bool{false, true}, mac.stops)
	assert.Equal(t, []string{stoppingMessage, killingMessage}, messages(observed))
}

func TestOpenDeviceKeepsKillingUpToTheLastAttempt(t *testing.T) {
	mac := &fakeMac{freeAfterStops: darwinOpenAttempts}
	ctx, observed := mac.install(t)

	device, err := openDevice(ctx, usb.DeviceInfo{})
	require.NoError(t, err)

	assert.Same(t, mac.device, device)
	assert.Equal(t, []bool{false, true, true, true, true}, mac.stops)
	assert.Equal(t, []string{stoppingMessage, killingMessage}, messages(observed), "the kill is logged once, not every time")
}

func TestOpenDeviceGivesUpWithTheBusyHint(t *testing.T) {
	mac := &fakeMac{freeAfterStops: -1}
	ctx, _ := mac.install(t)

	device, err := openDevice(ctx, usb.DeviceInfo{})
	require.ErrorIs(t, err, usb.ErrBusy)
	assert.Nil(t, device)

	assert.Equal(t, []bool{false, true, true, true, true}, mac.stops)
	assert.Contains(t, err.Error(), darwinBusyHint([]usb.Owner{{PID: 589, Name: "ptpcamerad", Exclusive: true}}))
	assert.Equal(t, kindBusy, errorKind(err))
}

func TestOpenDeviceFailedKillallIsOnlyDebug(t *testing.T) {
	// killall exits 1 when no ptpcamerad is running. The camera can still be
	// busy for another reason, so the retries carry on, quietly.
	mac := &fakeMac{freeAfterStops: 2, stopErr: errors.New("exit status 1")}
	ctx, observed := mac.install(t)

	device, err := openDevice(ctx, usb.DeviceInfo{})
	require.NoError(t, err)

	assert.Same(t, mac.device, device)
	assert.Equal(t, []bool{false, true}, mac.stops)
	assert.Equal(t, []string{stoppingMessage, killingMessage}, messages(observed), "no error above debug")
}

func TestOpenDeviceOtherErrorsReturnAtOnce(t *testing.T) {
	for name, err := range map[string]error{
		"timeout":       fmt.Errorf("libusb_open: %w", usb.ErrTimeout),
		"something new": errors.New("libusb_open: input/output error"),
	} {
		t.Run(name, func(t *testing.T) {
			mac := &fakeMac{openErr: err}
			ctx, observed := mac.install(t)

			device, got := openDevice(ctx, usb.DeviceInfo{})
			require.ErrorIs(t, got, err)
			assert.Nil(t, device)

			assert.Empty(t, mac.stops, "nothing to stop when the camera isn't busy")
			assert.Equal(t, 1, mac.opens)
			assert.Empty(t, messages(observed))
		})
	}
}

func TestOpenDeviceWrongUSBModeIsHintedWithoutStopping(t *testing.T) {
	mac := &fakeMac{openErr: fmt.Errorf("usb:001:002: %w", usb.ErrNoStillImage)}
	ctx, _ := mac.install(t)

	_, err := openDevice(ctx, usb.DeviceInfo{})
	require.ErrorIs(t, err, usb.ErrNoStillImage)
	assert.Contains(t, err.Error(), noStillImageHint)
	assert.Empty(t, mac.stops)
}

func TestOpenDeviceOtherErrorAfterAStopReturnsAtOnce(t *testing.T) {
	// The camera is busy, then something else goes wrong once ptpcamerad has
	// been asked to leave. No kill follows.
	mac := &fakeMac{freeAfterStops: -1}
	ctx, observed := mac.install(t)

	failure := errors.New("libusb_open: input/output error")
	mac.openErr = nil

	opens := 0
	openCamera = func(usb.DeviceInfo) (usb.Device, error) {
		opens++
		if opens == 1 {
			return nil, fmt.Errorf("libusb_claim_interface: %w", usb.ErrBusy)
		}

		return nil, failure
	}

	_, err := openDevice(ctx, usb.DeviceInfo{})
	require.ErrorIs(t, err, failure)

	assert.Equal(t, []bool{false}, mac.stops)
	assert.Equal(t, []string{stoppingMessage}, messages(observed))
}
