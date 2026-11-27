package cmd

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/ptp"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/usb"
)

// ioError is a USB connection failing the way the macOS backend reports it:
// LIBUSB_ERROR_IO, with what libusb said.
type ioError string

func (e ioError) Error() string        { return string(e) }
func (e ioError) Is(target error) bool { return target == usb.ErrIO }

// What issue 39's 5D Mark IV answered GetDeviceInfo with, with a reason added
// as libusb would give one.
const errIssue39 = ioError("libusb_bulk_transfer (endpoint 0x81): LIBUSB_ERROR_IO (-1); " +
	"libusb said: transfer error: device not responding (value = 0xe00002ed)")

// flakyCamera is a scripted camera whose USB connection fails the reply to
// each of the commands in fail, in order: the first read after that command
// is sent fails with failure. A class reset drops whatever the camera had
// queued, as a real one does.
type flakyCamera struct {
	*scriptedCamera

	fail    []uint16
	failure error
	armed   bool

	// sent is every command sent, and resets and halts how many class resets
	// and endpoint clears.
	sent          []uint16
	resets, halts int
}

func flaky(cam *scriptedCamera, fail ...uint16) *flakyCamera {
	return &flakyCamera{scriptedCamera: cam, fail: fail, failure: errIssue39}
}

func (c *flakyCamera) Write(p []byte, timeout time.Duration) (int, error) {
	if head, _, err := ptp.ParseHeader(p); err == nil && head.Type == ptp.ContainerCommand {
		c.sent = append(c.sent, head.Code)

		if len(c.fail) > 0 && c.fail[0] == head.Code {
			c.fail, c.armed = c.fail[1:], true
		}
	}

	return c.scriptedCamera.Write(p, timeout)
}

func (c *flakyCamera) Read(p []byte, timeout time.Duration) (int, error) {
	if c.armed {
		c.armed = false

		return 0, c.failure
	}

	return c.scriptedCamera.Read(p, timeout)
}

func (c *flakyCamera) ResetPTP() error {
	c.resets++
	c.pending = nil

	return nil
}

func (c *flakyCamera) ClearHalt() error {
	c.halts++

	return nil
}

// sessionSteps leaves out the tested list's steps, which come first.
func sessionSteps(out readOutput) []string {
	return slices.DeleteFunc(slices.Clone(out.Steps), func(step string) bool {
		return !strings.HasPrefix(step, "OpenSession") && !strings.HasPrefix(step, "GetDeviceInfo") &&
			!strings.HasPrefix(step, "Reset the camera")
	})
}

const (
	openedStep   = "OpenSession: OK"
	issue39Step  = "GetDeviceInfo: bulk read: " + string(errIssue39)
	reopenedStep = "Reset the camera and opened the session again: OK"
)

func TestAnIOErrorAtGetDeviceInfoIsTriedOnceMore(t *testing.T) {
	cam := flaky(fakeCamera(t, "Canon EOS R8", 0x330c, eventChannel, r8D167), ptp.OpGetDeviceInfo)

	out, _ := readFixture(t, cam)
	require.Nil(t, out.Error, "the read goes on")
	require.NotNil(t, out.Reading)

	steps := sessionSteps(out)
	require.Len(t, steps, 4)
	assert.Equal(t, []string{openedStep, issue39Step, reopenedStep}, steps[:3])
	assert.True(t, strings.HasPrefix(steps[3], "GetDeviceInfo: OK, Canon EOS R8, firmware 3-1.5.0"), steps[3])

	assert.Equal(t, 1, cam.resets)
	assert.Equal(t, 1, cam.halts)
	assert.Equal(t, []uint16{ptp.OpOpenSession, ptp.OpGetDeviceInfo, ptp.OpOpenSession, ptp.OpGetDeviceInfo},
		cam.sent[:4], "the session is opened again after the reset, and nothing else is sent before GetDeviceInfo")
}

func TestTwoIOErrorsAtGetDeviceInfoFailTheRead(t *testing.T) {
	cam := flaky(fakeCamera(t, "Canon EOS 5D Mark IV", 0x3281, factoryChannel, nil), ptp.OpGetDeviceInfo, ptp.OpGetDeviceInfo)

	out, _ := readFixture(t, cam)
	require.NotNil(t, out.Error)

	assert.Equal(t, kindUSBError, out.Error.Kind)
	assert.Equal(t, "bulk read: "+string(errIssue39), out.Error.Message)
	assert.Equal(t, usbErrorHint, out.Error.Hint)
	assert.Nil(t, out.Reading)

	assert.Equal(t, []string{openedStep, issue39Step, reopenedStep, issue39Step}, sessionSteps(out))
	assert.Equal(t, 1, cam.resets, "only one more try")
	assert.Equal(t, []uint16{ptp.OpOpenSession, ptp.OpGetDeviceInfo, ptp.OpOpenSession, ptp.OpGetDeviceInfo},
		cam.sent, "nothing is sent after the second failure")
}

func TestAFailedReopenFailsTheRead(t *testing.T) {
	cam := flaky(fakeCamera(t, "Canon EOS 5D Mark IV", 0x3281, factoryChannel, nil), ptp.OpGetDeviceInfo, ptp.OpOpenSession)

	out, _ := readFixture(t, cam)
	require.NotNil(t, out.Error)

	assert.Equal(t, kindUSBError, out.Error.Kind)
	assert.Equal(t, "OpenSession: bulk read: "+string(errIssue39), out.Error.Message)
	assert.Equal(t, []string{
		openedStep,
		issue39Step,
		"Reset the camera and opened the session again: OpenSession: bulk read: " + string(errIssue39),
	}, sessionSteps(out))
	assert.Equal(t, []uint16{ptp.OpOpenSession, ptp.OpGetDeviceInfo, ptp.OpOpenSession}, cam.sent,
		"GetDeviceInfo isn't asked for again")
}

func TestATimeoutAtGetDeviceInfoIsntTriedAgain(t *testing.T) {
	cam := flaky(fakeCamera(t, "Canon EOS 5D Mark IV", 0x3281, factoryChannel, nil), ptp.OpGetDeviceInfo)
	cam.failure = fmt.Errorf("libusb_bulk_transfer (endpoint 0x81): LIBUSB_ERROR_TIMEOUT: %w", usb.ErrTimeout)

	out, _ := readFixture(t, cam)
	require.NotNil(t, out.Error)

	assert.Equal(t, kindNoAnswer, out.Error.Kind)
	assert.Zero(t, cam.resets)
	assert.Equal(t, []uint16{ptp.OpOpenSession, ptp.OpGetDeviceInfo}, cam.sent)
}

func TestAnIOErrorAnywhereIsAUSBError(t *testing.T) {
	err := fmt.Errorf("libusb_claim_interface: %w", errIssue39)
	assert.Equal(t, kindUSBError, errorKind(err))

	out := failureOutput(readOutput{}, err)
	assert.Equal(t, usbErrorHint, out.Error.Hint)
	assert.Equal(t, err.Error(), out.Error.Message)
	assert.Contains(t, hintUSBError(err).Error(), usbErrorHint, "the command line shows it too")

	busy := withHint(fmt.Errorf("libusb_open: %w", usb.ErrBusy), "quit it")
	assert.Equal(t, busy, hintUSBError(busy), "nothing else gets the hint") //nolint:testifylint // the same error back
}
