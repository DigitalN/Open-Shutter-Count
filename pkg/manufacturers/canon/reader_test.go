package canon

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DigitalN/openshuttercounter/pkg/manufacturers"
	"github.com/DigitalN/openshuttercounter/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/pkg/usb"
)

// The standard property request, as camera.commands records it.
const askD1AC = "0x1015[53676]"

// r8Info is GetDeviceInfo as an EOS R8 answers it, cut down to what's read.
var r8Info = ptp.DeviceInfo{
	Manufacturer: "Canon.Inc",
	Model:        "Canon EOS R8",
	Operations: []uint16{
		ptp.OpGetDeviceInfo, ptp.OpGetDevicePropValue,
		OpEOSSetRemoteMode, OpEOSSetEventMode, OpEOSGetEvent, OpEOSRequestDevicePropValue,
	},
}

// eventCamera answers 0xD1AC as the R8 does, and reports d167 on its event
// channel. It fails the test if the factory channel is sent.
func eventCamera(t *testing.T, d167 []byte) *camera {
	t.Helper()

	sent := 0

	return &camera{answer: func(op uint16, _ []uint32) ([]byte, uint16) {
		switch op {
		case ptp.OpGetDevicePropValue:
			return nil, ptp.RCOperationNotSupported
		case OpEOSGetEvent:
			sent++
			if sent == 1 && d167 != nil {
				return eventList(propRecord(0xd167, d167...)), ptp.RCOK
			}

			return eventList(), ptp.RCOK
		case OpFactorySend, OpFactoryRecv:
			t.Fatalf("the factory channel was sent %#04x", op)
		}

		return nil, ptp.RCOK
	}}
}

func read(t *testing.T, cam *camera, productID uint16, info ptp.DeviceInfo) (manufacturers.Reading, error) {
	t.Helper()

	return New().Read(context.Background(), ptp.New(cam), usb.DeviceInfo{VendorID: VendorID, ProductID: productID}, info)
}

func TestReadAnR8(t *testing.T) {
	cam := eventCamera(t, r8D167)

	reading, err := read(t, cam, 0x330c, r8Info)
	require.NoError(t, err)

	assert.Equal(t, askD1AC, cam.commands[0], "the standard property comes first")
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

	assert.Equal(t, "GetDevicePropValue(0xd1ac): OperationNotSupported", reading.Steps[0])
	assert.Contains(t, reading.Steps, "0xd167: 1000000001000000d0070000f0550000")
}

func TestAnotherRFBodyReadsButIsNotTested(t *testing.T) {
	// libgphoto2's EOS R6 dump: 1000 and 1000. Its USB ID isn't one this
	// knows: the model name is what counts.
	info := r8Info
	info.Model = "Canon EOS R6"

	reading, err := read(t, eventCamera(t, []byte{
		0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xe8, 0x03, 0, 0, 0xe8, 0x03, 0, 0,
	}), 0x32f5, info)
	require.NoError(t, err)

	assert.Equal(t, "EOS R6", reading.Model)
	assert.False(t, reading.Confirmed)
	assert.Equal(t, MethodEventChannel, reading.Method)
	assert.Equal(t, uint32(1000), reading.Counters[0].Value)
}

func TestAnR8WithoutThePropertyIsAFailedRead(t *testing.T) {
	_, err := read(t, eventCamera(t, nil), 0x330c, r8Info)
	require.ErrorIs(t, err, errNoReleaseCounter)
	assert.NotErrorIs(t, err, manufacturers.ErrNotSupported)
}

func TestAnotherRFBodyWithoutThePropertyCantBeReadYet(t *testing.T) {
	info := r8Info
	info.Model = "Canon EOS R50"

	reading, err := read(t, eventCamera(t, nil), 0x3331, info)
	require.ErrorIs(t, err, manufacturers.ErrNotSupported)
	assert.NotEmpty(t, reading.Steps, "a failed read still says what it did")
}

