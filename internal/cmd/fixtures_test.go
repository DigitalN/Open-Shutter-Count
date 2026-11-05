package cmd

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DigitalN/openshuttercounter/pkg/manufacturers/canon"
	"github.com/DigitalN/openshuttercounter/pkg/ptp"
	"github.com/DigitalN/openshuttercounter/pkg/trace"
	"github.com/DigitalN/openshuttercounter/pkg/usb"
)

// The app's tests read what this tool prints from app/tests/fixtures, and those
// files are made here, from the real code reading a scripted camera, so the
// two can't drift apart. After changing what `read --json` or `list --json`
// prints, or what a read sends:
//
//	UPDATE_FIXTURES=1 go test ./internal/cmd -run TestAppFixtures
//
// and commit the files with the change.

const fixtureDir = "../../app/tests/fixtures"

// fakePath is where the scripted cameras are plugged in.
const fakePath = "usb:001:002"

// What the scripted cameras say that must never reach a fixture.
const (
	fakeSerial    = "0123456789abcdef0123456789abcdef"
	fakeBodyID    = "072022001307"
	fakeUSBSerial = "USBSERIAL0001"
	fakeOwner     = "Jane Doe"
)

// scriptedCamera answers each command from a script, like a real camera.
type scriptedCamera struct {
	info    usb.DeviceInfo
	answer  func(op uint16) ([]byte, uint16)
	pending [][]byte
}

func (c *scriptedCamera) Info() usb.DeviceInfo { return c.info }
func (c *scriptedCamera) ClearHalt() error     { return nil }
func (c *scriptedCamera) Close() error         { return nil }

func (c *scriptedCamera) Read(p []byte, _ time.Duration) (int, error) {
	if len(c.pending) == 0 {
		return 0, errors.New("the camera has nothing to send")
	}

	next := c.pending[0]
	c.pending = c.pending[1:]

	return copy(p, next), nil
}

