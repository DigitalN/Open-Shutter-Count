package canon

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/DigitalN/openshuttercounter/tool/pkg/manufacturers"
	"github.com/DigitalN/openshuttercounter/tool/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/tool/pkg/usb"
)

// How a reading was obtained, reported so that anyone comparing two readings
// knows whether they came from the same place.
const (
	// MethodStandardProperty is the standard PTP shutter counter property,
	// which bodies up to roughly the DIGIC 5 generation still answer.
	MethodStandardProperty = "ptp-property-0xd1ac"

	// MethodFactoryChannel is the maker specific channel that reaches the
	// MPU's own memory, read at a body's own address map or, on a body that
	// lists it and has none, at the 5D Mark IV's.
	MethodFactoryChannel = "canon-factory-channel"
)

// Reader reads actuation counters from Canon EOS cameras.
type Reader struct {
	logger *zap.Logger

	// factoryOnly skips the standard property and goes straight to the
	// factory channel. Useful when comparing the two on a body that
	// answers both.
	factoryOnly bool

	// tested decides which readings say tested, and which failed reads are
	// ones that can't be read yet. Nothing else.
	tested TestedList
}

// Option configures a Reader.
type Option func(*Reader)

// WithLogger attaches a logger.
func WithLogger(l *zap.Logger) Option {
	return func(r *Reader) {
		if l != nil {
			r.logger = l
		}
	}
}

// WithFactoryOnly skips the standard property probe.
func WithFactoryOnly(only bool) Option {
	return func(r *Reader) { r.factoryOnly = only }
}

// WithTestedList uses list instead of the built-in tested list. It changes
// which readings say tested, and nothing that's sent to the camera.
func WithTestedList(list TestedList) Option {
	return func(r *Reader) { r.tested = list }
}

// New builds a Canon reader. Without WithTestedList it uses the tested list
// built in.
func New(opts ...Option) *Reader {
	r := &Reader{logger: zap.NewNop(), tested: BuiltInTestedList()}

	for _, o := range opts {
		o(r)
	}

	return r
}

// Name implements manufacturers.Reader.
func (r *Reader) Name() string { return "Canon" }

// VendorID implements manufacturers.Reader.
func (r *Reader) VendorID() uint16 { return VendorID }

// Models implements manufacturers.Reader.
func (r *Reader) Models() []manufacturers.Model {
	known := Models()
	out := make([]manufacturers.Model, 0, len(known)+len(eventModels))

	for _, m := range known {
		counters := make([]manufacturers.CounterSpec, 0, len(m.Counters))
		for _, c := range m.Counters {
			counters = append(counters, manufacturers.CounterSpec{
				Label:    c.Label,
				Location: fmt.Sprintf("MPU %#04x", c.Address),
			})
		}

		out = append(out, manufacturers.Model{
			Name:      m.Name,
			ProductID: m.ProductID,
			Confirmed: r.tested.Has(m.Name, MethodFactoryChannel),
			RatedLife: m.RatedLife,
			Counters:  counters,
		})
	}

	for _, m := range eventModels {
		out = append(out, manufacturers.Model{
			Name:      m.Name,
			ProductID: m.ProductID,
			Confirmed: r.tested.Has(m.Name, MethodEventChannel),
			Counters: []manufacturers.CounterSpec{
				{Label: "Mechanical shutter, in thousands", Location: "0xd167 word 3"},
				{Label: "All photos, in thousands", Location: "0xd167 word 4"},
			},
		})
	}

	return out
}