func TestA90DIsReadThroughTheEventChannel(t *testing.T) {
	// An EF body that, like the R8, lists the event channel and not the
	// factory channel (libgphoto2's 90D dump). It hung on MonOpen upstream.
	info := r8Info
	info.Model = "Canon EOS 90D"

	cam := eventCamera(t, []byte{0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xe8, 0x03, 0, 0, 0xe8, 0x03, 0, 0})

	reading, err := read(t, cam, 0x3295, info)
	require.NoError(t, err)
	assert.Equal(t, MethodEventChannel, reading.Method)
	assert.False(t, reading.Confirmed)
}

func TestABodyThatListsNeitherChannelIsOnlyAskedTheStandardProperty(t *testing.T) {
	for _, model := range []string{"Canon EOS R100", "Canon EOS 750D", "Canon PowerShot G7 X Mark III"} {
		info := r8Info
		info.Model = model
		info.Operations = []uint16{ptp.OpGetDeviceInfo, ptp.OpGetDevicePropValue}

		cam := eventCamera(t, r8D167)

		reading, err := read(t, cam, 0x3340, info)
		require.ErrorIs(t, err, manufacturers.ErrNotSupported, model)
		assert.Equal(t, []string{askD1AC}, cam.commands, model)
		assert.Contains(t, err.Error(), reading.Model)
	}
}

// factoryCamera answers the factory channel's MonOpen with monOpen, then each
// read with the next reply, and the standard property and event channel as
// the R8 does.
func factoryCamera(t *testing.T, monOpen uint16, replies ...[]byte) *camera {
	t.Helper()

	sent, received := 0, 0
	inner := eventCamera(t, r8D167)

	return &camera{answer: func(op uint16, params []uint32) ([]byte, uint16) {
		switch op {
		case OpFactorySend:
			sent++
			if sent == 1 {
				return nil, monOpen
			}

			return nil, ptp.RCOK
		case OpFactoryRecv:
			received++
			if received > len(replies) {
				return nil, ptp.RCGeneralError
			}

			return replies[received-1], ptp.RCOK
		}

		return inner.answer(op, params)
	}}
}

// efInfo is an EF DSLR that lists the factory channel and the event channel,
// as libgphoto2's 80D dump shows.
var efInfo = ptp.DeviceInfo{
	Model: "Canon EOS 80D",
	Operations: []uint16{
		ptp.OpGetDeviceInfo, ptp.OpGetDevicePropValue, OpFactorySend, OpFactoryRecv,
		OpEOSSetRemoteMode, OpEOSSetEventMode, OpEOSGetEvent, OpEOSRequestDevicePropValue,
	},
}

func TestAnEFBodyThatListsTheFactoryChannelIsReadThere(t *testing.T) {
	cam := factoryCamera(t, ptp.RCOK,
		[]byte{0x00, 0x66, 0, 0, 0, 0, 0, 0xf2, 0, 0},
		[]byte{0x00, 0x66, 0, 0, 0, 0, 0, 0, 0xf2, 0},
		[]byte{0x00, 0x67, 0, 0, 0, 0, 0, 0, 0, 0x01})

	reading, err := read(t, cam, 0x3294, efInfo)
	require.NoError(t, err)

	assert.Equal(t, "EOS 80D", reading.Model)
	assert.Equal(t, MethodFactoryChannel, reading.Method)
	assert.False(t, reading.Confirmed, "the 5D Mark IV's addresses are a guess on an 80D")
	assert.Equal(t, uint32(0x66f2), reading.Counters[0].Value)
	assert.Zero(t, reading.RatedLife, "the 5D Mark IV's rating isn't the 80D's")
	assert.NotContains(t, cam.commands, getEvent, "the event channel isn't needed")
}

func TestTheEventChannelIsTriedWhenTheFactoryChannelGivesNothing(t *testing.T) {
	for name, cam := range map[string]*camera{
		"MonOpen refused":     factoryCamera(t, ptp.RCOperationNotSupported),
		"no counter answered": factoryCamera(t, ptp.RCOK),
	} {
		reading, err := read(t, cam, 0x3294, efInfo)
		require.NoError(t, err, name)
		assert.Equal(t, MethodEventChannel, reading.Method, name)
		assert.Contains(t, reading.Steps, "the factory channel gave no counts: trying the event channel", name)
	}
}

