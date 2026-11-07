package canon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/manufacturers"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/ptp"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/usb"
)

// Names and reasons the tests below use more than once.
const (
	theR8          = "EOS R8"
	the5DMarkIV    = "EOS 5D Mark IV"
	the80D         = "EOS 80D"
	cutShort       = "it isn't valid JSON: unexpected end of JSON input"
	noTestedList   = `it has no "tested" list`
	noUsableEntry  = "it has no usable entry"
	byFactory      = "listed with the factory channel"
	byEventChannel = "listed with the event channel"
)

// The built-in list is held to more than a given one is: a given list skips
// what it can't use, but this one is in every build, so nothing in it may be
// skipped. Which cameras it has isn't checked, since entries can be removed.
func TestTheBuiltInListPassesTheStrictCheck(t *testing.T) {
	require.True(t, json.Valid(builtInTested), "tested.json isn't valid JSON")

	var file struct {
		Tested []TestedEntry `json:"tested"`
	}

	require.NoError(t, json.Unmarshal(builtInTested, &file), "tested.json is an object with a \"tested\" list of entries")
	require.NotEmpty(t, file.Tested, "tested.json has at least one entry")

	seen := map[[2]string]bool{}

	for i, entry := range file.Tested {
		assert.NotEmpty(t, strings.TrimSpace(entry.Model), "entry %d has no model", i+1)
		assert.Equal(t, strings.TrimSpace(entry.Model), entry.Model,
			"entry %d's model has spaces around it, so it would never match", i+1)
		assert.Contains(t, Methods, entry.Method, "entry %d (%q) has an unknown method", i+1, entry.Model)

		pair := [2]string{entry.Model, entry.Method}
		assert.False(t, seen[pair], "entry %d (%q, %q) is in the list twice", i+1, entry.Model, entry.Method)
		seen[pair] = true
	}

	list, skipped, err := ParseTestedList(builtInTested)
	require.NoError(t, err)
	assert.Empty(t, skipped)
	assert.Equal(t, len(file.Tested), list.Len())
	assert.Equal(t, list.Len(), BuiltInTestedList().Len())
}

func TestAGivenListIsReadLeniently(t *testing.T) {
	list, skipped, err := ParseTestedList([]byte(`{
  "version": 2,
  "tested": [
    {"model": "EOS R8", "method": "canon-event-channel-0xd167", "note": "n", "added_later": true},
    {"model": "", "method": "canon-event-channel-0xd167"},
    {"method": "canon-factory-channel"},
    {"model": "EOS R9", "method": "canon-telepathy"},
    {"model": "EOS R10"},
    "EOS R50",
    {"model": 5, "method": "canon-factory-channel"},
    null,
    {"model": "EOS 80D", "method": "canon-factory-channel"}
  ]
}`))
	require.NoError(t, err)

	assert.Equal(t, 2, list.Len())
	assert.True(t, list.Has("EOS R8", MethodEventChannel), "fields it doesn't know are ignored")
	assert.True(t, list.Has(the80D, MethodFactoryChannel))
	assert.Equal(t, []string{
		"entry 2 has no model",
		"entry 3 has no model",
		`entry 4 ("EOS R9") has an unknown method, "canon-telepathy"`,
		`entry 5 ("EOS R10") has an unknown method, ""`,
		"entry 6 isn't an object with a text model and method",
		"entry 7 isn't an object with a text model and method",
		"entry 8 has no model",
	}, skipped)
}

func TestAListMatchesTheModelAndMethodExactly(t *testing.T) {
	list, _, err := ParseTestedList([]byte(`{"tested": [{"model": "EOS R8", "method": "canon-event-channel-0xd167"}]}`))
	require.NoError(t, err)

	assert.True(t, list.Has("EOS R8", MethodEventChannel))
	assert.False(t, list.Has("EOS R8", MethodFactoryChannel))
	assert.False(t, list.Has("EOS R8", MethodStandardProperty))
	assert.False(t, list.Has("Canon EOS R8", MethodEventChannel))
	assert.False(t, list.Has("EOS R8 ", MethodEventChannel))
	assert.False(t, list.Has("eos r8", MethodEventChannel))
}