// Read implements manufacturers.Reader. What's sent depends on what the camera
// lists in GetDeviceInfo, not on its name or mount:
//
//  1. the standard shutter counter property, 0xD1AC, which bodies up to about
//     DIGIC 5 answer;
//  2. Canon's factory channel, on the EOS 5D Mark IV, and on any body that lists
//     0x9052 and 0x9053 (EF DSLRs up to about DIGIC 7), read at the 5D Mark
//     IV's addresses;
//  3. property 0xD167 on the event channel, on any body that lists it (RF
//     bodies, the 90D, newer EOS M), read the way the EOS R8 lays it out. A body
//     that lists both is tried here when the factory channel gave nothing.
//
// Anything else gets manufacturers.ErrNotSupported, having been sent nothing
// but the standard property read. A reading says tested when the tested list
// has its model and the method it was read with; the list changes nothing
// above.
func (r *Reader) Read(
	_ context.Context, device *ptp.Device, info usb.DeviceInfo, camera ptp.DeviceInfo,
) (manufacturers.Reading, error) {
	reading := manufacturers.Reading{Manufacturer: r.Name(), Model: modelName(camera, info)}

	if !r.factoryOnly {
		found, err := r.readStandardProperty(device, camera, &reading)
		if found || err != nil {
			return reading, err
		}
	}

	model, known := LookupModel(info.ProductID)
	factory := known || ListsFactoryChannel(camera)
	events := ListsEventChannel(camera)

	switch {
	case factory:
		if !known {
			model = GuessModel(info.ProductID)
		}

		err := r.readFactoryChannel(device, model, known, &reading)

		// Only a factory channel that answered but gave nothing is worth
		// following with the event channel: a camera that has stopped
		// answering won't answer that either.
		gaveNothing := isRefusal(err) || errors.Is(err, errNoCounters)
		if err == nil || r.factoryOnly || !events || !gaveNothing {
			return reading, err
		}

		reading.Steps = append(reading.Steps, "the factory channel gave no counts: trying the event channel")
		reading.Counters, reading.RatedLife = nil, 0

		return reading, r.readReleaseCounter(device, camera, &reading)
	case r.factoryOnly:
		return reading, fmt.Errorf("the %s doesn't list Canon's factory channel (0x9052 and 0x9053), so it isn't sent",
			reading.Model)
	case events:
		reading.Steps = append(reading.Steps, "it lists Canon's event channel: reading 0xd167")

		return reading, r.readReleaseCounter(device, camera, &reading)
	default:
		reading.Steps = append(reading.Steps, "it lists neither Canon's factory channel nor its event channel: nothing else is sent")

		return reading, fmt.Errorf("%w: the %s doesn't answer the standard shutter count property, "+
			"and lists neither Canon's factory channel nor its event channel", manufacturers.ErrNotSupported, reading.Model)
	}
}

// ListsFactoryChannel reports whether a camera lists both of the factory
// channel's opcodes. The EOS R8 and the 90D don't, and both hung when MonOpen
// was sent anyway.
func ListsFactoryChannel(camera ptp.DeviceInfo) bool {
	return camera.Supports(OpFactorySend) && camera.Supports(OpFactoryRecv)
}

// ListsEventChannel reports whether a camera lists what reading its event
// channel needs. ReadEventTable checks again before sending anything.
func ListsEventChannel(camera ptp.DeviceInfo) bool {
	return camera.Supports(OpEOSSetRemoteMode) && camera.Supports(OpEOSSetEventMode) && camera.Supports(OpEOSGetEvent)
}

// errNoCounters means the factory channel opened but no counter could be read.
var errNoCounters = errors.New("the factory channel gave none of the counters")

// notYet marks a failure as one that can't be read yet, unless the tested list
// has the body with the method that was being tried: on any other body that
// way of reading it is a guess, so the log is what's needed.
func (r *Reader) notYet(model, method string, err error) error {
	if r.tested.Has(model, method) {
		return err
	}

	return fmt.Errorf("%w: %w", manufacturers.ErrNotSupported, err)
}