func (c *scriptedCamera) Write(p []byte, _ time.Duration) (int, error) {
	head, _, err := ptp.ParseHeader(p)
	if err != nil {
		return 0, err
	}

	if head.Type == ptp.ContainerData {
		return len(p), nil
	}

	data, code := c.answer(head.Code)
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

func eventRecord(code, prop uint32, value []byte) []byte {
	out := binary.LittleEndian.AppendUint32(nil, uint32(12+len(value))) //nolint:gosec // short test records
	out = binary.LittleEndian.AppendUint32(out, code)
	out = binary.LittleEndian.AppendUint32(out, prop)

	return append(out, value...)
}

func eventList(records ...[]byte) []byte {
	var out []byte
	for _, r := range records {
		out = append(out, r...)
	}

	return append(out, 8, 0, 0, 0, 0, 0, 0, 0)
}

// Which of Canon's channels a scripted camera lists.
var (
	eventChannel = []uint16{
		canon.OpEOSSetRemoteMode, canon.OpEOSSetEventMode, canon.OpEOSGetEvent, canon.OpEOSRequestDevicePropValue,
	}
	factoryChannel = []uint16{canon.OpFactorySend, canon.OpFactoryRecv}
)

// fakeCamera is a Canon body that answers like the maintainer's EOS R8 does,
// listing the channels given. Its event channel reports d167 along with its
// serial number and owner; its factory channel answers each read with the next
// of mpu.
func fakeCamera(
	t *testing.T, model string, productID uint16, channels []uint16, d167 []byte, mpu ...[]byte,
) *scriptedCamera {
	t.Helper()

	info := ptp.DeviceInfo{
		StandardVersion: 100, VendorExtensionID: 6, VendorExtensionVersion: 100,
		Manufacturer: "Canon.Inc", Model: model, DeviceVersion: "3-1.5.0", SerialNumber: fakeSerial,
		Operations: append([]uint16{
			ptp.OpGetDeviceInfo, ptp.OpOpenSession, ptp.OpCloseSession, ptp.OpGetDevicePropDesc, ptp.OpGetDevicePropValue,
		}, channels...),
	}

	raw, err := info.MarshalBinary()
	require.NoError(t, err)

	events, reads := 0, 0

	return &scriptedCamera{
		info: usb.DeviceInfo{
			Path: fakePath, Bus: 1, Address: 2, VendorID: canon.VendorID, ProductID: productID,
			Product: "Canon Digital Camera", Serial: fakeUSBSerial,
		},
		answer: func(op uint16) ([]byte, uint16) {
			if !info.Supports(op) {
				t.Fatalf("the %s was sent %#04x, which it doesn't list", model, op)
			}

			switch op {
			case ptp.OpGetDeviceInfo:
				return raw, ptp.RCOK
			case ptp.OpGetDevicePropValue:
				return nil, ptp.RCOperationNotSupported
			case canon.OpEOSGetEvent:
				events++
				if events > 1 {
					return eventList(), ptp.RCOK
				}

				return eventList(
					eventRecord(canon.EventPropValueChanged, 0xd1af, []byte(fakeBodyID+"\x00")),
					eventRecord(canon.EventPropValueChanged, 0xd115, []byte(fakeOwner+"\x00")),
					eventRecord(canon.EventPropValueChanged, canon.PropShutterReleaseCounter, d167),
				), ptp.RCOK
			case canon.OpFactoryRecv:
				reads++
				if reads > len(mpu) {
					return nil, ptp.RCGeneralError
				}

				return mpu[reads-1], ptp.RCOK
			}

			return nil, ptp.RCOK
		},
	}
}

// readFixture reads a scripted camera the way Read does, recording a trace.
func readFixture(t *testing.T, cam *scriptedCamera) (readOutput, trace.File) {
	t.Helper()

	recorder := trace.NewRecorder("0.5.0", canon.ScrubEvents, trace.ScrubProperties(canon.IdentifyingProperties...))
	recorder.Annotate("Open Shutter Count 0.5.0")
	recorder.Describe(cam.info)

	s := &session{info: cam.info, reader: canon.New(), device: ptp.New(cam, ptp.WithRecorder(recorder))}
	require.NoError(t, s.device.OpenSession())
	s.steps = append(s.steps, "OpenSession: OK")

	camera, err := s.deviceInfo()
	require.NoError(t, err)

	reading, readErr := s.reader.Read(context.Background(), s.device, s.info, camera)

	out := buildOutput(s.info, camera, reading, true)
	out.Steps = append(append([]string(nil), s.steps...), reading.Steps...)

	if readErr != nil {
		out.Reading, out.Counters = nil, nil
		out = failureOutput(out, readErr)
	}

	// The trace names the machine that made it. Make it the one the app
	// runs on, so the fixtures are the same wherever the tests run.
	file := recorder.File()
	file.Host = trace.Host{OS: "darwin", Arch: "arm64"}

	return out, file
}

// r8D167 is what the maintainer's EOS R8 reported after the 2026-11-04 test.
var r8D167 = []byte{0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xd0, 0x07, 0, 0, 0xf0, 0x55, 0, 0}

func TestAppFixtures(t *testing.T) {
	thousand := []byte{0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xe8, 0x03, 0, 0, 0xe8, 0x03, 0, 0}

	r8 := fakeCamera(t, "Canon EOS R8", 0x330c, eventChannel, r8D167)
	r8Out, r8Trace := readFixture(t, r8)
	require.Nil(t, r8Out.Error)

	// libgphoto2's R6 and 90D dumps: 1000 and 1000. The 90D is an EF body
	// that lists the event channel and not the factory channel.
	r6Out, _ := readFixture(t, fakeCamera(t, "Canon EOS R6", 0x32f5, eventChannel, thousand))
	require.Nil(t, r6Out.Error)

	d90Out, _ := readFixture(t, fakeCamera(t, "Canon EOS 90D", 0x3295, eventChannel, thousand))
	require.Nil(t, d90Out.Error)

	// An EF body that lists the factory channel, as libgphoto2's 80D dump
	// does, read at the 5D Mark IV's addresses. The replies are the 5D Mark
	// IV's, from docs/PROTOCOL.md.
	d80 := fakeCamera(t, "Canon EOS 80D", 0x3294, slices.Concat(factoryChannel, eventChannel), nil,
		[]byte{0x00, 0x66, 0, 0, 0, 0, 0, 0xf2, 0, 0},
		[]byte{0x00, 0x66, 0, 0, 0, 0, 0, 0, 0xf2, 0},
		[]byte{0x00, 0x67, 0, 0, 0, 0, 0, 0, 0, 0x01})
	d80Out, d80Trace := readFixture(t, d80)
	require.Nil(t, d80Out.Error)

	powershot := fakeCamera(t, "Canon PowerShot G7 X Mark III", 0x32f4, nil, nil)
	powershotOut, powershotTrace := readFixture(t, powershot)
	require.NotNil(t, powershotOut.Error)

	device := describeUSB(r8.info)
	failed := func(err error) readOutput {
		return failureOutput(readOutput{Device: device, Steps: nil}, err)
	}

	busy := withHint(fmt.Errorf("libusb_open: LIBUSB_ERROR_ACCESS: %w", usb.ErrBusy),
		darwinBusyHint([]usb.Owner{{Name: "EOS Utility"}}))
	wrongMode := withHint(fmt.Errorf("usb:001:003: %w; it offers: interface 0 class 0xff/0xf0", usb.ErrNoStillImage),
		noStillImageHint)
	wrongModeDevice := &deviceOutput{VendorID: "04a9", ProductID: "32a6", Path: "usb:001:003"}
	r8Listed := usb.DeviceInfo{
		Path: fakePath, Bus: 1, Address: 2, VendorID: canon.VendorID, ProductID: 0x330c, Serial: fakeUSBSerial,
	}

	fixtures := map[string]any{
		"read-r8.json":             r8Out,
		"read-r6.json":             r6Out,
		"read-90d.json":            d90Out,
		"read-80d.json":            d80Out,
		"read-powershot.json":      powershotOut,
		"read-busy.json":           failed(busy),
		"read-wrong-usb-mode.json": failureOutput(readOutput{Device: wrongModeDevice}, wrongMode),
		"read-no-camera.json":      failureOutput(readOutput{}, noDeviceError{}),
		"read-no-answer.json":      failed(fmt.Errorf("the camera didn't answer, even after a reset: %w", usb.ErrTimeout)),
		"list-r8.json":             listOutput(registry(nil, false), []usb.DeviceInfo{r8Listed}),
		"trace-r8.json":            r8Trace,
		"trace-80d.json":           d80Trace,
		"trace-powershot.json":     powershotTrace,
	}

	for name, value := range fixtures {
		raw, err := json.MarshalIndent(value, "", "  ")
		require.NoError(t, err)

		raw = append(raw, '\n')

		for _, secret := range []string{fakeSerial, fakeBodyID, fakeUSBSerial, fakeOwner} {
			require.NotContains(t, string(raw), secret, "%s holds something that identifies the camera", name)
		}

		path := filepath.Join(fixtureDir, name)

		if os.Getenv("UPDATE_FIXTURES") != "" {
			require.NoError(t, os.WriteFile(path, raw, 0o600))

			continue
		}

		want, err := os.ReadFile(path) //nolint:gosec // a fixture in the repository
		require.NoError(t, err, "run with UPDATE_FIXTURES=1 to make it")

		if filepath.Base(name)[:6] == "trace-" {
			assert.Equal(t, withoutTimes(t, want), withoutTimes(t, raw), "%s is out of date; run with UPDATE_FIXTURES=1", name)
		} else {
			assert.JSONEq(t, string(want), string(raw), "%s is out of date; run with UPDATE_FIXTURES=1", name)
		}
	}
}

// withoutTimes is a trace with its timestamps taken out, which differ each run.
func withoutTimes(t *testing.T, raw []byte) trace.File {
	t.Helper()

	var file trace.File
	require.NoError(t, json.Unmarshal(raw, &file))

	file.RecordedAt = time.Time{}
	for i := range file.Entries {
		file.Entries[i].At = nil
	}

	return file
}
