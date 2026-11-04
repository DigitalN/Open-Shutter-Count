package canon

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/DigitalN/openshuttercounter/pkg/manufacturers"
	"github.com/DigitalN/openshuttercounter/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/pkg/usb"
)

// How a reading was obtained, reported so that anyone comparing two readings
// knows whether they came from the same place.
const (
	// MethodStandardProperty is the standard PTP shutter counter property,
	// which bodies up to roughly the DIGIC 5 generation still answer.
	MethodStandardProperty = "ptp-property-0xd1ac"

	// MethodFactoryChannel is the maker specific channel that reaches the
	// MPU's own memory. Only the EOS 5D Mark IV is read this way.
	MethodFactoryChannel = "canon-factory-channel"
)

// Reader reads actuation counters from Canon EOS cameras.
type Reader struct {
	logger *zap.Logger

	// factoryOnly skips the standard property and goes straight to the
	// factory channel. Useful when comparing the two on a body that
	// answers both.
	factoryOnly bool
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

// New builds a Canon reader.
func New(opts ...Option) *Reader {
	r := &Reader{logger: zap.NewNop()}

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
			Name:        m.Name,
			ProductID:   m.ProductID,
			Confirmed:   m.Confirmed,
			ConfirmedOn: m.ConfirmedOn,
			RatedLife:   m.RatedLife,
			Counters:    counters,
		})
	}

	for _, m := range eventModels {
		out = append(out, manufacturers.Model{
			Name:        m.Name,
			ProductID:   m.ProductID,
			Confirmed:   true,
			ConfirmedOn: m.ConfirmedOn,
			Counters: []manufacturers.CounterSpec{
				{Label: "Mechanical shutter, in thousands", Location: "0xd167 word 3"},
				{Label: "All photos, in thousands", Location: "0xd167 word 4"},
			},
		})
	}

	return out
}

// Read implements manufacturers.Reader. It tries, in order:
//
//  1. the standard shutter counter property, 0xD1AC, on any body;
//  2. the factory channel, on the EOS 5D Mark IV only;
//  3. property 0xD167 on the event channel, on RF bodies, spotted by the
//     model name in GetDeviceInfo and sent only the commands they list.
//
// Anything else gets manufacturers.ErrNotSupported, having been sent nothing
// but the standard property read.
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

	if model, ok := LookupModel(info.ProductID); ok {
		return reading, r.readFactoryChannel(device, model, &reading)
	}

	if r.factoryOnly {
		return reading, fmt.Errorf("the factory channel is only read on the %s, not the %s",
			strings.Join(factoryModelNames(), ", "), reading.Model)
	}

	if IsRFBody(camera.Model) {
		reading.Steps = append(reading.Steps, fmt.Sprintf("%q is an RF body: reading 0xd167 off the event channel", camera.Model))

		return reading, r.readReleaseCounter(device, camera, &reading)
	}

	reading.Steps = append(reading.Steps,
		fmt.Sprintf("%q isn't the EOS 5D Mark IV or an RF body: nothing else is tried", camera.Model))

	return reading, fmt.Errorf("%w: the %s doesn't answer the standard shutter count property, "+
		"and only the EOS 5D Mark IV and RF bodies are read any other way", manufacturers.ErrNotSupported, reading.Model)
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
	reading.Confirmed = tested[reading.Model]
	reading.Method = MethodStandardProperty
	reading.Counters = []manufacturers.Counter{{Label: "Shutter actuations", Location: "0xd1ac", Value: count}}

	return true, nil
}

// readFactoryChannel reads the counters out of MPU memory.
func (r *Reader) readFactoryChannel(device *ptp.Device, model Model, reading *manufacturers.Reading) error {
	reading.Steps = append(reading.Steps, "the "+model.Name+" is read through the factory channel")

	readings, err := ReadModel(device, model)

	for _, c := range readings {
		step := fmt.Sprintf("MonReadAndGetData(%#04x, %d): ", c.Counter.Address, c.Counter.Length)
		if c.Err != nil {
			step += c.Err.Error()
		} else {
			step += hex.EncodeToString(c.Raw)
		}

		reading.Steps = append(reading.Steps, step)
	}

	if err != nil {
		reading.Steps = append(reading.Steps, "factory channel: "+err.Error())

		return err
	}

	if reading.Model == "" || strings.HasPrefix(reading.Model, "Canon camera") {
		reading.Model = model.Name
	}

	reading.Confirmed = model.Confirmed && tested[model.Name]
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

// factoryModelNames are the bodies the factory channel is sent to.
func factoryModelNames() []string {
	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Name)
	}

	return names
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
