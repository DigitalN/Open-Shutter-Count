//go:build darwin

package usb

import (
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLibusb stands in for libusb, with no camera and no libusb loaded. Each
// transfer logs the messages given, as libusb's callback would, and returns
// rc.
func fakeLibusb(rc int32, says ...string) *darwinDevice {
	api := &libusbAPI{said: &libusbLog{}}

	transfer := func() int32 {
		for _, message := range says {
			api.said.add(libusbLogLevelWarning, message)
		}

		return rc
	}

	api.bulkTransfer = func(_ uintptr, _ uint8, _ unsafe.Pointer, _ int32, _ *int32, _ uint32) int32 {
		return transfer()
	}
	api.controlTransfer = func(_ uintptr, _, _ uint8, _, _ uint16, _ unsafe.Pointer, _ uint16, _ uint32) int32 {
		return transfer()
	}
	api.errorName = func(code int32) string {
		return map[int32]string{
			libusbErrorIO: "LIBUSB_ERROR_IO", libusbErrorTimeout: "LIBUSB_ERROR_TIMEOUT",
		}[code]
	}

	return &darwinDevice{api: api, epIn: 0x81, epOut: 0x02}
}

func TestAnIOErrorSaysWhatLibusbSaid(t *testing.T) {
	d := fakeLibusb(libusbErrorIO, deviceNotResponding)

	// Something said before the transfer isn't its reason.
	d.api.said.add(libusbLogLevelWarning, "libusb: warning [darwin_open] USBDeviceOpen: busy\n")

	_, err := d.Read(make([]byte, 512), time.Second)
	require.ErrorIs(t, err, ErrIO)
	require.EqualError(t, err, "libusb_bulk_transfer (endpoint 0x81): LIBUSB_ERROR_IO (-1); "+
		"libusb said: transfer error: device not responding (value = 0xe00002ed)")
}

func TestAnIOErrorSaysWhenLibusbGaveNoReason(t *testing.T) {
	d := fakeLibusb(libusbErrorIO)

	_, err := d.Write([]byte{1}, time.Second)
	require.ErrorIs(t, err, ErrIO)
	require.EqualError(t, err, "libusb_bulk_transfer (endpoint 0x02): LIBUSB_ERROR_IO (-1); libusb gave no reason")
}

func TestATimeoutIsStillATimeout(t *testing.T) {
	d := fakeLibusb(libusbErrorTimeout, "libusb: warning [darwin_transfer_status] transfer error: timed out\n")

	_, err := d.Read(make([]byte, 512), time.Second)
	require.ErrorIs(t, err, ErrTimeout)
	require.NotErrorIs(t, err, ErrIO)
	require.EqualError(t, err, "libusb_bulk_transfer (endpoint 0x81): LIBUSB_ERROR_TIMEOUT: transfer timed out; "+
		"libusb said: transfer error: timed out")
}

func TestATransferThatWorkedSaysNothing(t *testing.T) {
	d := fakeLibusb(libusbSuccess, "libusb: warning [f] something harmless\n")

	_, err := d.Read(make([]byte, 512), time.Second)
	require.NoError(t, err)
	assert.Empty(t, d.api.said.stop(), "the log stops with the transfer")
}

func TestAFailedResetSaysWhatLibusbSaid(t *testing.T) {
	d := fakeLibusb(libusbErrorIO, "libusb: error [darwin_transfer_status] control request failed: x\n")

	err := d.ResetPTP()
	require.ErrorIs(t, err, ErrIO)
	require.EqualError(t, err, "libusb_control_transfer (PTP device reset): LIBUSB_ERROR_IO (-1); "+
		"libusb said: control request failed: x")

	require.NoError(t, fakeLibusb(libusbSuccess).ResetPTP())
}

func TestCString(t *testing.T) {
	b := []byte("transfer error: timed out\n\x00garbage")
	assert.Equal(t, "transfer error: timed out\n", cString(&b[0], len(b)))

	unterminated := []byte("abc")
	assert.Equal(t, "abc", cString(&unterminated[0], len(unterminated)))
	assert.Empty(t, cString(nil, 10))
}

func TestTheCallbackKeepsWhatLibusbSaid(t *testing.T) {
	api := &libusbAPI{said: &libusbLog{}}
	message := []byte(deviceNotResponding + "\x00")

	api.said.start()
	api.logged(0, libusbLogLevelWarning, &message[0])

	assert.Equal(t, []string{notResponding}, api.said.stop())
}
