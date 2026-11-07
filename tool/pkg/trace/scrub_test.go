package trace

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/ptp"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/usb"
)

const serial = "0123456789abcdef0123456789abcdef"

func record(r *Recorder, tx uint32, opcode uint16, params []uint32, data []byte) {
	r.Record(ptp.DirectionOut, ptp.Container{
		Type: ptp.ContainerCommand, Code: opcode, TransactionID: tx, Payload: ptp.MarshalParams(params...),
	})

	if data != nil {
		r.Record(ptp.DirectionIn, ptp.Container{Type: ptp.ContainerData, Code: opcode, TransactionID: tx, Payload: data})
	}

	r.Record(ptp.DirectionIn, ptp.Container{Type: ptp.ContainerResponse, Code: ptp.RCOK, TransactionID: tx})
}

func payloads(t *testing.T, file File) string {
	t.Helper()

	var all strings.Builder

	for _, e := range file.Entries {
		b, err := e.Bytes()
		require.NoError(t, err)
		all.Write(b)
	}

	return all.String()
}

func TestTheSerialNumberIsLeftOutOfATrace(t *testing.T) {
	info, err := ptp.DeviceInfo{
		Manufacturer: "Canon.Inc", Model: "Canon EOS R8", DeviceVersion: "3-1.5.0", SerialNumber: serial,
	}.MarshalBinary()
	require.NoError(t, err)

	r := NewRecorder("v1")
	r.Describe(usb.DeviceInfo{VendorID: 0x04a9, ProductID: 0x330c, Serial: "USB-SERIAL-1"})
	r.Annotate("serial " + serial)
	record(r, 1, ptp.OpGetDeviceInfo, nil, info)
	// The same serial anywhere else, as text and as UTF-16.
	record(r, 2, 0x9116, nil, append([]byte("x"+serial+"\x00"), ptp.MarshalString(serial)...))

	file := r.File()

	assert.Empty(t, file.Device.Serial)
	assert.NotContains(t, file.Notes, serial)
	assert.NotContains(t, payloads(t, file), serial)
	assert.NotContains(t, payloads(t, file), string(ptp.MarshalString(serial)[1:]))

	b, err := file.Entries[1].Bytes()
	require.NoError(t, err)

	parsed, err := ptp.ParseDeviceInfo(b)
	require.NoError(t, err, "the scrubbed dataset still parses")
	assert.Equal(t, strings.Repeat("x", len(serial)), parsed.SerialNumber)
	assert.Equal(t, "Canon EOS R8", parsed.Model)
	assert.Equal(t, "3-1.5.0", parsed.DeviceVersion)

	// What the recorder keeps is untouched; only what it gives out is.
	assert.Contains(t, r.file.Notes, serial)
}

func TestScrubbersFindWhatsInTheirOwnCommands(t *testing.T) {
	r := NewRecorder("v1", func(opcode uint16, _ []uint32, data []byte) []string {
		if opcode != 0x9116 {
			return nil
		}

		return []string{BlankBytes(data[:12])}
	})
	record(r, 1, 0x9116, nil, []byte("072022001307\x00 and the rest"))
	record(r, 2, 0x9999, nil, []byte("again 072022001307"))

	all := payloads(t, r.File())
	assert.NotContains(t, all, "072022001307")
	assert.Contains(t, all, "and the rest")
	assert.Contains(t, all, "again xxxxxxxxxxxx")
}

func TestArtistAndCopyrightAreBlanked(t *testing.T) {
	r := NewRecorder("v1")
	record(r, 1, ptp.OpGetDevicePropValue, []uint32{uint32(PropArtist)}, ptp.MarshalString("Jane Doe"))

	desc := append([]byte{0x1f, 0x50, 0xff, 0xff, 1}, ptp.MarshalString("")...)
	desc = append(desc, ptp.MarshalString("(c) Jane Doe")...)
	record(r, 2, ptp.OpGetDevicePropDesc, []uint32{uint32(PropCopyrightInfo)}, desc)

	record(r, 3, ptp.OpGetDevicePropValue, []uint32{0x5001}, []byte{0x64})

	file := r.File()
	all := payloads(t, file)
	assert.NotContains(t, all, "J\x00a\x00n\x00e")

	b, err := file.Entries[len(file.Entries)-2].Bytes()
	require.NoError(t, err)
	assert.Equal(t, []byte{0x64}, b, "other properties are left alone")
}

func TestBlankValues(t *testing.T) {
	b := ptp.MarshalString("Jane")
	assert.Equal(t, []string{"Jane"}, BlankValues(b))
	assert.Equal(t, ptp.MarshalString("xxxx"), b)

	raw := []byte("Jane\x00\x00")
	assert.Equal(t, []string{"Jane"}, BlankValues(raw))
	assert.Equal(t, []byte("xxxx\x00\x00"), raw)

	number := binary.LittleEndian.AppendUint32(nil, 0x01020300)
	BlankValues(number)
	assert.Equal(t, "00787878", hex.EncodeToString(number))
}

func TestReadFindsTheTraceInALog(t *testing.T) {
	var buf strings.Builder

	require.NoError(t, recordConversation(t).Write(&buf))

	log := "Open Shutter Count 0.5.0\nResult: read\n\n" + LogBegin + "\n" + buf.String() + LogEnd + "\n"

	file, err := Read([]byte(log))
	require.NoError(t, err)
	assert.Len(t, file.Entries, 3)
	assert.Equal(t, "v1.2.3", file.ToolVersion)
}
