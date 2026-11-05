package ptp

import (
	"encoding/binary"
	"fmt"
	"slices"
	"unicode/utf16"
)

// Standard data type codes, PIMA 15740 clause 5.3.
const (
	TypeInt8    uint16 = 0x0001
	TypeUint8   uint16 = 0x0002
	TypeInt16   uint16 = 0x0003
	TypeUint16  uint16 = 0x0004
	TypeInt32   uint16 = 0x0005
	TypeUint32  uint16 = 0x0006
	TypeInt64   uint16 = 0x0007
	TypeUint64  uint16 = 0x0008
	TypeInt128  uint16 = 0x0009
	TypeUint128 uint16 = 0x000a
	TypeString  uint16 = 0xffff

	// typeArray is set on the array form of each integer type.
	typeArray uint16 = 0x4000
)

// DeviceInfo is the GetDeviceInfo dataset: what the camera says it supports.
type DeviceInfo struct {
	StandardVersion        uint16   `json:"standard_version"`
	VendorExtensionID      uint32   `json:"vendor_extension_id"`
	VendorExtensionVersion uint16   `json:"vendor_extension_version"`
	VendorExtensionDesc    string   `json:"vendor_extension_desc,omitempty"`
	FunctionalMode         uint16   `json:"functional_mode"`
	Operations             []uint16 `json:"operations"`
	Events                 []uint16 `json:"events"`
	Properties             []uint16 `json:"properties"`
	CaptureFormats         []uint16 `json:"capture_formats"`
	ImageFormats           []uint16 `json:"image_formats"`
	Manufacturer           string   `json:"manufacturer,omitempty"`
	Model                  string   `json:"model,omitempty"`
	DeviceVersion          string   `json:"device_version,omitempty"`
	SerialNumber           string   `json:"serial_number,omitempty"`
}

// Supports reports whether the camera lists an operation.
func (i DeviceInfo) Supports(opcode uint16) bool {
	return slices.Contains(i.Operations, opcode)
}

// ParseDeviceInfo reads a GetDeviceInfo dataset.
func ParseDeviceInfo(b []byte) (DeviceInfo, error) {
	r := &dataset{b: b}

	info := DeviceInfo{
		StandardVersion:        r.u16(),
		VendorExtensionID:      r.u32(),
		VendorExtensionVersion: r.u16(),
		VendorExtensionDesc:    r.str(),
		FunctionalMode:         r.u16(),
		Operations:             r.u16Array(),
		Events:                 r.u16Array(),
		Properties:             r.u16Array(),
		CaptureFormats:         r.u16Array(),
		ImageFormats:           r.u16Array(),
		Manufacturer:           r.str(),
		Model:                  r.str(),
		DeviceVersion:          r.str(),
		SerialNumber:           r.str(),
	}

	if r.err != nil {
		return info, fmt.Errorf("parsing the device info: %w", r.err)
	}

	return info, nil
}

// PropDesc is the part of a GetDevicePropDesc dataset worth comparing.
type PropDesc struct {
	Code     uint16
	DataType uint16
	Writable bool
	// Current is the property's current value, raw.
	Current []byte
}

// ParsePropDesc reads a GetDevicePropDesc dataset up to the current value.
func ParsePropDesc(b []byte) (PropDesc, error) {
	r := &dataset{b: b}

	desc := PropDesc{Code: r.u16(), DataType: r.u16()}
	desc.Writable = r.u8() == 1

	r.value(desc.DataType) // the factory default, not needed
	desc.Current = append([]byte(nil), r.value(desc.DataType)...)

	if r.err != nil {
		return desc, fmt.Errorf("parsing the description of property %#04x: %w", desc.Code, r.err)
	}

	return desc, nil
}