// readStandardProperty tries PTP property 0xD1AC. found reports whether the
// camera answered it; a body that doesn't is the expected case. err is set
// only when the camera stopped answering.
func (r *Reader) readStandardProperty(device *ptp.Device, camera ptp.DeviceInfo, reading *manufacturers.Reading) (bool, error) {
	name := fmt.Sprintf("GetDevicePropValue(%#04x)", PropShutterCounter)

	if !camera.Supports(ptp.OpGetDevicePropValue) {
		reading.Steps = append(reading.Steps, name+": not sent, the camera doesn't list GetDevicePropValue")

		return false, nil
	}

	count, code, err := legacyShutterCounter(device)

	switch {
	case err == nil:
	case errors.Is(err, ErrLegacyCounterUnsupported):
		reading.Steps = append(reading.Steps, name+": "+ptp.ResponseName(code))
		r.logger.Debug("the standard shutter counter property is absent, as expected on DIGIC 6 and newer")

		return false, nil
	case code != 0:
		// Refused some other way. Not worth giving up on the camera for.
		reading.Steps = append(reading.Steps, name+": "+err.Error())

		return false, nil
	default:
		reading.Steps = append(reading.Steps, name+": "+err.Error())

		return false, fmt.Errorf("%s: %w", name, err)
	}

	reading.Steps = append(reading.Steps, fmt.Sprintf("%s: OK, %d", name, count))
	reading.Confirmed = r.tested.Has(reading.Model, MethodStandardProperty)
	reading.Method = MethodStandardProperty
	reading.Counters = []manufacturers.Counter{{Label: "Shutter actuations", Location: "0xd1ac", Value: count}}

	return true, nil
}

// readFactoryChannel reads the counters out of MPU memory. known says model is
// the body's own address map (LookupModel found it), not GuessModel's.
func (r *Reader) readFactoryChannel(device *ptp.Device, model Model, known bool, reading *manufacturers.Reading) error {
	if known {
		reading.Steps = append(reading.Steps, "the "+model.Name+" is read through the factory channel")
	} else {
		reading.Steps = append(reading.Steps, fmt.Sprintf(
			"it lists Canon's factory channel: reading it at the %s's MPU addresses", models[0].Name))
	}

	readings, err := ReadModel(device, model)

	failed := 0

	for _, c := range readings {
		step := fmt.Sprintf("MonReadAndGetData(%#04x, %d): ", c.Counter.Address, c.Counter.Length)
		if c.Err != nil {
			step += c.Err.Error()
			failed++
		} else {
			step += hex.EncodeToString(c.Raw)
		}

		reading.Steps = append(reading.Steps, step)
	}

	if err != nil {
		reading.Steps = append(reading.Steps, "factory channel: "+err.Error())

		if isRefusal(err) {
			return r.notYet(reading.Model, MethodFactoryChannel, err)
		}

		return err
	}

	if failed == len(readings) {
		return r.notYet(reading.Model, MethodFactoryChannel, errNoCounters)
	}

	if known && strings.HasPrefix(reading.Model, "Canon camera") {
		reading.Model = model.Name // GetDeviceInfo gave no name
	}

	reading.Confirmed = r.tested.Has(reading.Model, MethodFactoryChannel)
	reading.Method = MethodFactoryChannel
	reading.RatedLife = model.RatedLife
	reading.Counters = make([]manufacturers.Counter, 0, len(readings))

	for _, c := range readings {
		reading.Counters = append(reading.Counters, manufacturers.Counter{
			Label:    c.Counter.Label,
			Location: fmt.Sprintf("MPU %#04x", c.Counter.Address),
			Value:    c.Value,
			Raw:      c.Raw,
			Err:      c.Err,
		})
	}

	return nil
}

// modelName is what the camera calls itself in GetDeviceInfo, less "Canon ",
// or failing that its USB product name.
func modelName(camera ptp.DeviceInfo, info usb.DeviceInfo) string {
	if name := strings.TrimSpace(strings.TrimPrefix(camera.Model, "Canon ")); name != "" {
		return name
	}

	if info.Product != "" && info.Product != "Canon Digital Camera" {
		return strings.TrimPrefix(info.Product, "Canon ")
	}

	return fmt.Sprintf("Canon camera (USB %04x:%04x)", info.VendorID, info.ProductID)
}