func TestAListThatCantBeUsedSaysWhy(t *testing.T) {
	for raw, want := range map[string]string{
		``:                     cutShort,
		`{"tested": [`:         cutShort,
		`{"tested": [}`:        "it isn't valid JSON: invalid character '}' looking for beginning of value",
		`[]`:                   "it isn't a JSON object",
		`"tested"`:             "it isn't a JSON object",
		`null`:                 noTestedList,
		`{}`:                   noTestedList,
		`{"Tested": []}`:       noTestedList,
		`{"tested": "EOS R8"}`: `its "tested" isn't a list`,
		`{"tested": {}}`:       `its "tested" isn't a list`,
		`{"tested": []}`:       noUsableEntry,
		`{"tested": null}`:     noUsableEntry,
		`{"tested": [{"model": "EOS R8", "method": "canon-x"}]}`: noUsableEntry,
	} {
		list, _, err := ParseTestedList([]byte(raw))
		require.EqualError(t, err, want, raw)
		assert.Zero(t, list.Len(), raw)
	}
}

func writeList(t *testing.T, text string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "tested.json")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))

	return path
}

// standIn has the 80D, read through the factory channel, and not the R8.
const standIn = `{"tested": [
  {"model": "EOS 5D Mark IV", "method": "canon-factory-channel"},
  {"model": "EOS 80D", "method": "canon-factory-channel"}
]}`

func TestAGivenListReplacesTheBuiltInOne(t *testing.T) {
	list, report := LoadTestedList(writeList(t, standIn))

	assert.Equal(t, TestedListReport{Given: true, Source: TestedListGiven, Entries: 2}, report)
	assert.True(t, list.Has(the80D, MethodFactoryChannel), "it adds a body")
	assert.False(t, list.Has("EOS R8", MethodEventChannel), "and takes one off")
	assert.Equal(t, []string{"tested list: the one given, 2 entries"}, report.Steps())
}

func TestNoListGivenIsTheBuiltInOneAndSaysNothing(t *testing.T) {
	list, report := LoadTestedList("")

	assert.Equal(t, BuiltInTestedList(), list)
	assert.Equal(t, TestedListReport{Source: TestedListBuiltIn, Entries: BuiltInTestedList().Len()}, report)
	assert.Empty(t, report.Steps(), "a read without --tested-list says what it always did")
}

func TestAGivenListThatCantBeUsedGivesTheBuiltInOne(t *testing.T) {
	builtInList := BuiltInTestedList()
	dir := t.TempDir()

	for name, c := range map[string]struct {
		path, reason string
	}{
		"missing":         {filepath.Join(dir, "nowhere.json"), "it couldn't be read: no such file or directory"},
		"a folder":        {dir, "it couldn't be read: is a directory"},
		"bad JSON":        {writeList(t, `{"tested": [`), cutShort},
		"empty":           {writeList(t, `{"tested": []}`), noUsableEntry},
		"unknown methods": {writeList(t, `{"tested": [{"model": "EOS R9", "method": "x"}]}`), noUsableEntry},
		"too big":         {writeList(t, strings.Repeat(" ", maxTestedListSize+1)), "it's over 1048576 bytes"},
	} {
		list, report := LoadTestedList(c.path)

		assert.Equal(t, builtInList, list, name)
		assert.Equal(t, TestedListBuiltIn, report.Source, name)
		assert.Equal(t, builtInList.Len(), report.Entries, name)
		assert.Equal(t, c.reason, report.Reason, name)
		assert.NotContains(t, report.Reason, dir, "%s: the reason doesn't name the file, which is in the app's log", name)
		require.NotEmpty(t, report.Steps(), name)
		assert.Equal(t,
			fmt.Sprintf("tested list: the built-in one, %d entries, as the one given couldn't be used: %s",
				builtInList.Len(), c.reason),
			report.Steps()[0], name)
	}
}

