package usb

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deviceNotResponding is what libusb 1.0.30's darwin backend logs for an IOKit
// error it has no special case for (darwin_transfer_status), and notResponding
// that message without libusb's prefix.
const (
	notResponding       = "transfer error: device not responding (value = 0xe00002ed)"
	deviceNotResponding = "libusb: warning [darwin_transfer_status] " + notResponding + "\n"
)

func TestTheLogKeepsWhatWasSaidDuringATransfer(t *testing.T) {
	var said libusbLog

	said.add(libusbLogLevelWarning, "libusb: warning [darwin_open] USBDeviceOpen: (os/kern) failure\n")

	said.start()
	said.add(libusbLogLevelWarning, deviceNotResponding)
	got := said.stop()

	said.add(libusbLogLevelWarning, "libusb: warning [darwin_claim_interface] after the transfer\n")

	assert.Equal(t, []string{notResponding}, got,
		"only what was said during the transfer, without libusb's prefix")
	assert.Empty(t, said.stop())
}

func TestEachTransferStartsAfresh(t *testing.T) {
	var said libusbLog

	said.start()
	said.add(libusbLogLevelError, "libusb: error [darwin_bulk_transfer] bulk transfer failed\n")
	said.start()

	assert.Empty(t, said.stop())
}

func TestTheLogKeepsOnlyErrorsAndWarnings(t *testing.T) {
	var said libusbLog

	said.start()
	said.add(libusbLogLevelError, "libusb: error [f] an error\n")
	said.add(libusbLogLevelWarning, "libusb: warning [f] a warning\n")
	said.add(3, "libusb: info [f] some information\n")
	said.add(4, "[ 0.000123] [00001a2b] libusb: debug [f] some debugging\n")
	said.add(0, "nothing")

	assert.Equal(t, []string{"an error", "a warning"}, said.stop())
}

func TestTheLogHasRoomForAFew(t *testing.T) {
	var said libusbLog

	said.start()

	for range maxSaid + 3 {
		said.add(libusbLogLevelWarning, deviceNotResponding)
	}

	assert.Len(t, said.stop(), maxSaid)
}

func TestNoLogKeepsNothing(t *testing.T) {
	var said *libusbLog

	said.start()
	said.add(libusbLogLevelWarning, deviceNotResponding)
	assert.Nil(t, said.stop())
}

func TestTheLogIsSafeFromAnotherThread(t *testing.T) {
	// libusb logs from its event thread as well as the caller's.
	var said libusbLog

	said.start()

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { said.add(libusbLogLevelWarning, deviceNotResponding) })
	}

	wg.Wait()

	assert.Len(t, said.stop(), maxSaid)
}

func TestLibusbsPrefixComesOff(t *testing.T) {
	for in, want := range map[string]string{
		deviceNotResponding: notResponding,
		"[ 0.004321] [00001a2b] libusb: warning [darwin_transfer_status] transfer error: timed out\n": "transfer error: timed out",
		"no prefix at all\n": "no prefix at all",
		"   \n":              "",
	} {
		assert.Equal(t, want, tidyLibusbMessage(in), "%q", in)
	}
}

func TestAFailedTransferSaysWhatLibusbSaid(t *testing.T) {
	failed := &codeError{text: "libusb_bulk_transfer (endpoint 0x81): LIBUSB_ERROR_IO (-1)", sentinel: ErrIO}

	err := explain(failed, []string{notResponding})
	require.EqualError(t, err, "libusb_bulk_transfer (endpoint 0x81): LIBUSB_ERROR_IO (-1); "+
		"libusb said: transfer error: device not responding (value = 0xe00002ed)")
	require.ErrorIs(t, err, ErrIO)

	err = explain(failed, []string{"one", "two"})
	require.EqualError(t, err, "libusb_bulk_transfer (endpoint 0x81): LIBUSB_ERROR_IO (-1); libusb said: one; two")
}

func TestAFailedTransferSaysWhenLibusbGaveNoReason(t *testing.T) {
	// An aborted transfer: libusb turns kIOReturnAborted into
	// LIBUSB_ERROR_IO without logging anything.
	failed := &codeError{text: "libusb_bulk_transfer (endpoint 0x81): LIBUSB_ERROR_IO (-1)", sentinel: ErrIO}

	err := explain(failed, nil)
	require.EqualError(t, err, "libusb_bulk_transfer (endpoint 0x81): LIBUSB_ERROR_IO (-1); libusb gave no reason")
	require.ErrorIs(t, err, ErrIO)
}

func TestExplainingKeepsWhatTheErrorIs(t *testing.T) {
	timeout := errors.Join(errors.New("libusb_bulk_transfer (endpoint 0x81): LIBUSB_ERROR_TIMEOUT"), ErrTimeout)

	err := explain(timeout, []string{"transfer error: timed out"})
	require.ErrorIs(t, err, ErrTimeout)
	require.NotErrorIs(t, err, ErrIO)
	require.NoError(t, explain(nil, []string{"anything"}))
}
