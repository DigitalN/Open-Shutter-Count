package canon

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DigitalN/openshuttercounter/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/pkg/usb"
)

// r8D167 is what the maintainer's EOS R8 reported after the 2026-11-04 test.
var r8D167 = []byte{
	0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xd0, 0x07, 0, 0, 0xf0, 0x55, 0, 0,
}

func TestDecodeReleaseCounter(t *testing.T) {
	counts, err := DecodeReleaseCounter(r8D167)
	require.NoError(t, err)

	assert.Equal(t, ReleaseCounter{
		Mechanical: Range{Min: 1001, Max: 2000},
		All:        Range{Min: 21001, Max: 22000},
		Electronic: Range{Min: 19001, Max: 20999},
	}, counts)
}

func TestDecodeReleaseCounterNewCamera(t *testing.T) {
	// libgphoto2's M50 Mark II dump: 1000 and 1000.
	counts, err := DecodeReleaseCounter([]byte{
		0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xe8, 0x03, 0, 0, 0xe8, 0x03, 0, 0,
	})
	require.NoError(t, err)

	assert.Equal(t, Range{Min: 1, Max: 1000}, counts.Mechanical)
	assert.Equal(t, Range{Min: 0, Max: 999}, counts.Electronic)
}

func TestDecodeReleaseCounterRefusesOtherLayouts(t *testing.T) {
	for name, raw := range map[string][]byte{
		"short":               r8D167[:12],
		"other header":        append([]byte{0x10, 0, 0, 0, 0x02, 0, 0, 0}, r8D167[8:]...),
		"not thousands":       {0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xd1, 0x07, 0, 0, 0xf0, 0x55, 0, 0},
		"mechanical over all": {0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xf0, 0x55, 0, 0, 0xd0, 0x07, 0, 0},
	} {
		_, err := DecodeReleaseCounter(raw)
		assert.Error(t, err, name)
	}
}

// deviceInfo builds a minimal GetDeviceInfo dataset listing ops.
func deviceInfo(ops ...uint16) []byte {
	array := func(values ...uint16) []byte {
		out := binary.LittleEndian.AppendUint32(nil, uint32(len(values))) //nolint:gosec // a handful of test values
		for _, v := range values {
			out = binary.LittleEndian.AppendUint16(out, v)
		}

		return out
	}

	out := binary.LittleEndian.AppendUint16(nil, 100) // standard version
	out = binary.LittleEndian.AppendUint32(out, 6)    // vendor extension ID
	out = binary.LittleEndian.AppendUint16(out, 100)  // vendor extension version
	out = append(out, 0)                              // vendor extension description
	out = binary.LittleEndian.AppendUint16(out, 0)    // functional mode
	out = append(out, array(ops...)...)

	for range 4 { // events, properties, capture and image formats
		out = append(out, array()...)
	}

	return append(out, 0, 0, 0, 0) // manufacturer, model, version, serial
}

func TestReadAnR8(t *testing.T) {
	ops := append([]uint16{ptp.OpGetDeviceInfo}, eventOps.Operations...)
	sent := 0

	cam := &camera{answer: func(op uint16, _ []uint32) ([]byte, uint16) {
		switch op {
		case ptp.OpGetDeviceInfo:
			return deviceInfo(ops...), ptp.RCOK
		case OpEOSGetEvent:
			sent++
			if sent == 1 {
				return eventList(propRecord(0xd167, r8D167...)), ptp.RCOK
			}

			return eventList(), ptp.RCOK
		case OpFactorySend, OpFactoryRecv:
			t.Fatalf("the factory channel was sent %#04x", op)
		}

		return nil, ptp.RCOK
	}}

	reading, err := New().Read(context.Background(), ptp.New(cam), usb.DeviceInfo{VendorID: VendorID, ProductID: 0x330c})
	require.NoError(t, err)

	assert.Equal(t, "EOS R8", reading.Model)
	assert.Equal(t, MethodEventChannel, reading.Method)
	assert.True(t, reading.Confirmed)
	require.Len(t, reading.Counters, 3)

	mech, elec, all := reading.Counters[0], reading.Counters[1], reading.Counters[2]
	assert.Equal(t,
		[]any{"Mechanical shutter", true, uint32(1001), uint32(2000)},
		[]any{mech.Label, mech.Range, mech.Min, mech.Value})
	assert.Equal(t, []any{"Electronic shutter", uint32(19001), uint32(20999)}, []any{elec.Label, elec.Min, elec.Value})
	assert.Equal(t, []any{"All photos", uint32(21001), uint32(22000)}, []any{all.Label, all.Min, all.Value})
}

func TestReadAnR8WithoutTheProperty(t *testing.T) {
	ops := append([]uint16{ptp.OpGetDeviceInfo}, OpEOSSetRemoteMode, OpEOSSetEventMode, OpEOSGetEvent)

	cam := &camera{answer: func(op uint16, _ []uint32) ([]byte, uint16) {
		switch op {
		case ptp.OpGetDeviceInfo:
			return deviceInfo(ops...), ptp.RCOK
		case OpEOSGetEvent:
			return eventList(), ptp.RCOK
		}

		return nil, ptp.RCOK
	}}

	_, err := New().Read(context.Background(), ptp.New(cam), usb.DeviceInfo{VendorID: VendorID, ProductID: 0x330c})
	require.ErrorIs(t, err, errNoReleaseCounter)
}