func TestSkippedEntriesAreInTheSteps(t *testing.T) {
	_, report := LoadTestedList(writeList(t, `{"tested": [
  {"model": "EOS R8", "method": "canon-event-channel-0xd167"},
  {"model": "EOS R9", "method": "canon-telepathy"}
]}`))

	assert.Equal(t, TestedListGiven, report.Source)
	assert.Equal(t, 1, report.Entries)
	assert.Equal(t, []string{
		"tested list: the one given, 1 entries",
		`tested list: skipped entry 2 ("EOS R9") has an unknown method, "canon-telepathy"`,
	}, report.Steps())

	// A list with nothing usable says why each entry was skipped.
	_, report = LoadTestedList(writeList(t, `{"tested": [{"model": "EOS R9", "method": "x"}]}`))
	assert.Contains(t, report.Steps(), `tested list: skipped entry 1 ("EOS R9") has an unknown method, "x"`)
}

func TestManySkippedEntriesAreCountedNotListed(t *testing.T) {
	entries := make([]string, 0, 26)
	entries = append(entries, `{"model": "EOS R8", "method": "canon-event-channel-0xd167"}`)

	for range 25 {
		entries = append(entries, `{"model": "EOS R9", "method": "x"}`)
	}

	_, report := LoadTestedList(writeList(t, `{"tested": [`+strings.Join(entries, ",")+`]}`))

	steps := report.Steps()
	assert.Len(t, steps, 1+maxSkippedSteps+1)
	assert.Equal(t, "tested list: 15 more entries skipped", steps[len(steps)-1])
}

// listOf is a tested list with these model and method pairs.
func listOf(t *testing.T, pairs ...string) TestedList {
	t.Helper()

	entries := make([]TestedEntry, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		entries = append(entries, TestedEntry{Model: pairs[i], Method: pairs[i+1]})
	}

	raw, err := json.Marshal(map[string]any{"tested": entries})
	require.NoError(t, err)

	list, skipped, err := ParseTestedList(raw)
	require.NoError(t, err)
	require.Empty(t, skipped)

	return list
}

func readWith(
	t *testing.T, list TestedList, cam *camera, productID uint16, info ptp.DeviceInfo,
) (manufacturers.Reading, error) {
	t.Helper()

	return New(WithTestedList(list)).Read(context.Background(), ptp.New(cam),
		usb.DeviceInfo{VendorID: VendorID, ProductID: productID}, info)
}

// d80Camera is the 80D, answering the factory channel at the 5D Mark IV's
// addresses with the 5D Mark IV's replies.
func d80Camera(t *testing.T) *camera {
	t.Helper()

	return factoryCamera(t, ptp.RCOK,
		[]byte{0x00, 0x66, 0, 0, 0, 0, 0, 0xf2, 0, 0},
		[]byte{0x00, 0x66, 0, 0, 0, 0, 0, 0, 0xf2, 0},
		[]byte{0x00, 0x67, 0, 0, 0, 0, 0, 0, 0, 0x01})
}

func TestAnEFBodyReadAtThe5DMarkIVsAddressesIsTestedOnlyWhenListedWithTheFactoryChannel(t *testing.T) {
	for name, c := range map[string]struct {
		list   TestedList
		tested bool
	}{
		byFactory:                           {listOf(t, the80D, MethodFactoryChannel), true},
		byEventChannel:                      {listOf(t, the80D, MethodEventChannel), false},
		"listed with the standard property": {listOf(t, the80D, MethodStandardProperty), false},
		"not listed":                        {BuiltInTestedList(), false},
	} {
		cam := d80Camera(t)

		reading, err := readWith(t, c.list, cam, 0x3294, efInfo)
		require.NoError(t, err, name)

		assert.Equal(t, MethodFactoryChannel, reading.Method, name)
		assert.Equal(t, c.tested, reading.Confirmed, name)
		assert.Contains(t, reading.Steps, "it lists Canon's factory channel: reading it at the EOS 5D Mark IV's MPU addresses",
			name)
		assert.NotContains(t, cam.commands, getEvent, "%s: the list changes nothing that's sent", name)
	}
}

