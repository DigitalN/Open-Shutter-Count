package canon

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
)

// builtInTested is tested.json, built into the tool. Installed copies of the
// app fetch the same file from main at launch and pass it with --tested-list,
// so a body can be marked tested without a release. Its path in the
// repository is permanent: those copies fetch it by URL.
//
//go:embed tested.json
var builtInTested []byte

// TestedEntry is one body in the tested list.
type TestedEntry struct {
	// Model is the name the camera reports, as modelName gives it. It must
	// match exactly.
	Model string `json:"model"`

	// Method is how its reading was worked out on the body: one of Methods.
	Method string `json:"method"`

	// Note says how and when it was tested. No code reads it.
	Note string `json:"note,omitempty"`
}

// Methods are the methods a tested list entry can name.
var Methods = []string{MethodStandardProperty, MethodFactoryChannel, MethodEventChannel}

// TestedList is the bodies whose readings were worked out on the body itself,
// each with the method that was worked out. A reading is tested when the list
// has its model and the method it was read with; any other reading is
// labelled as not fully tested.
//
// The list decides that label, and whether a failed read is one that can't be
// read yet. Never what's sent to the camera, the reading rule, or how counts
// are worked out: a list fetched from GitHub isn't signed, and this limit is
// what makes that acceptable.
type TestedList struct {
	entries []TestedEntry
}

// Has reports whether the list has this model, read with this method.
func (l TestedList) Has(model, method string) bool {
	return slices.ContainsFunc(l.entries, func(e TestedEntry) bool {
		return e.Model == model && e.Method == method
	})
}

// Len is how many entries counted.
func (l TestedList) Len() int { return len(l.entries) }

// Entries returns the entries that counted.
func (l TestedList) Entries() []TestedEntry { return slices.Clone(l.entries) }

// ParseTestedList reads a tested list, leniently: fields it doesn't know are
// ignored, so a field added later doesn't make older copies skip entries, and
// an entry with no model or an unknown method is skipped, with the reason in
// skipped. It fails only when raw isn't a JSON object with a "tested" list, or
// no entry in it is usable.
func ParseTestedList(raw []byte) (TestedList, []string, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		if _, ok := errors.AsType[*json.SyntaxError](err); ok {
			return TestedList{}, nil, fmt.Errorf("it isn't valid JSON: %w", err)
		}

		return TestedList{}, nil, errors.New("it isn't a JSON object")
	}

	listed, ok := top["tested"]
	if !ok {
		return TestedList{}, nil, errors.New(`it has no "tested" list`)
	}

	var items []json.RawMessage
	if err := json.Unmarshal(listed, &items); err != nil {
		return TestedList{}, nil, errors.New(`its "tested" isn't a list`)
	}

	var (
		list    TestedList
		skipped []string
	)

	for i, item := range items {
		var entry TestedEntry

		switch err := json.Unmarshal(item, &entry); {
		case err != nil:
			skipped = append(skipped, fmt.Sprintf("entry %d isn't an object with a text model and method", i+1))
		case strings.TrimSpace(entry.Model) == "":
			skipped = append(skipped, fmt.Sprintf("entry %d has no model", i+1))
		case !slices.Contains(Methods, entry.Method):
			skipped = append(skipped, fmt.Sprintf("entry %d (%q) has an unknown method, %q", i+1, entry.Model, entry.Method))
		default:
			list.entries = append(list.entries, entry)
		}
	}

	if list.Len() == 0 {
		return TestedList{}, skipped, errors.New("it has no usable entry")
	}

	return list, skipped, nil
}

// builtIn parses tested.json once. The tests hold it to a strict check, so it
// can't fail in a build; if it somehow did, every reading would say it's not
// fully tested, which is the safe way round, and no read would fail.
var builtIn = sync.OnceValue(func() TestedList {
	list, _, err := ParseTestedList(builtInTested)
	if err != nil {
		return TestedList{}
	}

	return list
})

// BuiltInTestedList is tested.json as it was when the tool was built.
func BuiltInTestedList() TestedList { return builtIn() }

// Where the tested list a command used came from.
const (
	TestedListGiven   = "given"
	TestedListBuiltIn = "built in"
)

// maxTestedListSize is more than any real list will reach. A bigger file
// isn't read.
const maxTestedListSize = 1 << 20

// maxSkippedSteps is how many skipped entries the steps name one by one.
const maxSkippedSteps = 10

// TestedListReport says which tested list a command used, and why a given one
// wasn't.
type TestedListReport struct {
	// Given says a list was given with --tested-list.
	Given bool

	// Source is TestedListGiven or TestedListBuiltIn.
	Source string

	// Entries is how many entries counted in the list used.
	Entries int

	// Reason says why a given list wasn't used.
	Reason string

	// Skipped says which of a given list's entries were skipped, and why.
	Skipped []string
}

// LoadTestedList reads the tested list in path, or gives the built-in one
// when path is empty. It never fails: a file that can't be read, doesn't parse
// or has no usable entry gives the built-in list, and the report says why. A
// usable list replaces the built-in one entirely, so bodies can be taken off
// as well as added.
func LoadTestedList(path string) (TestedList, TestedListReport) {
	builtInList := BuiltInTestedList()
	report := TestedListReport{Source: TestedListBuiltIn, Entries: builtInList.Len()}

	if path == "" {
		return builtInList, report
	}

	report.Given = true

	raw, err := readTestedList(path)
	if err != nil {
		report.Reason = err.Error()

		return builtInList, report
	}

	list, skipped, err := ParseTestedList(raw)
	report.Skipped = skipped

	if err != nil {
		report.Reason = err.Error()

		return builtInList, report
	}

	report.Source, report.Entries = TestedListGiven, list.Len()

	return list, report
}

// readTestedList reads a given list. What went wrong is said without the
// path, since it ends up in the app's log.
func readTestedList(path string) ([]byte, error) {
	file, err := os.Open(path) //nolint:gosec // the file named on the command line
	if err != nil {
		return nil, fmt.Errorf("it couldn't be read: %w", withoutPath(err))
	}

	defer func() { _ = file.Close() }()

	raw, err := io.ReadAll(io.LimitReader(file, maxTestedListSize+1))
	if err != nil {
		return nil, fmt.Errorf("it couldn't be read: %w", withoutPath(err))
	}

	if len(raw) > maxTestedListSize {
		return nil, fmt.Errorf("it's over %d bytes", maxTestedListSize)
	}

	return raw, nil
}

func withoutPath(err error) error {
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		return pathErr.Err
	}

	return err
}

// Steps says which list was used, for a read's steps. Without --tested-list
// it says nothing, so a read without it is told exactly as before.
func (r TestedListReport) Steps() []string {
	if !r.Given {
		return nil
	}

	var steps []string

	if r.Source == TestedListGiven {
		steps = append(steps, fmt.Sprintf("tested list: the one given, %d entries", r.Entries))
	} else {
		steps = append(steps, fmt.Sprintf("tested list: the built-in one, %d entries, as the one given couldn't be used: %s",
			r.Entries, r.Reason))
	}

	for i, why := range r.Skipped {
		if i == maxSkippedSteps {
			steps = append(steps, fmt.Sprintf("tested list: %d more entries skipped", len(r.Skipped)-maxSkippedSteps))

			break
		}

		steps = append(steps, "tested list: skipped "+why)
	}

	return steps
}
