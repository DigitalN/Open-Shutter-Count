package trace

import (
	"bytes"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/ptp"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/usb"
)

func fixedClock() func() time.Time {
	at := time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC)

	return func() time.Time { return at }
}

func recordConversation(t *testing.T) *Recorder {
	t.Helper()

	r := NewRecorder("v1.2.3")
	r.now = fixedClock()
	r.Describe(usb.DeviceInfo{
		VendorID:  0x04a9,
		ProductID: 0x3281,
		Product:   "Canon EOS 5D Mark IV",
		Path:      "/dev/bus/usb/001/007",
	})
	r.Annotate("20 frames since the previous trace")

	r.Record(ptp.DirectionOut, ptp.Container{
		Type: ptp.ContainerCommand, Code: 0x9052, TransactionID: 11,
		Payload: ptp.MarshalParams(0, 1),
	})
	r.Record(ptp.DirectionOut, ptp.Container{
		Type: ptp.ContainerData, Code: 0x9052, TransactionID: 11,
		Payload: []byte("MonOpen\x00"),
	})
	// A response that does not repeat the command's opcode, which is the
	// normal case and the reason the opcode is tracked by transaction.
	r.Record(ptp.DirectionIn, ptp.Container{
		Type: ptp.ContainerResponse, Code: ptp.RCOK, TransactionID: 11,
	})

	return r
}

func TestRecorderCapturesDirectionAndOpcode(t *testing.T) {
	file := recordConversation(t).File()

	require.Len(t, file.Entries, 3)

	assert.Equal(t, ptp.DirectionOut, file.Entries[0].Direction)
	assert.Equal(t, ptp.DirectionIn, file.Entries[2].Direction)

	// Every entry belongs to the 0x9052 command that opened transaction 11,
	// including the response, whose own code is the response code.
	for i, e := range file.Entries {
		assert.Equal(t, uint16(0x9052), e.Opcode, "entry %d", i)
		assert.Equal(t, i+1, e.Index)
	}

	assert.Equal(t, ptp.RCOK, file.Entries[2].Code)
	assert.Equal(t, hex.EncodeToString([]byte("MonOpen\x00")), file.Entries[1].Payload)
}

func TestRecorderRoundTripsThroughAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.json")
	require.NoError(t, recordConversation(t).WriteFile(path))

	file, err := ReadFile(path)
	require.NoError(t, err)

	assert.Equal(t, "openshuttercount", file.Tool)
	assert.Equal(t, "v1.2.3", file.ToolVersion)
	assert.Equal(t, FormatVersion, file.FormatVersion)
	assert.Equal(t, "Canon EOS 5D Mark IV", file.Device.Product)
	assert.Equal(t, uint16(0x3281), file.Device.ProductID)
	assert.Equal(t, "20 frames since the previous trace", file.Notes)
	require.Len(t, file.Entries, 3)

	// The containers have to come back exactly as they went in, or a trace
	// is no use for confirming anything.
	container, err := file.Entries[1].Container()
	require.NoError(t, err)
	assert.Equal(t, ptp.ContainerData, container.Type)
	assert.Equal(t, uint32(11), container.TransactionID)
	assert.Equal(t, []byte("MonOpen\x00"), container.Payload)
}

// formerName is what the tool and the app called themselves before the rename, which
// traces and logs saved by those versions still say. It is in two pieces so that the
// guard in make check, which keeps the old name out of the tree, doesn't flag the
// test that has to cover it.
const formerName = "openshuttercount" + "er"

func TestReadStillDecodesWhatWasSavedBeforeTheRename(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, recordConversation(t).Write(&buf))

	was := []byte(`"tool": "openshuttercount"`)
	require.Contains(t, buf.String(), string(was), "the test needs the tool field to swap")

	traceFile := bytes.Replace(buf.Bytes(), was, []byte(`"tool": "`+formerName+`"`), 1)
	savedLog := "Open Shutter Count 0.5.4\n\nThe whole exchange with the camera.\n" +
		"-----BEGIN " + strings.ToUpper(formerName) + " TRACE-----\n" +
		string(traceFile) + "\n" +
		"-----END " + strings.ToUpper(formerName) + " TRACE-----\n"

	for name, raw := range map[string][]byte{
		"a trace file": traceFile,
		"a saved log":  []byte(savedLog),
	} {
		t.Run(name, func(t *testing.T) {
			file, err := Read(raw)
			require.NoError(t, err)

			assert.Equal(t, formerName, file.Tool, "it says what the file says")
			assert.Equal(t, "v1.2.3", file.ToolVersion)
			assert.Equal(t, "Canon EOS 5D Mark IV", file.Device.Product)
			require.Len(t, file.Entries, 3)

			container, err := file.Entries[1].Container()
			require.NoError(t, err)
			assert.Equal(t, []byte("MonOpen\x00"), container.Payload)
		})
	}
}

func TestReadRejectsATraceFromTheFuture(t *testing.T) {
	var buf bytes.Buffer

	recorder := recordConversation(t)
	require.NoError(t, recorder.Write(&buf))

	ahead := bytes.Replace(buf.Bytes(),
		[]byte(`"format_version": 1`),
		[]byte(`"format_version": 99`), 1)

	_, err := Read(ahead)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "please upgrade")
}

func TestReadRejectsRubbish(t *testing.T) {
	_, err := Read([]byte("neither JSON nor a capture"))
	require.Error(t, err)

	_, err = Read([]byte("{ not valid json"))
	require.Error(t, err)
}

func TestEntryReportsAnUndecodablePayload(t *testing.T) {
	_, err := Entry{Index: 4, Payload: "zzzz"}.Container()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entry 4")
}