func TestTheEventChannelAfterTheFactoryChannelGaveNothingUsesTheEventChannelsEntry(t *testing.T) {
	for name, c := range map[string]struct {
		list   TestedList
		tested bool
	}{
		byEventChannel: {listOf(t, the80D, MethodEventChannel), true},
		byFactory:      {listOf(t, the80D, MethodFactoryChannel), false},
	} {
		reading, err := readWith(t, c.list, factoryCamera(t, ptp.RCOK), 0x3294, efInfo)
		require.NoError(t, err, name)

		assert.Equal(t, MethodEventChannel, reading.Method, name)
		assert.Equal(t, c.tested, reading.Confirmed, name)
	}
}

func TestAListedModelReadAnotherWayIsntTested(t *testing.T) {
	// An R8 that answered the standard property would be read there, and the
	// list has the R8 only with the event channel.
	cam := &camera{answer: func(op uint16, _ []uint32) ([]byte, uint16) {
		if op == ptp.OpGetDevicePropValue {
			return []byte{0x39, 0x30, 0, 0}, ptp.RCOK
		}

		return nil, ptp.RCOK
	}}

	reading, err := readWith(t, BuiltInTestedList(), cam, 0x330c, r8Info)
	require.NoError(t, err)
	assert.Equal(t, MethodStandardProperty, reading.Method)
	assert.False(t, reading.Confirmed)

	// And the 5D Mark IV, if the list only had it with the event channel.
	reading, err = readWith(t, listOf(t, the5DMarkIV, MethodEventChannel), d80Camera(t), 0x3281,
		ptp.DeviceInfo{Model: "Canon EOS 5D Mark IV", Operations: []uint16{ptp.OpGetDevicePropValue}})
	require.NoError(t, err)
	assert.Equal(t, MethodFactoryChannel, reading.Method)
	assert.False(t, reading.Confirmed)
}

func TestTheStandardPropertyIsTestedWhenListedWithIt(t *testing.T) {
	cam := &camera{answer: func(op uint16, _ []uint32) ([]byte, uint16) {
		return []byte{0x39, 0x30, 0, 0}, ptp.RCOK
	}}

	info := r8Info
	info.Model = "Canon EOS 5D Mark III"

	reading, err := readWith(t, listOf(t, "EOS 5D Mark III", MethodStandardProperty), cam, 0x3226, info)
	require.NoError(t, err)
	assert.Equal(t, MethodStandardProperty, reading.Method)
	assert.True(t, reading.Confirmed)
}

func TestTheListTakesBodiesOffAsWellAsAddingThem(t *testing.T) {
	r6 := r8Info
	r6.Model = "Canon EOS R6"
	thousand := []byte{0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xe8, 0x03, 0, 0, 0xe8, 0x03, 0, 0}
	list := listOf(t, "EOS R6", MethodEventChannel)

	reading, err := readWith(t, list, eventCamera(t, thousand), 0x32f5, r6)
	require.NoError(t, err)
	assert.True(t, reading.Confirmed, "the R6 is added")

	reading, err = readWith(t, list, eventCamera(t, r8D167), 0x330c, r8Info)
	require.NoError(t, err)
	assert.False(t, reading.Confirmed, "the R8 is taken off")
	assert.Equal(t, uint32(2000), reading.Counters[0].Value, "and read just the same")
}

