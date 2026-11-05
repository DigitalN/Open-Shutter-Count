package canon

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"go.uber.org/zap"

	"github.com/DigitalN/openshuttercounter/tool/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/tool/pkg/trace"
)

// Canon's EOS tethering operations. EOS Utility, Lightroom and libgphoto2 send
// these every time they connect to an EOS body. Remote and event mode last
// only as long as the connection: they change no stored setting.
//
// These are the only ones sent here. The event channel is read with them and
// nothing else, so adding one means asking the maintainer first; see
// .claude/CLAUDE.md.
const (
	OpEOSSetRemoteMode          uint16 = 0x9114
	OpEOSSetEventMode           uint16 = 0x9115
	OpEOSGetEvent               uint16 = 0x9116
	OpEOSRequestDevicePropValue uint16 = 0x9127
)

// Event record codes in a GetEvent reply.
const (
	// EventPropValueChanged carries a property code and its current value.
	EventPropValueChanged uint32 = 0xc189

	// EventAvailListChanged carries a property code and its allowed values.
	EventAvailListChanged uint32 = 0xc18a
)

// PropShutterReleaseCounter is EOS property 0xD167, which libgphoto2 names
// EOS_ShutterReleaseCounter. In libgphoto2's EOS R and M50 Mark II dumps it is
// 16 bytes, four 32 bit words: 16, 1, then two multiples of 1,000. What those
// two words count is not established.
const PropShutterReleaseCounter uint32 = 0xd167

// ErrNoEventChannel means the camera doesn't list the operations needed to
// read its event channel. Nothing was sent to it.
var ErrNoEventChannel = errors.New("the camera doesn't list Canon's event channel operations")

// maxEventRounds bounds how many GetEvent requests one drain sends. A camera
// replies with an empty list once it has nothing more to report; libgphoto2
// stops there too. The bound only matters if one never does.
const maxEventRounds = 20

// recordHeaderLen is a record's size and code words.
const recordHeaderLen = 8

// EventRecord is one record in a GetEvent reply.
type EventRecord struct {
	// Code is the record type, such as EventPropValueChanged.
	Code uint32

	// Prop is the property code, for property records. Zero otherwise.
	Prop uint32

	// Data is what follows the code, or the property code for property
	// records: a property's current value, say.
	Data []byte
}

// IsProperty reports whether the record describes a property.
func (r EventRecord) IsProperty() bool {
	return r.Code == EventPropValueChanged || r.Code == EventAvailListChanged
}

// ParseEvents reads the records in a GetEvent reply. Each is a 32 bit size,
// counting itself, then a 32 bit code, then data; the list ends with an 8 byte
// record of code 0. That layout is libgphoto2's (ptp_unpack_EOS_events).
func ParseEvents(b []byte) ([]EventRecord, error) {
	var records []EventRecord

	for off := 0; ; {
		if len(b)-off < recordHeaderLen {
			return records, fmt.Errorf("event list has %d bytes left at byte %d, too few for a record", len(b)-off, off)
		}

		size := int(binary.LittleEndian.Uint32(b[off:]))
		code := binary.LittleEndian.Uint32(b[off+4:])

		if size == recordHeaderLen && code == 0 {
			return records, nil
		}

		if size < recordHeaderLen || size > len(b)-off {
			return records, fmt.Errorf("event record at byte %d says it is %d bytes, with %d left", off, size, len(b)-off)
		}

		rec := EventRecord{Code: code, Data: append([]byte(nil), b[off+recordHeaderLen:off+size]...)}

		if rec.IsProperty() && len(rec.Data) >= 4 {
			rec.Prop = binary.LittleEndian.Uint32(rec.Data)
			rec.Data = rec.Data[4:]
		}

		records = append(records, rec)
		off += size
	}
}

// EventTable is what a camera reported on its event channel in one connection.
type EventTable struct {
	// Replies are the GetEvent data phases, in the order they arrived.
	Replies [][]byte

	// Records are every record from Replies, in order.
	Records []EventRecord

	// Steps says what was sent and how the camera answered.
	Steps []string
}

// Property returns the last value the camera reported for a property.
func (t *EventTable) Property(prop uint32) ([]byte, bool) {
	for _, r := range slices.Backward(t.Records) {
		if r.Code == EventPropValueChanged && r.Prop == prop {
			return r.Data, true
		}
	}

	return nil, false
}

// ReadEventTable reads everything a camera reports on Canon's event channel.
//
// It sends SetRemoteMode(1) and SetEventMode(1), then GetEvent until the
// camera has nothing more to report. If property 0xD167 wasn't among it, it
// asks for it with RequestDevicePropValue and drains again. On the way out it
// does what libgphoto2 does when it stops tethering: SetRemoteMode(0), then
// SetRemoteMode(1), which it notes switches the display back on, then
// SetEventMode(0).
//
// Nothing is sent unless the camera lists SetRemoteMode, SetEventMode and
// GetEvent. RequestDevicePropValue is sent only if it lists that too.
func ReadEventTable(dev *ptp.Device, info ptp.DeviceInfo, logger *zap.Logger) (EventTable, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	var table EventTable

	for _, op := range []uint16{OpEOSSetRemoteMode, OpEOSSetEventMode, OpEOSGetEvent} {
		if !info.Supports(op) {
			return table, fmt.Errorf("%w: it doesn't list %#04x", ErrNoEventChannel, op)
		}
	}

	if err := table.send(dev, "SetRemoteMode(1)", OpEOSSetRemoteMode, 1); err != nil {
		return table, err
	}

	err := table.readEvents(dev, info, logger)

	if exitErr := table.leave(dev); exitErr != nil {
		if err == nil {
			return table, exitErr
		}

		logger.Debug("leaving remote mode", zap.Error(exitErr))
	}

	return table, err
}

