package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/DigitalN/openshuttercounter/tool/pkg/manufacturers/canon"
)

func writeTestedList(t *testing.T, text string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "tested.json")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))

	return path
}

// Names the tests below use more than once.
const (
	theR8       = "EOS R8"
	the5DMarkIV = "EOS 5D Mark IV"
)

// listedAs is a read's "tested_list", as the app reads it.
func listedAs(source string, entries int, reason string) map[string]any {
	out := map[string]any{"source": source, "entries": float64(entries)}
	if reason != "" {
		out["reason"] = reason
	}

	return out
}

// addsTheR6 has the R6 and not the R8.
const addsTheR6 = `{"tested": [
  {"model": "EOS R6", "method": "canon-event-channel-0xd167", "note": "a stand-in"},
  {"model": "EOS 5D Mark IV", "method": "canon-factory-channel"}
]}`

// thousand is libgphoto2's R6 0xD167: 1000 and 1000.
var thousand = []byte{0x10, 0, 0, 0, 0x01, 0, 0, 0, 0xe8, 0x03, 0, 0, 0xe8, 0x03, 0, 0}

// readAs reads a scripted camera as `read --json --tested-list path` would,
// and gives back what it printed, as the app reads it.
func readAs(t *testing.T, model string, productID uint16, d167 []byte, path string) map[string]any {
	t.Helper()

	out, _ := readFixtureWith(t, fakeCamera(t, model, productID, eventChannel, d167), path)

	raw, err := json.Marshal(out)
	require.NoError(t, err)

	var printed map[string]any
	require.NoError(t, json.Unmarshal(raw, &printed))

	return printed
}

func confirmed(printed map[string]any) bool {
	reading, _ := printed["reading"].(map[string]any)

	return reading["confirmed"] == true
}

func steps(printed map[string]any) []string {
	list, _ := printed["steps"].([]any)
	out := make([]string, 0, len(list))

	for _, s := range list {
		out = append(out, fmt.Sprint(s))
	}

	return out
}

func TestAGivenListReplacesTheBuiltInOneInARead(t *testing.T) {
	path := writeTestedList(t, addsTheR6)

	r6 := readAs(t, "Canon EOS R6", 0x32f5, thousand, path)
	assert.Nil(t, r6["error"])
	assert.True(t, confirmed(r6), "the R6 is added")
	assert.Equal(t, listedAs("given", 2, ""), r6["tested_list"])
	assert.Equal(t, "tested list: the one given, 2 entries", steps(r6)[0])

	r8 := readAs(t, "Canon EOS R8", 0x330c, r8D167, path)
	assert.Nil(t, r8["error"])
	assert.False(t, confirmed(r8), "the R8 is taken off")

	// Without it, the built-in list, and nothing new in the steps.
	r8 = readAs(t, "Canon EOS R8", 0x330c, r8D167, "")
	assert.True(t, confirmed(r8))
	assert.Equal(t, listedAs("built in", canon.BuiltInTestedList().Len(), ""),
		r8["tested_list"])
	assert.Equal(t, "OpenSession: OK", steps(r8)[0])
}

func TestAGivenListThatCantBeUsedStillReads(t *testing.T) {
	builtIn := canon.BuiltInTestedList().Len()

	for name, c := range map[string]struct {
		path, reason string
	}{
		"missing":         {filepath.Join(t.TempDir(), "nowhere.json"), "it couldn't be read: no such file or directory"},
		"bad JSON":        {writeTestedList(t, `{"tested": [{"model": "EOS R8",`), "it isn't valid JSON: unexpected end of JSON input"},
		"empty":           {writeTestedList(t, `{"tested": []}`), "it has no usable entry"},
		"unknown methods": {writeTestedList(t, `{"tested": [{"model": "EOS R8", "method": "x"}]}`), "it has no usable entry"},
	} {
		r8 := readAs(t, "Canon EOS R8", 0x330c, r8D167, c.path)

		assert.Nil(t, r8["error"], name)
		assert.True(t, confirmed(r8), "%s: the R8 is tested on the built-in list", name)
		assert.Equal(t, listedAs("built in", builtIn, c.reason), r8["tested_list"], name)
		assert.Equal(t,
			fmt.Sprintf("tested list: the built-in one, %d entries, as the one given couldn't be used: %s", builtIn, c.reason),
			steps(r8)[0], name)
	}
}

