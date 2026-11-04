package trace

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/DigitalN/openshuttercounter/pkg/ptp"
)

// A Scrubber blanks, in place, whatever in one data phase identifies the
// camera or its owner, such as a serial number, and returns the strings it
// blanked. Those are then blanked wherever else they turn up in the trace.
//
// opcode is the operation the data phase belongs to, and params the
// parameters its command carried.
type Scrubber func(opcode uint16, params []uint32, data []byte) []string

// Standard PTP properties that hold the owner's name. libgphoto2 reads them
// as /main/settings/artist and /main/settings/copyright.
const (
	PropArtist        uint16 = 0x501e
	PropCopyrightInfo uint16 = 0x501f
)

// minSecretLen is the shortest string blanked everywhere it appears. Anything
// shorter would blank bytes that only happen to match it; it's still blanked
// where a Scrubber found it.
const minSecretLen = 4

// ScrubDeviceInfo blanks the serial number in a GetDeviceInfo reply.
func ScrubDeviceInfo(opcode uint16, _ []uint32, data []byte) []string {
	if opcode != ptp.OpGetDeviceInfo {
		return nil
	}

	return []string{ptp.ScrubSerialNumber(data)}
}

// ScrubProperties blanks the values of the given properties wherever they're
// read with GetDevicePropValue or GetDevicePropDesc.
func ScrubProperties(codes ...uint16) Scrubber {
	return func(opcode uint16, params []uint32, data []byte) []string {
		if len(params) == 0 || !slices.Contains(codes, uint16(params[0])) { //nolint:gosec // property codes are 16 bits
			return nil
		}

		switch opcode {
		case ptp.OpGetDevicePropValue:
			return BlankValues(data)
		case ptp.OpGetDevicePropDesc:
			// The code, the data type and the get/set flag come first.
			const descHeader = 5
			if len(data) <= descHeader {
				return nil
			}

			return BlankValues(data[descHeader:])
		default:
			return nil
		}
	}
}

// BlankValues blanks a property value, in place, and returns what it held.
// One or more PTP strings, as a description holds, keep their lengths and
// read as xxxx. Anything else has every byte that isn't NUL replaced with x,
// which is how a Canon body sends a string on its event channel.
func BlankValues(b []byte) []string {
	var lengths []int

	for off := 0; off < len(b); {
		n := ptp.StringLen(b[off:])
		if n < 1 {
			return []string{BlankBytes(b)}
		}

		lengths = append(lengths, n)
		off += n
	}

	found := make([]string, 0, len(lengths))

	for off, i := 0, 0; i < len(lengths); off, i = off+lengths[i], i+1 {
		found = append(found, ptp.BlankString(b[off:]))
	}

	return found
}

// BlankBytes replaces every byte of b that isn't NUL with x, in place, and
// returns what b held up to its first NUL.
func BlankBytes(b []byte) string {
	text := string(b)
	if i := strings.IndexByte(text, 0); i >= 0 {
		text = text[:i]
	}

	for i, c := range b {
		if c != 0 {
			b[i] = 'x'
		}
	}

	return text
}

// scrub returns a copy of file with what identifies the camera or its owner
// blanked: the USB serial number, whatever the scrubbers find, and every
// other place those strings turn up, as ASCII or as UTF-16.
func scrub(file File, scrubbers []Scrubber) File {
	secrets := []string{file.Device.Serial}
	file.Device.Serial = ""

	params := map[uint32][]uint32{}
	payloads := make([][]byte, len(file.Entries))

	for i, e := range file.Entries {
		payloads[i], _ = hex.DecodeString(e.Payload)

		if e.Type == ptp.ContainerCommand {
			params[e.Transaction] = ptp.Container{Payload: payloads[i]}.Params()
		}
	}

	for i, e := range file.Entries {
		if e.Type != ptp.ContainerData || len(payloads[i]) == 0 {
			continue
		}

		for _, s := range scrubbers {
			secrets = append(secrets, s(e.Opcode, params[e.Transaction], payloads[i])...)
		}
	}

	patterns := secretPatterns(secrets)

	out := file
	out.Entries = make([]Entry, len(file.Entries))

	for i, e := range file.Entries {
		for _, p := range patterns {
			payloads[i] = bytes.ReplaceAll(payloads[i], p.find, p.blank)
		}

		e.Payload = hex.EncodeToString(payloads[i])
		out.Entries[i] = e
	}

	for _, s := range secrets {
		if len(strings.TrimSpace(s)) >= minSecretLen {
			out.Notes = strings.ReplaceAll(out.Notes, s, strings.Repeat("x", len(s)))
		}
	}

	return out
}

type pattern struct{ find, blank []byte }

// secretPatterns is each secret long enough to look for, as ASCII and as
// UTF-16LE, with what replaces it.
func secretPatterns(secrets []string) []pattern {
	var out []pattern

	seen := map[string]bool{}

	for _, s := range secrets {
		if len(strings.TrimSpace(s)) < minSecretLen || seen[s] {
			continue
		}

		seen[s] = true

		ascii := []byte(s)
		out = append(out, pattern{find: ascii, blank: bytes.Repeat([]byte{'x'}, len(ascii))})

		units := utf16.Encode([]rune(s))
		wide := make([]byte, 0, 2*len(units))
		blank := make([]byte, 0, 2*len(units))

		for _, u := range units {
			wide = binary.LittleEndian.AppendUint16(wide, u)
			blank = append(blank, 'x', 0)
		}

		out = append(out, pattern{find: wide, blank: blank})
	}

	return out
}