// TypeName names a data type code.
func TypeName(t uint16) string {
	names := map[uint16]string{
		TypeInt8: "INT8", TypeUint8: "UINT8", TypeInt16: "INT16", TypeUint16: "UINT16",
		TypeInt32: "INT32", TypeUint32: "UINT32", TypeInt64: "INT64", TypeUint64: "UINT64",
		TypeInt128: "INT128", TypeUint128: "UINT128", TypeString: "STR",
	}

	if name, ok := names[t]; ok {
		return name
	}

	if t&typeArray != 0 {
		if name, ok := names[t&^typeArray]; ok {
			return "A" + name
		}
	}

	return fmt.Sprintf("%#04x", t)
}

// String reads a value of type STR.
func String(raw []byte) string {
	return (&dataset{b: raw}).str()
}

// Number reads a value of an integer type of up to 64 bits.
func Number(t uint16, raw []byte) (int64, bool) {
	if scalarSize(t) != len(raw) || len(raw) > 8 {
		return 0, false
	}

	var u uint64

	for _, c := range slices.Backward(raw) {
		u = u<<8 | uint64(c)
	}

	switch t {
	case TypeInt8:
		return int64(int8(u)), true //nolint:gosec // the value is one byte wide
	case TypeInt16:
		return int64(int16(u)), true //nolint:gosec // the value is two bytes wide
	case TypeInt32:
		return int64(int32(u)), true //nolint:gosec // the value is four bytes wide
	default:
		return int64(u), true //nolint:gosec // a UINT64 above MaxInt64 isn't a counter
	}
}

// scalarSize is the width of an integer type, or 0 for anything else.
func scalarSize(t uint16) int {
	switch t {
	case TypeInt8, TypeUint8:
		return 1
	case TypeInt16, TypeUint16:
		return 2
	case TypeInt32, TypeUint32:
		return 4
	case TypeInt64, TypeUint64:
		return 8
	case TypeInt128, TypeUint128:
		return 16
	default:
		return 0
	}
}

// dataset reads the little endian fields PTP datasets are made of. The first
// field that runs past the end sets err, and every read after it is a no-op.
type dataset struct {
	b   []byte
	off int
	err error
}

func (r *dataset) take(n int) []byte {
	if r.err != nil {
		return nil
	}

	if n < 0 || n > len(r.b)-r.off {
		r.err = fmt.Errorf("dataset is %d bytes, a field at offset %d needs %d", len(r.b), r.off, n)

		return nil
	}

	out := r.b[r.off : r.off+n]
	r.off += n

	return out
}

func (r *dataset) u8() uint8 {
	if b := r.take(1); b != nil {
		return b[0]
	}

	return 0
}

func (r *dataset) u16() uint16 {
	if b := r.take(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}

	return 0
}

func (r *dataset) u32() uint32 {
	if b := r.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}

	return 0
}

// count reads an array length, refusing one longer than what's left.
func (r *dataset) count(elementSize int) int {
	n := r.u32()
	if r.err != nil {
		return 0
	}

	if elementSize <= 0 || int64(n) > int64((len(r.b)-r.off)/elementSize) {
		r.err = fmt.Errorf("array of %d elements at offset %d runs past the end", n, r.off)

		return 0
	}

	return int(n)
}

func (r *dataset) u16Array() []uint16 {
	n := r.count(2)
	out := make([]uint16, n)

	for i := range out {
		out[i] = r.u16()
	}

	return out
}

// str reads a PTP string: a character count including the terminating NUL,
// then that many UTF-16LE code units.
func (r *dataset) str() string {
	n := int(r.u8())
	raw := r.take(2 * n)

	units := make([]uint16, 0, n)

	for i := 0; i+2 <= len(raw); i += 2 {
		u := binary.LittleEndian.Uint16(raw[i:])
		if u == 0 {
			break
		}

		units = append(units, u)
	}

	return string(utf16.Decode(units))
}