func TestEntriesWithUnknownMethodsAreSkippedInARead(t *testing.T) {
	path := writeTestedList(t, `{"tested": [
  {"model": "EOS R8", "method": "canon-telepathy"},
  {"model": "", "method": "canon-event-channel-0xd167"},
  {"model": "EOS R6", "method": "canon-event-channel-0xd167"}
]}`)

	r8 := readAs(t, "Canon EOS R8", 0x330c, r8D167, path)
	assert.Nil(t, r8["error"])
	assert.False(t, confirmed(r8), "the R8's entry was skipped, and the list given is used")
	assert.Equal(t, listedAs("given", 1, ""), r8["tested_list"])
	assert.Equal(t, []string{
		"tested list: the one given, 1 entries",
		`tested list: skipped entry 1 ("EOS R8") has an unknown method, "canon-telepathy"`,
		"tested list: skipped entry 2 has no model",
		"OpenSession: OK",
	}, steps(r8)[:4])

	r6 := readAs(t, "Canon EOS R6", 0x32f5, thousand, path)
	assert.True(t, confirmed(r6))
}

func TestACameraThatCantBeOpenedStillSaysWhichList(t *testing.T) {
	_, listed := canon.LoadTestedList(writeTestedList(t, `{"tested": [`))
	out := failureOutput(unopened(listed, nil), noDeviceError{})

	assert.Equal(t, kindNoCamera, out.Error.Kind)
	assert.Equal(t, "built in", out.TestedList.Source)
	assert.NotEmpty(t, out.TestedList.Reason)
	assert.Contains(t, out.Steps[0], "the one given couldn't be used")
}

// runModels runs `models` with args, and gives back what it printed.
func runModels(t *testing.T, args ...string) string {
	t.Helper()

	command := &cli.Command{
		Name:   "models",
		Action: Models,
		Flags:  []cli.Flag{&cli.BoolFlag{Name: "json"}, &cli.StringFlag{Name: "tested-list"}},
	}

	read, write, err := os.Pipe()
	require.NoError(t, err)

	stdout := os.Stdout
	os.Stdout = write

	runErr := command.Run(context.Background(), append([]string{"models"}, args...))

	os.Stdout = stdout

	require.NoError(t, write.Close())

	printed, err := io.ReadAll(read)
	require.NoError(t, err)
	require.NoError(t, runErr, "models never fails because of the list")

	return string(printed)
}

// marks is each body `models` lists, with whether it says confirmed.
func marks(t *testing.T, printed string) map[string]bool {
	t.Helper()

	out := map[string]bool{}

	for line := range strings.SplitSeq(printed, "\n") {
		for _, name := range []string{the5DMarkIV, theR8} {
			if strings.HasPrefix(strings.TrimSpace(line), name+" ") {
				out[name] = strings.HasSuffix(line, "(confirmed)")
			}
		}
	}

	require.Len(t, out, 2, printed)

	return out
}

func TestModelsMarksComeFromTheGivenList(t *testing.T) {
	printed := runModels(t)
	assert.Equal(t, map[string]bool{the5DMarkIV: true, theR8: true}, marks(t, printed))
	assert.NotContains(t, printed, "Tested list", "without --tested-list, it says what it always did")

	printed = runModels(t, "--tested-list", writeTestedList(t, addsTheR6))
	assert.Equal(t, map[string]bool{the5DMarkIV: true, theR8: false}, marks(t, printed))
	assert.Contains(t, printed, "  Tested list: the one given, 2 entries\n")
}

func TestModelsWithAListThatCantBeUsedShowsTheBuiltInMarks(t *testing.T) {
	for name, path := range map[string]string{
		"missing":         filepath.Join(t.TempDir(), "nowhere.json"),
		"bad JSON":        writeTestedList(t, `{"tested": [`),
		"empty":           writeTestedList(t, `{"tested": []}`),
		"unknown methods": writeTestedList(t, `{"tested": [{"model": "EOS R8", "method": "x"}]}`),
	} {
		printed := runModels(t, "--tested-list", path)

		assert.Equal(t, map[string]bool{the5DMarkIV: true, theR8: true}, marks(t, printed), name)
		assert.Contains(t, printed, "  Tested list: the built-in one, ", name)
		assert.Contains(t, printed, "as the one given couldn't be used: ", name)
	}

	printed := runModels(t, "--tested-list", writeTestedList(t, `{"tested": [
  {"model": "EOS R8", "method": "canon-event-channel-0xd167"},
  {"model": "EOS 5D Mark IV", "method": "canon-telepathy"}
]}`))
	assert.Equal(t, map[string]bool{the5DMarkIV: false, theR8: true}, marks(t, printed))
	assert.Contains(t, printed, `  Tested list: skipped entry 2 ("EOS 5D Mark IV") has an unknown method, "canon-telepathy"`)
}

func TestModelsJSONStaysAListWithAGivenList(t *testing.T) {
	for _, path := range []string{writeTestedList(t, addsTheR6), writeTestedList(t, "nonsense")} {
		var out []struct {
			Models []struct {
				Name      string `json:"name"`
				Confirmed bool   `json:"confirmed"`
			} `json:"models"`
		}

		require.NoError(t, json.Unmarshal([]byte(runModels(t, "--json", "--tested-list", path)), &out))
		require.Len(t, out, 1)
		assert.Len(t, out[0].Models, 2)
	}
}
