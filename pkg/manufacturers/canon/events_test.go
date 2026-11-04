package canon

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DigitalN/openshuttercounter/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/pkg/usb"
)

// eosD167 is property 0xD167 as libgphoto2's EOS R dump shows it.
var eosD167 = []byte{
	0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xa0, 0x0f, 0, 0, 0xb8, 0x88, 0, 0,
}

// What the camera sees, in the form camera.commands records it.
const (
	enterRemote = "0x9114[1]"
	enterEvents = "0x9115[1]"
	getEvent    = "0x9116[]"
	resetRemote = "0x9114[0]"
	leaveEvents = "0x9115[0]"
)

// leaving is what libgphoto2 sends when it stops tethering.
var leaving = []string{resetRemote, enterRemote, leaveEvents}

func record(code uint32, data ...byte) []byte {
	out := binary.LittleEndian.AppendUint32(nil, uint32(recordHeaderLen+len(data))) //nolint:gosec // test records are small
	out = binary.LittleEndian.AppendUint32(out, code)

	return append(out, data...)
}

func propRecord(prop uint32, value ...byte) []byte {
	return record(EventPropValueChanged, append(binary.LittleEndian.AppendUint32(nil, prop), value...)...)
}

func eventList(records ...[]byte) []byte {
	var out []byte
	for _, r := range records {
		out = append(out, r...)
	}

	return append(out, record(0)...)
}

func TestParseEvents(t *testing.T) {
	records, err := ParseEvents(eventList(
		propRecord(0xd167, eosD167...),
		record(0xc1a5, 0x01, 0x02),
	))
	require.NoError(t, err)
	require.Len(t, records, 2)

	assert.Equal(t, EventRecord{Code: EventPropValueChanged, Prop: 0xd167, Data: eosD167}, records[0])
	assert.Equal(t, EventRecord{Code: 0xc1a5, Data: []byte{0x01, 0x02}}, records[1])
}