// value reads one value of the given type and returns its raw bytes.
func (r *dataset) value(t uint16) []byte {
	start := r.off

	switch {
	case t == TypeString:
		r.take(2 * int(r.u8()))
	case scalarSize(t) > 0:
		r.take(scalarSize(t))
	case t&typeArray != 0 && scalarSize(t&^typeArray) > 0:
		size := scalarSize(t &^ typeArray)
		r.take(size * r.count(size))
	default:
		if r.err == nil {
			r.err = fmt.Errorf("unknown data type %#04x", t)
		}
	}

	if r.err != nil {
		return nil
	}

	return r.b[start:r.off]
}

// ScrubSerialNumber blanks the serial number in a GetDeviceInfo dataset, in
// place, with an x for each character, and returns what it was. The dataset
// still parses; its serial number then reads xxxx. A dataset that doesn't
// parse as far as the serial number is left alone.
func ScrubSerialNumber(b []byte) string {
	r := &dataset{b: b}

	r.u16()
	r.u32()
	r.u16()
	r.str()
	r.u16()

	for range 5 { // operations, events, properties, capture and image formats
		r.u16Array()
	}

	for range 3 { // manufacturer, model, device version
		r.str()
	}

	if r.err != nil || r.off >= len(b) {
		return ""
	}

	return BlankString(b[r.off:])
}

// BlankString blanks the PTP string at the start of b, in place, with an x
// for each character, and returns what it was. Its length and terminating
// NUL are kept, so whatever holds it still parses. b is left alone if it
// doesn't start with a whole PTP string.
func BlankString(b []byte) string {
	r := &dataset{b: b}

	s := r.str()
	if r.err != nil {
		return ""
	}

	for i := 1; i+2 <= r.off; i += 2 {
		if binary.LittleEndian.Uint16(b[i:]) != 0 {
			b[i], b[i+1] = 'x', 0
		}
	}

	return s
}

// StringLen is how many bytes the PTP string at the start of b takes, or -1
// if b doesn't start with a whole one.
func StringLen(b []byte) int {
	r := &dataset{b: b}
	r.str()

	if r.err != nil {
		return -1
	}

	return r.off
}

// MarshalBinary renders the dataset the way a camera sends it, which is what
// tests of anything that reads one need.
func (i DeviceInfo) MarshalBinary() ([]byte, error) {
	var b []byte

	b = binary.LittleEndian.AppendUint16(b, i.StandardVersion)
	b = binary.LittleEndian.AppendUint32(b, i.VendorExtensionID)
	b = binary.LittleEndian.AppendUint16(b, i.VendorExtensionVersion)
	b = append(b, MarshalString(i.VendorExtensionDesc)...)
	b = binary.LittleEndian.AppendUint16(b, i.FunctionalMode)

	for _, array := range [][]uint16{i.Operations, i.Events, i.Properties, i.CaptureFormats, i.ImageFormats} {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(array))) //nolint:gosec // a dataset's arrays are small
		for _, v := range array {
			b = binary.LittleEndian.AppendUint16(b, v)
		}
	}

	for _, s := range []string{i.Manufacturer, i.Model, i.DeviceVersion, i.SerialNumber} {
		if len(utf16.Encode([]rune(s))) > maxStringUnits {
			return nil, fmt.Errorf("%q is too long for a PTP string", s)
		}

		b = append(b, MarshalString(s)...)
	}

	return b, nil
}

// maxStringUnits is the most characters a PTP string holds, its NUL aside.
const maxStringUnits = 254

// MarshalString renders a PTP string: a character count including the
// terminating NUL, then the characters in UTF-16LE. The empty string is a
// lone zero. A string longer than a PTP string holds is cut short.
func MarshalString(s string) []byte {
	units := utf16.Encode([]rune(s))
	if len(units) == 0 {
		return []byte{0}
	}

	units = units[:min(len(units), maxStringUnits)]

	out := []byte{byte(len(units) + 1)} //nolint:gosec // at most maxStringUnits, cut above
	for _, u := range units {
		out = binary.LittleEndian.AppendUint16(out, u)
	}

	return append(out, 0, 0)
}