// The failure headline ("Couldn't read" or "Can't read ... yet") comes from
// whether the error is ErrNotSupported, which follows the same match.
func TestTheFailureHeadlineFollowsTheSameMatch(t *testing.T) {
	r50 := r8Info
	r50.Model = "Canon EOS R50"

	for name, c := range map[string]struct {
		list    TestedList
		info    ptp.DeviceInfo
		notYet  bool
		product uint16
	}{
		"the R8, listed":                             {BuiltInTestedList(), r8Info, false, 0x330c},
		"the R8, taken off":                          {listOf(t, "EOS R50", MethodEventChannel), r8Info, true, 0x330c},
		"the R8, listed with another method":         {listOf(t, "EOS R8", MethodFactoryChannel), r8Info, true, 0x330c},
		"the R50, not listed":                        {BuiltInTestedList(), r50, true, 0x3331},
		"the R50, listed":                            {listOf(t, "EOS R50", MethodEventChannel), r50, false, 0x3331},
		"the R50, listed with the standard property": {listOf(t, "EOS R50", MethodStandardProperty), r50, true, 0x3331},
	} {
		_, err := readWith(t, c.list, eventCamera(t, nil), c.product, c.info)
		require.ErrorIs(t, err, errNoReleaseCounter, name)
		assert.Equal(t, c.notYet, errors.Is(err, manufacturers.ErrNotSupported), name)
	}

	// The factory channel, giving nothing, on a body with no event channel.
	info := efInfo
	info.Operations = []uint16{ptp.OpGetDeviceInfo, ptp.OpGetDevicePropValue, OpFactorySend, OpFactoryRecv}

	for name, c := range map[string]struct {
		list   TestedList
		notYet bool
	}{
		byFactory:      {listOf(t, the80D, MethodFactoryChannel), false},
		byEventChannel: {listOf(t, the80D, MethodEventChannel), true},
		"not listed":   {BuiltInTestedList(), true},
	} {
		_, err := readWith(t, c.list, factoryCamera(t, ptp.RCOK), 0x3294, info)
		require.ErrorIs(t, err, errNoCounters, name)
		assert.Equal(t, c.notYet, errors.Is(err, manufacturers.ErrNotSupported), name)
	}
}

func TestModelsMarksComeFromTheList(t *testing.T) {
	confirmed := func(r *Reader) map[string]bool {
		out := map[string]bool{}
		for _, m := range r.Models() {
			out[m.Name] = m.Confirmed
		}

		return out
	}

	assert.Equal(t, map[string]bool{the5DMarkIV: true, theR8: true}, confirmed(New()))
	assert.Equal(t, map[string]bool{the5DMarkIV: false, theR8: true},
		confirmed(New(WithTestedList(listOf(t, "EOS R8", MethodEventChannel, the5DMarkIV, MethodEventChannel)))))
	assert.Equal(t, map[string]bool{the5DMarkIV: true, theR8: false},
		confirmed(New(WithTestedList(listOf(t, the5DMarkIV, MethodFactoryChannel)))))
}

func TestTheListChangesNothingThatsSent(t *testing.T) {
	sent := func(list TestedList, cam *camera, productID uint16, info ptp.DeviceInfo) []string {
		_, _ = readWith(t, list, cam, productID, info)

		return slices.Clone(cam.commands)
	}

	empty := TestedList{}
	everything := listOf(t,
		"EOS R8", MethodEventChannel, "EOS R8", MethodFactoryChannel, "EOS R8", MethodStandardProperty,
		the80D, MethodEventChannel, the80D, MethodFactoryChannel, the80D, MethodStandardProperty)

	for _, list := range []TestedList{empty, everything} {
		assert.Equal(t, sent(BuiltInTestedList(), eventCamera(t, r8D167), 0x330c, r8Info),
			sent(list, eventCamera(t, r8D167), 0x330c, r8Info))
		assert.Equal(t, sent(BuiltInTestedList(), eventCamera(t, nil), 0x330c, r8Info),
			sent(list, eventCamera(t, nil), 0x330c, r8Info))
		assert.Equal(t, sent(BuiltInTestedList(), d80Camera(t), 0x3294, efInfo),
			sent(list, d80Camera(t), 0x3294, efInfo))
		assert.Equal(t, sent(BuiltInTestedList(), factoryCamera(t, ptp.RCOK), 0x3294, efInfo),
			sent(list, factoryCamera(t, ptp.RCOK), 0x3294, efInfo))
	}
}