func TestParseEventsEmpty(t *testing.T) {
	records, err := ParseEvents(eventList())
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestParseEventsKeepsWhatItReadBeforeABadRecord(t *testing.T) {
	data := propRecord(0xd167, eosD167...)
	data = append(data, 0xff, 0, 0, 0, 0x89, 0xc1, 0, 0)

	records, err := ParseEvents(data)
	require.Error(t, err)
	assert.Len(t, records, 1)

	_, err = ParseEvents(propRecord(0xd167, eosD167...))
	require.Error(t, err, "a list with no end record is truncated")
}

// camera answers each command container from a script, like a real one would.
type camera struct {
	answer   func(op uint16, params []uint32) (data []byte, code uint16)
	commands []string
	pending  [][]byte
}

func (c *camera) Info() usb.DeviceInfo { return usb.DeviceInfo{VendorID: VendorID} }

func (c *camera) Read(p []byte, _ time.Duration) (int, error) {
	if len(c.pending) == 0 {
		return 0, errors.New("the camera has nothing to send")
	}

	next := c.pending[0]
	c.pending = c.pending[1:]

	return copy(p, next), nil
}

func (c *camera) Write(p []byte, _ time.Duration) (int, error) {
	head, _, err := ptp.ParseHeader(p)
	if err != nil {
		return 0, err
	}

	head.Payload = p[ptp.HeaderLen:]
	params := head.Params()

	c.commands = append(c.commands, fmt.Sprintf("%#04x%v", head.Code, params))

	data, code := c.answer(head.Code, params)

	if data != nil {
		raw, err := ptp.Container{
			Type: ptp.ContainerData, Code: head.Code, TransactionID: head.TransactionID, Payload: data,
		}.MarshalBinary()
		if err != nil {
			return 0, err
		}

		c.pending = append(c.pending, raw)
	}

	raw, err := ptp.Container{Type: ptp.ContainerResponse, Code: code, TransactionID: head.TransactionID}.MarshalBinary()
	if err != nil {
		return 0, err
	}

	c.pending = append(c.pending, raw)

	return len(p), nil
}

func (c *camera) ClearHalt() error { return nil }
func (c *camera) Close() error     { return nil }

var eventOps = ptp.DeviceInfo{Operations: []uint16{
	OpEOSSetRemoteMode, OpEOSSetEventMode, OpEOSGetEvent, OpEOSRequestDevicePropValue,
}}

func TestReadEventTable(t *testing.T) {
	replies := [][]byte{
		eventList(propRecord(0xd115), propRecord(0xd167, eosD167...)),
		eventList(),
	}

	cam := &camera{answer: func(op uint16, _ []uint32) ([]byte, uint16) {
		if op != OpEOSGetEvent {
			return nil, ptp.RCOK
		}

		next := replies[0]
		if len(replies) > 1 {
			replies = replies[1:]
		}

		return next, ptp.RCOK
	}}

	table, err := ReadEventTable(ptp.New(cam), eventOps, nil)
	require.NoError(t, err)

	assert.Equal(t, append([]string{enterRemote, enterEvents, getEvent, getEvent}, leaving...), cam.commands)

	value, ok := table.Property(PropShutterReleaseCounter)
	require.True(t, ok)
	assert.Equal(t, eosD167, value)
	assert.Len(t, table.Replies, 2)
}

func TestReadEventTableAsksForD167WhenItIsMissing(t *testing.T) {
	requested := false

	cam := &camera{answer: func(op uint16, params []uint32) ([]byte, uint16) {
		switch op {
		case OpEOSRequestDevicePropValue:
			requested = len(params) == 1 && params[0] == PropShutterReleaseCounter

			return nil, ptp.RCOK
		case OpEOSGetEvent:
			if requested {
				requested = false

				return eventList(propRecord(0xd167, eosD167...)), ptp.RCOK
			}

			return eventList(), ptp.RCOK
		default:
			return nil, ptp.RCOK
		}
	}}

	table, err := ReadEventTable(ptp.New(cam), eventOps, nil)
	require.NoError(t, err)

	assert.Equal(t, append([]string{
		enterRemote, enterEvents, getEvent,
		"0x9127[53607]", getEvent, getEvent,
	}, leaving...), cam.commands)

	_, ok := table.Property(PropShutterReleaseCounter)
	assert.True(t, ok)
}

func TestReadEventTableSendsNothingWithoutTheOperations(t *testing.T) {
	cam := &camera{answer: func(uint16, []uint32) ([]byte, uint16) { return nil, ptp.RCOK }}

	_, err := ReadEventTable(ptp.New(cam), ptp.DeviceInfo{Operations: []uint16{OpEOSSetRemoteMode}}, nil)
	require.ErrorIs(t, err, ErrNoEventChannel)
	assert.Empty(t, cam.commands)
}

func TestReadEventTableLeavesRemoteModeWhenEventModeIsRefused(t *testing.T) {
	cam := &camera{answer: func(op uint16, params []uint32) ([]byte, uint16) {
		if op == OpEOSSetEventMode && params[0] == 1 {
			return nil, ptp.RCDeviceBusy
		}

		return nil, ptp.RCOK
	}}

	_, err := ReadEventTable(ptp.New(cam), eventOps, nil)
	require.Error(t, err)

	assert.Equal(t, append([]string{enterRemote, enterEvents}, leaving...), cam.commands)
}

func TestReadEventTableStopsDrainingACameraThatNeverRunsOut(t *testing.T) {
	cam := &camera{answer: func(op uint16, _ []uint32) ([]byte, uint16) {
		if op == OpEOSGetEvent {
			return eventList(propRecord(0xd167, eosD167...)), ptp.RCOK
		}

		return nil, ptp.RCOK
	}}

	table, err := ReadEventTable(ptp.New(cam), eventOps, nil)
	require.NoError(t, err)
	assert.Len(t, table.Replies, maxEventRounds)
}