func TestAFactoryChannelThatGivesNothingWithNoEventChannelCantBeReadYet(t *testing.T) {
	info := efInfo
	info.Operations = []uint16{ptp.OpGetDeviceInfo, ptp.OpGetDevicePropValue, OpFactorySend, OpFactoryRecv}

	_, err := read(t, factoryCamera(t, ptp.RCOK), 0x3294, info)
	require.ErrorIs(t, err, manufacturers.ErrNotSupported)
}

func TestTheStandardPropertyWinsWhenAnswered(t *testing.T) {
	cam := &camera{answer: func(op uint16, _ []uint32) ([]byte, uint16) {
		if op == ptp.OpGetDevicePropValue {
			return []byte{0x39, 0x30, 0, 0}, ptp.RCOK // 12345
		}

		t.Fatalf("sent %#04x after the standard property answered", op)

		return nil, ptp.RCOK
	}}

	info := r8Info
	info.Model = "Canon EOS 5D Mark III"

	reading, err := read(t, cam, 0x3226, info)
	require.NoError(t, err)

	assert.Equal(t, MethodStandardProperty, reading.Method)
	assert.Equal(t, uint32(12345), reading.Counters[0].Value)
	assert.False(t, reading.Confirmed, "only the R8 and the 5D Mark IV say tested")
}

func TestTheStandardPropertyIsntAskedOfACameraThatDoesntListTheRequest(t *testing.T) {
	info := r8Info
	info.Operations = slices.DeleteFunc(slices.Clone(info.Operations), func(op uint16) bool {
		return op == ptp.OpGetDevicePropValue
	})

	cam := eventCamera(t, r8D167)

	_, err := read(t, cam, 0x330c, info)
	require.NoError(t, err)
	assert.NotContains(t, cam.commands, askD1AC)
}

func TestThe5DMarkIVIsReadThroughTheFactoryChannel(t *testing.T) {
	// Each counter reply, as docs/PROTOCOL.md shows them.
	replies := map[int][]byte{
		0: {0x00, 0x66, 0, 0, 0, 0, 0, 0xf2, 0, 0},
		1: {0x00, 0x66, 0, 0, 0, 0, 0, 0, 0xf2, 0},
		2: {0x00, 0x67, 0, 0, 0, 0, 0, 0, 0, 0x01},
	}

	read := 0
	sent := []string{}

	cam := &camera{answer: func(op uint16, _ []uint32) ([]byte, uint16) {
		switch op {
		case ptp.OpGetDevicePropValue:
			return nil, ptp.RCDevicePropNotSupported
		case OpFactorySend:
			sent = append(sent, "send")
		case OpFactoryRecv:
			read++

			return replies[read-1], ptp.RCOK
		default:
			t.Fatalf("sent %#04x to a 5D Mark IV", op)
		}

		return nil, ptp.RCOK
	}}

	info := ptp.DeviceInfo{Model: "Canon EOS 5D Mark IV", Operations: []uint16{ptp.OpGetDevicePropValue}}

	reading, err := New().Read(context.Background(), ptp.New(cam), usb.DeviceInfo{VendorID: VendorID, ProductID: 0x3281}, info)
	require.NoError(t, err)

	assert.Equal(t, "EOS 5D Mark IV", reading.Model)
	assert.Equal(t, MethodFactoryChannel, reading.Method)
	assert.True(t, reading.Confirmed)
	assert.Equal(t, uint32(0x66f2), reading.Counters[0].Value)
	assert.Len(t, sent, 5, "MonOpen, three reads and MonClose")
}

func TestFactoryOnlyRefusesBodiesThatDontListIt(t *testing.T) {
	cam := eventCamera(t, r8D167)

	_, err := New(WithFactoryOnly(true)).Read(context.Background(), ptp.New(cam),
		usb.DeviceInfo{VendorID: VendorID, ProductID: 0x330c}, r8Info)
	require.Error(t, err)
	assert.Empty(t, cam.commands)
}
