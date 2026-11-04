package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DigitalN/openshuttercounter/pkg/manufacturers"
	"github.com/DigitalN/openshuttercounter/pkg/manufacturers/canon"
	"github.com/DigitalN/openshuttercounter/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/pkg/usb"
)

func TestErrorKinds(t *testing.T) {
	for err, want := range map[error]string{
		noDeviceError{}: kindNoCamera,
		fmt.Errorf("%w: 2 are connected", errSeveralCameras):              kindSeveralCameras,
		withHint(fmt.Errorf("usb:001:002: %w", usb.ErrNoStillImage), "x"): kindWrongUSBMode,
		withHint(fmt.Errorf("libusb_open: %w", usb.ErrBusy), "quit it"):   kindBusy,
		fmt.Errorf("%w: the EOS 90D", manufacturers.ErrNotSupported):      kindNotSupported,
		fmt.Errorf("the camera didn't answer: %w", usb.ErrTimeout):        kindNoAnswer,
		errors.New("something else"):                                      kindFailed,
	} {
		assert.Equal(t, want, errorKind(err), err.Error())
	}
}

func TestTheHintIsKeptApart(t *testing.T) {
	err := withHint(fmt.Errorf("libusb_open: %w", usb.ErrBusy), "Quit it completely.")

	var hinted hintedError
	require.ErrorAs(t, err, &hinted)
	assert.Equal(t, "Quit it completely.", hinted.hint)
	assert.Contains(t, err.Error(), "Quit it completely.", "the command line still shows it")
	assert.Equal(t, err, withHint(err, "")) //nolint:testifylint // the same error back
}

func TestNoSerialNumberInTheOutput(t *testing.T) {
	info := usb.DeviceInfo{
		VendorID: 0x04a9, ProductID: 0x330c, Product: "Canon Digital Camera", Serial: "SERIAL123", Path: fakePath,
	}
	camera := ptp.DeviceInfo{
		Model: "Canon EOS R8", DeviceVersion: "3-1.5.0", SerialNumber: "SERIAL456", Operations: []uint16{0x1001, 0x9116},
	}

	raw, err := json.Marshal(buildOutput(info, camera, manufacturers.Reading{Model: "EOS R8"}, true))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "SERIAL")
	assert.Contains(t, string(raw), `"operations":["0x1001","0x9116"]`)
	assert.Contains(t, string(raw), `"firmware":"3-1.5.0"`)

	listed, err := json.Marshal(listOutput(registry(nil, false), []usb.DeviceInfo{info}))
	require.NoError(t, err)
	assert.NotContains(t, string(listed), "SERIAL")
	assert.Contains(t, string(listed), `"name":"Canon Digital Camera"`)
}

func TestTheDeviceListNamesTheR8ByItsUSBID(t *testing.T) {
	// macOS's list doesn't read the product name.
	listed := listOutput(registry(nil, false), []usb.DeviceInfo{{VendorID: canon.VendorID, ProductID: 0x330c}})
	assert.Equal(t, "Canon EOS R8", listed[0].Name)
}