// readEvents is ReadEventTable once remote mode is on.
func (t *EventTable) readEvents(dev *ptp.Device, info ptp.DeviceInfo, logger *zap.Logger) error {
	if err := t.send(dev, "SetEventMode(1)", OpEOSSetEventMode, 1); err != nil {
		return err
	}

	if err := t.drain(dev); err != nil {
		return err
	}

	if _, ok := t.Property(PropShutterReleaseCounter); ok {
		return nil
	}

	if !info.Supports(OpEOSRequestDevicePropValue) {
		t.Steps = append(t.Steps, "0xd167 wasn't reported, and the camera doesn't list RequestDevicePropValue")

		return nil
	}

	// A camera without the property answers GeneralError, which libgphoto2
	// logs and carries on from; so does this.
	name := fmt.Sprintf("RequestDevicePropValue(%#04x)", PropShutterReleaseCounter)

	resp, err := dev.Transact(OpEOSRequestDevicePropValue, PropShutterReleaseCounter)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	t.Steps = append(t.Steps, name+": "+ptp.ResponseName(resp.Code))

	if !resp.OK() {
		logger.Debug("the camera refused the property request", zap.String("response", ptp.ResponseName(resp.Code)))

		return nil
	}

	return t.drain(dev)
}

// drain sends GetEvent until the camera reports nothing.
func (t *EventTable) drain(dev *ptp.Device) error {
	for range maxEventRounds {
		resp, err := dev.Transact(OpEOSGetEvent)
		if err != nil {
			return fmt.Errorf("GetEvent: %w", err)
		}

		if err := resp.Err(); err != nil {
			return fmt.Errorf("GetEvent: %w", err)
		}

		t.Replies = append(t.Replies, resp.Data)

		records, parseErr := ParseEvents(resp.Data)
		t.Records = append(t.Records, records...)
		t.Steps = append(t.Steps, fmt.Sprintf("GetEvent: %d bytes, %d records", len(resp.Data), len(records)))

		if parseErr != nil {
			// The raw reply is kept, so a layout this doesn't understand
			// can still be worked out from the file.
			t.Steps = append(t.Steps, "  "+parseErr.Error())
		}

		if len(records) == 0 {
			return nil
		}
	}

	t.Steps = append(t.Steps, fmt.Sprintf("stopped after %d GetEvent requests", maxEventRounds))

	return nil
}

// leave restores the camera the way libgphoto2 does when it stops tethering.
// Each step is tried even if one before it was refused.
func (t *EventTable) leave(dev *ptp.Device) error {
	var errs []error

	for _, step := range []struct {
		name  string
		op    uint16
		param uint32
	}{
		{"SetRemoteMode(0)", OpEOSSetRemoteMode, 0},
		{"SetRemoteMode(1)", OpEOSSetRemoteMode, 1},
		{"SetEventMode(0)", OpEOSSetEventMode, 0},
	} {
		if err := t.send(dev, step.name, step.op, step.param); err != nil {
			errs = append(errs, err)

			// A transport failure means the camera has stopped answering,
			// and sending more won't help.
			if !isRefusal(err) {
				break
			}
		}
	}

	return errors.Join(errs...)
}

// send runs an operation with no data phase and records how it went.
func (t *EventTable) send(dev *ptp.Device, name string, op uint16, param uint32) error {
	if !slices.Contains([]uint16{OpEOSSetRemoteMode, OpEOSSetEventMode}, op) {
		return fmt.Errorf("refusing to send %#04x: only the event channel operations are sent here", op)
	}

	resp, err := dev.Transact(op, param)
	if err != nil {
		t.Steps = append(t.Steps, name+": "+err.Error())

		return fmt.Errorf("%s: %w", name, err)
	}

	t.Steps = append(t.Steps, name+": "+ptp.ResponseName(resp.Code))

	if err := resp.Err(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	return nil
}

func isRefusal(err error) bool {
	var refusal *ptp.ResponseError

	return errors.As(err, &refusal)
}

// IdentifyingProperties are the Canon EOS properties that identify the camera
// or its owner, as libgphoto2's ptp.h names them: Owner 0xD115,
// CameraNickname 0xD125, SerialNumber 0xD1AF (not the one GetDeviceInfo
// gives), Artist 0xD1D0 and Copyright 0xD1D1. A trace has them blanked.
var IdentifyingProperties = []uint16{0xd115, 0xd125, 0xd1af, 0xd1d0, 0xd1d1}

// ScrubEvents is a trace.Scrubber for GetEvent replies. It blanks, in place,
// the values of IdentifyingProperties, with an x for every byte that isn't
// NUL, and returns what they were.
func ScrubEvents(opcode uint16, _ []uint32, data []byte) []string {
	if opcode != OpEOSGetEvent {
		return nil
	}

	var found []string

	for off := 0; len(data)-off >= recordHeaderLen; {
		size := int(binary.LittleEndian.Uint32(data[off:]))
		code := binary.LittleEndian.Uint32(data[off+4:])

		if size < recordHeaderLen || size > len(data)-off {
			break
		}

		const propHeader = recordHeaderLen + 4

		if code == EventPropValueChanged && size >= propHeader {
			prop := binary.LittleEndian.Uint32(data[off+recordHeaderLen:])

			if prop <= 0xffff && slices.Contains(IdentifyingProperties, uint16(prop)) {
				found = append(found, trace.BlankBytes(data[off+propHeader:off+size]))
			}
		}

		off += size
	}

	return found
}
