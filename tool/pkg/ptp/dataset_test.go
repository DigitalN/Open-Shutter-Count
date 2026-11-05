package ptp

import (
	"encoding/binary"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptpString(s string) []byte {
	if s == "" {
		return []byte{0}
	}

	out := []byte{byte(len(s) + 1)} //nolint:gosec // test strings are short
	for _, r := range s {
		out = binary.LittleEndian.AppendUint16(out, uint16(r)) //nolint:gosec // test strings are ASCII
	}

	return append(out, 0, 0)
}

func u16Array(values ...uint16) []byte {
	out := binary.LittleEndian.AppendUint32(nil, uint32(len(values))) //nolint:gosec // a handful of test values
	for _, v := range values {
		out = binary.LittleEndian.AppendUint16(out, v)
	}

	return out
}

func TestParseDeviceInfo(t *testing.T) {
	var b []byte
	b = binary.LittleEndian.AppendUint16(b, 100)
	b = binary.LittleEndian.AppendUint32(b, 11)
	b = binary.LittleEndian.AppendUint16(b, 100)
	b = append(b, ptpString("Canon")...)
	b = binary.LittleEndian.AppendUint16(b, 0)
	b = append(b, u16Array(0x1001, 0x1002, 0x1014)...)
	b = append(b, u16Array(0x4002)...)
	b = append(b, u16Array(0x5001, 0xd402)...)
	b = append(b, u16Array()...)
	b = append(b, u16Array(0x3801)...)
	b = append(b, ptpString("Canon Inc.")...)
	b = append(b, ptpString("Canon EOS R8")...)
	b = append(b, ptpString("1-1.4.0")...)
	b = append(b, ptpString("")...)

	info, err := ParseDeviceInfo(b)
	require.NoError(t, err)

	assert.Equal(t, uint32(11), info.VendorExtensionID)
	assert.Equal(t, "Canon", info.VendorExtensionDesc)
	assert.Equal(t, []uint16{0x1001, 0x1002, 0x1014}, info.Operations)
	assert.Equal(t, []uint16{0x5001, 0xd402}, info.Properties)
	assert.Empty(t, info.CaptureFormats)
	assert.Equal(t, "Canon EOS R8", info.Model)
	assert.Equal(t, "1-1.4.0", info.DeviceVersion)
	assert.True(t, info.Supports(0x1014))
	assert.False(t, info.Supports(0x1015))
}

func TestParseDeviceInfoRefusesATruncatedDataset(t *testing.T) {
	var b []byte
	b = binary.LittleEndian.AppendUint16(b, 100)
	b = binary.LittleEndian.AppendUint32(b, 11)
	b = binary.LittleEndian.AppendUint16(b, 100)
	b = append(b, 0)
	b = binary.LittleEndian.AppendUint16(b, 0)
	b = binary.LittleEndian.AppendUint32(b, 1000) // 1000 operations, none present

	_, err := ParseDeviceInfo(b)
	require.Error(t, err)
}

func TestParsePropDesc(t *testing.T) {
	// BatteryLevel, UINT8, read only, default 0, current 80, range form.
	b := []byte{0x01, 0x50, 0x02, 0x00, 0x00, 0x00, 80, 0x01, 0, 100, 1}

	desc, err := ParsePropDesc(b)
	require.NoError(t, err)

	assert.Equal(t, uint16(0x5001), desc.Code)
	assert.Equal(t, "UINT8", TypeName(desc.DataType))
	assert.False(t, desc.Writable)
	assert.Equal(t, []byte{80}, desc.Current)

	n, ok := Number(desc.DataType, desc.Current)
	require.True(t, ok)
	assert.Equal(t, int64(80), n)
}

func TestParsePropDescString(t *testing.T) {
	b := slices.Concat([]byte{0x02, 0xd4, 0xff, 0xff, 0x01}, ptpString(""), ptpString("EOS R8"), []byte{0x00})

	desc, err := ParsePropDesc(b)
	require.NoError(t, err)

	assert.True(t, desc.Writable)
	assert.Equal(t, "STR", TypeName(desc.DataType))
	assert.Equal(t, "EOS R8", String(desc.Current))
}

func TestParsePropDescArrayAndSignedNumbers(t *testing.T) {
	// INT16 current value -2.
	b := []byte{0x10, 0x50, 0x03, 0x00, 0x00, 0x00, 0x00, 0xfe, 0xff, 0x00}

	desc, err := ParsePropDesc(b)
	require.NoError(t, err)

	n, ok := Number(desc.DataType, desc.Current)
	require.True(t, ok)
	assert.Equal(t, int64(-2), n)

	// AUINT16 with two elements, default empty.
	b = slices.Concat([]byte{0x99, 0xd1, 0x04, 0x40, 0x00}, u16Array(), u16Array(7, 9))

	desc, err = ParsePropDesc(b)
	require.NoError(t, err)

	assert.Equal(t, "AUINT16", TypeName(desc.DataType))
	assert.Len(t, desc.Current, 4+2*2)

	_, ok = Number(desc.DataType, desc.Current)
	assert.False(t, ok)
}
