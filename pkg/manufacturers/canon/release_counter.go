package canon

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/DigitalN/openshuttercounter/pkg/manufacturers"
	"github.com/DigitalN/openshuttercounter/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/pkg/usb"
)

// MethodEventChannel is property 0xD167 on Canon's event channel, which
// bodies like the EOS R8 answer in steps of 1,000.
const MethodEventChannel = "canon-event-channel-0xd167"

// releaseCounterLen is 0xD167's size: four 32 bit words.
const releaseCounterLen = 16

// releaseCounterStep is what 0xD167 counts in. Each word is a count rounded
// up to a multiple of it.
const releaseCounterStep = 1000

// eventModel is a body whose 0xD167 words are established.
type eventModel struct {
	Name        string
	ConfirmedOn string
}

// eventModels lists the bodies read through 0xD167. A body goes here only
// once what its words count has been worked out on that body.
//
// On the EOS R8 (2026-11-04, firmware 1.5.0, docs/R8-PROGRESS.md): word 4 is
// every photo, electronic included, rounded up to a thousand. It read 22000
// with a photo's own total at 21,270, and no single-shutter count fits that
// sum. Word 3 is the mechanical shutter: it stepped from 1000 to 2000 across
// 20 Elec. 1st-curtain and 7 Electronic photos, and the only alternative,
// electronic, would put around 20,000 photos on a shutter the owner has barely
// used.
var eventModels = map[uint16]eventModel{
	0x330c: {Name: "EOS R8", ConfirmedOn: "firmware 1.5.0"},
}

// ReleaseCounter is 0xD167 decoded: each count is given to within a range,
// because the camera rounds it up to a thousand.
type ReleaseCounter struct {
	Mechanical Range
	All        Range
	Electronic Range
}

// Range is a count known to be from Min to Max.
type Range struct {
	Min, Max uint32
}

// DecodeReleaseCounter reads 0xD167 the way the EOS R8 lays it out: 16, 1,
// the mechanical count and then every photo, each rounded up to a thousand.
// Anything else is refused rather than read some other way.
func DecodeReleaseCounter(raw []byte) (ReleaseCounter, error) {
	if len(raw) != releaseCounterLen {
		return ReleaseCounter{}, fmt.Errorf("0xd167 is %d bytes, not %d: %s", len(raw), releaseCounterLen, hex.EncodeToString(raw))
	}

	word := func(i int) uint32 { return binary.LittleEndian.Uint32(raw[4*i:]) }

	if word(0) != releaseCounterLen || word(1) != 1 {
		return ReleaseCounter{}, fmt.Errorf("0xd167 doesn't start 16, 1: %s", hex.EncodeToString(raw))
	}

	mechanical, all := word(2), word(3)

	if mechanical%releaseCounterStep != 0 || all%releaseCounterStep != 0 || mechanical > all {
		return ReleaseCounter{}, fmt.Errorf("0xd167 words 3 and 4 aren't two rising multiples of 1,000: %d, %d", mechanical, all)
	}

	out := ReleaseCounter{Mechanical: roundedUp(mechanical), All: roundedUp(all)}

	// Electronic is every photo less the mechanical ones.
	out.Electronic.Max = out.All.Max - out.Mechanical.Min
	if out.All.Min > out.Mechanical.Max {
		out.Electronic.Min = out.All.Min - out.Mechanical.Max
	}

	return out, nil
}

// roundedUp is the range a count rounded up to a thousand came from.
func roundedUp(v uint32) Range {
	if v == 0 {
		return Range{}
	}

	return Range{Min: v - releaseCounterStep + 1, Max: v}
}

// errNoReleaseCounter means the camera's event channel didn't include 0xD167.
var errNoReleaseCounter = errors.New("the camera didn't report property 0xd167")

const releaseCounterNote = `  The camera keeps these counts in thousands, rounded up, so it can only say
  which thousand each is in. A photo's own metadata holds the exact total.`

// readReleaseCounter reads 0xD167 off the event channel.
func (r *Reader) readReleaseCounter(device *ptp.Device, info usb.DeviceInfo, model eventModel) (manufacturers.Reading, error) {
	devInfo, _, err := device.GetDeviceInfo()
	if err != nil {
		return manufacturers.Reading{}, err
	}

	table, err := ReadEventTable(device, devInfo, r.logger)
	if err != nil {
		return manufacturers.Reading{}, fmt.Errorf("reading the event channel: %w", err)
	}

	r.logger.Debug("event channel", zap.Strings("steps", table.Steps))

	raw, ok := table.Property(PropShutterReleaseCounter)
	if !ok {
		return manufacturers.Reading{}, errNoReleaseCounter
	}

	counts, err := DecodeReleaseCounter(raw)
	if err != nil {
		return manufacturers.Reading{}, err
	}

	name := model.Name
	if info.Product != "" && info.Product != "Canon Digital Camera" {
		name = info.Product
	}

	counter := func(label, location string, rng Range) manufacturers.Counter {
		return manufacturers.Counter{
			Label: label, Location: location, Range: true, Min: rng.Min, Value: rng.Max, Raw: raw,
		}
	}

	return manufacturers.Reading{
		Manufacturer: r.Name(),
		Model:        name,
		Confirmed:    true,
		Method:       MethodEventChannel,
		Note:         releaseCounterNote,
		Counters: []manufacturers.Counter{
			counter("Mechanical shutter", "0xd167 word 3", counts.Mechanical),
			counter("Electronic shutter", "word 4 less word 3", counts.Electronic),
			counter("All photos", "0xd167 word 4", counts.All),
		},
	}, nil
}
