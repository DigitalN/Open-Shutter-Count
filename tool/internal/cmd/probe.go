package cmd

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/DigitalN/Open-Shutter-Count/tool/internal/app"
	"github.com/DigitalN/Open-Shutter-Count/tool/internal/logs"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/manufacturers/canon"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/ptp"
	"github.com/DigitalN/Open-Shutter-Count/tool/pkg/usb"
)

// probeFile is what `probe` saves: what the camera says it supports, and
// every property it lists, read with standard PTP read requests only
// (GetDeviceInfo, GetDevicePropDesc, GetDevicePropValue). With --events, it
// also holds everything a Canon body reports on its event channel. Two of them
// taken a known number of frames apart show whether anything is a shutter
// count.
type probeFile struct {
	Tool          string           `json:"tool"`
	ToolVersion   string           `json:"tool_version"`
	RecordedAt    time.Time        `json:"recorded_at"`
	Notes         string           `json:"notes,omitempty"`
	Device        usb.DeviceInfo   `json:"device"`
	DeviceInfo    ptp.DeviceInfo   `json:"device_info"`
	DeviceInfoRaw string           `json:"device_info_raw"`
	Operations    []string         `json:"operations"`
	Properties    []probedProperty `json:"properties"`
	Events        *probedEvents    `json:"events,omitempty"`
}

// probedEvents is what a Canon body reported on its event channel.
type probedEvents struct {
	// Steps says what was sent and how the camera answered.
	Steps []string `json:"steps"`

	// Replies are the GetEvent data phases, hex encoded, in order.
	Replies []string `json:"replies"`

	// Records are every record in Replies, in order.
	Records []probedRecord `json:"records"`

	Error string `json:"error,omitempty"`
}

// probedRecord is one event record.
type probedRecord struct {
	// Key names the record for comparing two probes: "prop 0xd167" for a
	// property value, "allowed 0xd167" for a property's allowed values,
	// and "event 0xc1a5 #2" for the second record of any other code.
	Key  string `json:"key"`
	Code uint32 `json:"code"`
	Prop uint32 `json:"prop,omitempty"`

	// Data is the record's data after its code, or after its property code
	// for a property, hex encoded.
	Data string `json:"data"`
}

type probedProperty struct {
	Code     uint16 `json:"code"`
	Name     string `json:"name,omitempty"`
	DataType uint16 `json:"data_type,omitempty"`
	Type     string `json:"type,omitempty"`
	Writable bool   `json:"writable,omitempty"`
	// Value is the current value, hex encoded.
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

// propertyNames are the standard PTP (PIMA 15740) and MTP property names.
var propertyNames = map[uint16]string{
	0x5001: "BatteryLevel", 0x5002: "FunctionalMode", 0x5003: "ImageSize",
	0x5004: "CompressionSetting", 0x5005: "WhiteBalance", 0x5006: "RGBGain",
	0x5007: "FNumber", 0x5008: "FocalLength", 0x5009: "FocusDistance",
	0x500a: "FocusMode", 0x500b: "ExposureMeteringMode", 0x500c: "FlashMode",
	0x500d: "ExposureTime", 0x500e: "ExposureProgramMode", 0x500f: "ExposureIndex",
	0x5010: "ExposureBiasCompensation", 0x5011: "DateTime", 0x5012: "CaptureDelay",
	0x5013: "StillCaptureMode", 0x5014: "Contrast", 0x5015: "Sharpness",
	0x5016: "DigitalZoom", 0x5017: "EffectMode", 0x5018: "BurstNumber",
	0x5019: "BurstInterval", 0x501a: "TimelapseNumber", 0x501b: "TimelapseInterval",
	0x501c: "FocusMeteringMode", 0x501d: "UploadURL", 0x501e: "Artist",
	0x501f: "CopyrightInfo",
	0xd1ac: "CanonShutterCounter (pre DIGIC 6)",
	0xd401: "SynchronizationPartner", 0xd402: "DeviceFriendlyName", 0xd403: "Volume",
	0xd404: "SupportedFormatsOrdered", 0xd405: "DeviceIcon",
	0xd406: "SessionInitiatorVersionInfo", 0xd407: "PerceivedDeviceType",
}

// Probe saves everything the camera reports through standard read requests.
func Probe(ctx context.Context, cmd *cli.Command) error {
	out := cmd.String("out")
	if out == "" {
		return errors.New("--out is required: the file to save what the camera reports in")
	}

	session, err := openSession(ctx, cmd)
	if err != nil {
		return err
	}

	defer session.close()

	file, probeErr := probe(session)
	file.Notes = cmd.String("note")

	if probeErr == nil && cmd.Bool("events") {
		probeErr = probeEvents(ctx, session, &file)
	}

	if err := session.saveTrace(ctx); err != nil {
		return err
	}

	// What the event channel gave before failing is still worth saving.
	if probeErr != nil && file.Events == nil {
		return probeErr
	}

	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(out, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("saving %s: %w", out, err)
	}

	printProbe(file)
	fmt.Printf("\n  Saved in %s\n\n", out)

	return probeErr
}

// probeEvents reads a Canon body's event channel into file.
func probeEvents(ctx context.Context, s *session, file *probeFile) error {
	if s.reader.Name() != "Canon" {
		return fmt.Errorf("--events reads Canon's event channel, and this is a %s camera", s.reader.Name())
	}

	table, err := canon.ReadEventTable(s.device, file.DeviceInfo, logs.LoggerFromContext(ctx))
	if errors.Is(err, canon.ErrNoEventChannel) {
		return err
	}

	events := &probedEvents{Steps: table.Steps}
	if err != nil {
		events.Error = err.Error()
	}

	for _, reply := range table.Replies {
		events.Replies = append(events.Replies, hex.EncodeToString(reply))
	}

	seen := map[uint32]int{}

	for _, r := range table.Records {
		key := fmt.Sprintf("prop %#04x", r.Prop)

		switch r.Code {
		case canon.EventPropValueChanged:
		case canon.EventAvailListChanged:
			key = fmt.Sprintf("allowed %#04x", r.Prop)
		default:
			seen[r.Code]++
			key = fmt.Sprintf("event %#04x #%d", r.Code, seen[r.Code])
		}

		events.Records = append(events.Records, probedRecord{
			Key: key, Code: r.Code, Prop: r.Prop, Data: hex.EncodeToString(r.Data),
		})
	}

	file.Events = events

	return err
}

func probe(s *session) (probeFile, error) {
	file := probeFile{
		Tool:        app.Name,
		ToolVersion: app.Version,
		RecordedAt:  time.Now().UTC(),
		Device:      s.info,
	}

	info, raw, err := s.device.GetDeviceInfo()
	if err != nil {
		return file, err
	}

	file.DeviceInfo, file.DeviceInfoRaw = info, hex.EncodeToString(raw)

	for _, op := range info.Operations {
		file.Operations = append(file.Operations, fmt.Sprintf("%#04x %s", op, opcodeLabel(op)))
	}

	for _, code := range info.Properties {
		p, err := readProperty(s.device, info, code)
		if err != nil {
			// The transport failed, so the camera has stopped answering.
			return file, fmt.Errorf("reading property %#04x: %w", code, err)
		}

		file.Properties = append(file.Properties, p)
	}

	return file, nil
}

// readProperty reads one property with whichever standard read request the
// camera lists. A refusal is recorded; only a transport failure is an error.
func readProperty(dev *ptp.Device, info ptp.DeviceInfo, code uint16) (probedProperty, error) {
	p := probedProperty{Code: code, Name: propertyNames[code]}

	var refusals []string

	if info.Supports(ptp.OpGetDevicePropDesc) {
		refusal, err := readPropDesc(dev, &p)
		if err != nil || refusal == "" {
			return p, err
		}

		refusals = append(refusals, refusal)
	}

	if info.Supports(ptp.OpGetDevicePropValue) {
		resp, err := dev.Transact(ptp.OpGetDevicePropValue, uint32(code))
		if err != nil {
			return p, err
		}

		if resp.OK() {
			p.Value = hex.EncodeToString(resp.Data)

			return p, nil
		}

		refusals = append(refusals, "GetDevicePropValue: "+ptp.ResponseName(resp.Code))
	}

	if len(refusals) == 0 {
		refusals = append(refusals, "the camera lists no request for reading properties")
	}

	p.Error = strings.Join(refusals, "; ")

	return p, nil
}

// readPropDesc fills in p from GetDevicePropDesc. It returns why the camera
// refused, or "" when p was filled in.
func readPropDesc(dev *ptp.Device, p *probedProperty) (string, error) {
	resp, err := dev.Transact(ptp.OpGetDevicePropDesc, uint32(p.Code))
	if err != nil {
		return "", err
	}

	if !resp.OK() {
		return "GetDevicePropDesc: " + ptp.ResponseName(resp.Code), nil
	}

	desc, parseErr := ptp.ParsePropDesc(resp.Data)
	if parseErr != nil {
		// A description this can't parse is recorded like a refusal, so the
		// value read can still be tried.
		return parseErr.Error(), nil //nolint:nilerr // a parse failure is a refusal, not a transport failure
	}

	p.DataType, p.Type, p.Writable = desc.DataType, ptp.TypeName(desc.DataType), desc.Writable
	p.Value = hex.EncodeToString(desc.Current)

	return "", nil
}

func printProbe(file probeFile) {
	info := file.DeviceInfo

	fmt.Printf("\n  %s %s, firmware %s\n", info.Manufacturer, info.Model, info.DeviceVersion)
	fmt.Printf("  It lists %d operations, %d events and %d properties.\n\n",
		len(info.Operations), len(info.Events), len(info.Properties))

	for _, p := range file.Properties {
		value := p.Error
		if p.Error == "" {
			value = displayValue(p)
		}

		fmt.Printf("  %#04x  %-34s %-7s %s\n", p.Code, p.Name, p.Type, value)
	}

	if file.Events == nil {
		return
	}

	ev := file.Events
	fmt.Printf("\n  Canon event channel: %d records in %d replies.\n", len(ev.Records), len(ev.Replies))

	if ev.Error != "" {
		fmt.Printf("  It stopped with: %s\n", ev.Error)
	}

	fmt.Printf("  %s\n", describeD167(ev))
}

// d167Key is how property 0xD167 is keyed in a probe.
var d167Key = fmt.Sprintf("prop %#04x", canon.PropShutterReleaseCounter)

// describeD167 shows property 0xD167 as plain 32 bit words, by position. What
// each word counts is not established, so they aren't named.
func describeD167(ev *probedEvents) string {
	value, ok := eventTable(ev)[d167Key]
	if !ok {
		return "0xd167: not reported"
	}

	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw)%4 != 0 {
		return "0xd167: " + value
	}

	words := make([]string, 0, len(raw)/4)
	for i := 0; i+4 <= len(raw); i += 4 {
		words = append(words, fmt.Sprintf("word %d: %d", i/4+1, binary.LittleEndian.Uint32(raw[i:])))
	}

	return "0xd167 " + strings.Join(words, ", ")
}

// eventTable maps each record's key to its last value.
func eventTable(ev *probedEvents) map[string]string {
	table := map[string]string{}
	if ev == nil {
		return table
	}

	for _, r := range ev.Records {
		table[r.Key] = r.Data
	}

	return table
}

func displayValue(p probedProperty) string {
	raw, err := hex.DecodeString(p.Value)
	if err != nil {
		return p.Value
	}

	if n, ok := ptp.Number(p.DataType, raw); ok {
		return strconv.FormatInt(n, 10)
	}

	if p.DataType == ptp.TypeString {
		return strconv.Quote(ptp.String(raw))
	}

	const maxHex = 48
	if len(p.Value) > maxHex {
		return p.Value[:maxHex] + "…"
	}

	return p.Value
}

// target is a number of photos to look for a counter going up by.
type target struct {
	n     uint64
	label string
}

// labelPhotosTaken is the target's label when only one shutter mode was used.
const labelPhotosTaken = "photos taken"

// Compare shows what changed between two probes, and whether anything went
// up by exactly the number of photos taken between them.
func Compare(_ context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 2 {
		return errors.New("expected two probe files: before and after")
	}

	before, err := readProbe(cmd.Args().Get(0))
	if err != nil {
		return err
	}

	after, err := readProbe(cmd.Args().Get(1))
	if err != nil {
		return err
	}

	targets, err := compareTargets(cmd.Int("frames"), cmd.Int("electronic"))
	if err != nil {
		return err
	}

	if before.Device.ProductID != after.Device.ProductID || before.Device.Serial != after.Device.Serial {
		fmt.Println("\n  These two probes are from different cameras.")
	}

	labels := make([]string, len(targets))
	for i, t := range targets {
		labels[i] = fmt.Sprintf("%d (%s)", t.n, t.label)
	}

	fmt.Printf("\n  Looking for anything that went up by exactly %s.\n", strings.Join(labels, ", or "))

	found := compareProperties(before, after, targets)

	if before.Events != nil && after.Events != nil {
		found += compareEvents(before.Events, after.Events, targets)
	} else {
		fmt.Print("\n  The event channel isn't in both probes, so only properties were compared.\n")
	}

	if found == 0 {
		fmt.Printf("\n  Nothing went up by exactly %s.\n\n", strings.Join(labels, " or "))
	} else {
		fmt.Printf("\n  Found %d matches, marked <== above. One field can match at several widths.\n\n", found)
	}

	return nil
}

// compareTargets is what to look for: the photos taken with the shutter
// moving, those with the electronic shutter, and all of them.
func compareTargets(frames, electronic int) ([]target, error) {
	if frames <= 0 {
		return nil, errors.New("--frames must be the number of photos taken with the shutter moving")
	}

	if electronic < 0 {
		return nil, errors.New("--electronic can't be negative")
	}

	f, e := uint64(frames), uint64(electronic)

	if e == 0 {
		return []target{{f, labelPhotosTaken}}, nil
	}

	if e == f {
		return nil, errors.New("--frames and --electronic must differ, or a match couldn't say which it counts")
	}

	return []target{
		{f, "photos with the shutter moving"},
		{e, "electronic shutter photos"},
		{f + e, "all photos"},
	}, nil
}

// compareProperties compares the standard properties, returning how many
// fields matched a target.
func compareProperties(before, after probeFile, targets []target) int {
	previous := make(map[uint16]probedProperty, len(before.Properties))
	for _, p := range before.Properties {
		previous[p.Code] = p
	}

	fmt.Printf("\n  Standard properties (%d):\n", len(after.Properties))

	changed, found := 0, 0

	for _, p := range after.Properties {
		b, ok := previous[p.Code]
		if !ok || b.Value == p.Value {
			continue
		}

		changed++

		lines, hits := describeChange(b, p, targets)
		found += hits

		fmt.Printf("    %#04x  %-34s %s\n", p.Code, p.Name, lines[0])

		for _, line := range lines[1:] {
			fmt.Printf("    %6s  %-34s %s\n", "", "", line)
		}
	}

	fmt.Printf("    %d of %d changed.\n", changed, len(after.Properties))

	return found
}

// compareEvents compares two event tables record by record, returning how
// many fields matched a target.
func compareEvents(before, after *probedEvents, targets []target) int {
	b, a := eventTable(before), eventTable(after)

	fmt.Printf("\n  Canon event channel (%d records before, %d after):\n", len(b), len(a))
	fmt.Printf("    before: %s\n", describeD167(before))
	fmt.Printf("    after:  %s\n", describeD167(after))

	keys := make([]string, 0, len(a))
	for k := range a {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	changed, found, onlyAfter := 0, 0, 0

	for _, k := range keys {
		old, ok := b[k]

		switch {
		case !ok:
			onlyAfter++

			continue
		case old == a[k]:
			continue
		}

		changed++

		lines, hits := describeBytes(old, a[k], targets)
		found += hits

		fmt.Printf("    %-22s %s\n", k, lines[0])

		for _, line := range lines[1:] {
			fmt.Printf("    %-22s %s\n", "", line)
		}
	}

	onlyBefore := 0

	for k := range b {
		if _, ok := a[k]; !ok {
			onlyBefore++
		}
	}

	fmt.Printf("    %d of %d records changed; %d only before, %d only after.\n", changed, len(a), onlyBefore, onlyAfter)

	return found
}

// describeChange explains how a property changed. Values of an integer type
// are compared as numbers; anything else is searched byte by byte.
func describeChange(before, after probedProperty, targets []target) ([]string, int) {
	b, errB := hex.DecodeString(before.Value)
	a, errA := hex.DecodeString(after.Value)

	if errB != nil || errA != nil {
		return []string{"changed (undecodable)"}, 0
	}

	nb, okB := ptp.Number(before.DataType, b)
	na, okA := ptp.Number(after.DataType, a)

	if !okB || !okA {
		return describeBytes(before.Value, after.Value, targets)
	}

	line := fmt.Sprintf("%d -> %d (%+d)", nb, na, na-nb)

	if na <= nb {
		return []string{line}, 0
	}

	if t, ok := matchTarget(uint64(na-nb), targets); ok { //nolint:gosec // na > nb was checked above
		return []string{fmt.Sprintf("%s   <== %s", line, t.label)}, 1
	}

	return []string{line}, 0
}

// describeBytes searches two hex encoded values for a field that went up by
// exactly a target: at every byte offset, 2, 4 and 8 bytes wide, both byte
// orders. The first line says how the value changed; one line per match
// follows.
func describeBytes(beforeHex, afterHex string, targets []target) ([]string, int) {
	b, errB := hex.DecodeString(beforeHex)
	a, errA := hex.DecodeString(afterHex)

	if errB != nil || errA != nil {
		return []string{"changed (undecodable)"}, 0
	}

	lines := []string{fmt.Sprintf("changed (%d -> %d bytes)", len(b), len(a))}

	if len(a) != len(b) {
		return lines, 0
	}

	orders := []struct {
		name  string
		order binary.ByteOrder
	}{{"little endian", binary.LittleEndian}, {"big endian", binary.BigEndian}}

	for _, width := range []int{2, 4, 8} {
		for _, o := range orders {
			for off := 0; off+width <= len(a); off++ {
				vb, va := field(o.order, b[off:off+width]), field(o.order, a[off:off+width])
				if va <= vb {
					continue
				}

				if t, ok := matchTarget(va-vb, targets); ok {
					lines = append(lines, fmt.Sprintf("%d bits %s at byte %d: %d -> %d   <== %s",
						width*8, o.name, off, vb, va, t.label))
				}
			}
		}
	}

	return lines, len(lines) - 1
}

func field(order binary.ByteOrder, b []byte) uint64 {
	switch len(b) {
	case 2:
		return uint64(order.Uint16(b))
	case 4:
		return uint64(order.Uint32(b))
	default:
		return order.Uint64(b)
	}
}

func matchTarget(delta uint64, targets []target) (target, bool) {
	for _, t := range targets {
		if delta == t.n {
			return t, true
		}
	}

	return target{}, false
}

func readProbe(path string) (probeFile, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // reading the file the user named is the point
	if err != nil {
		return probeFile{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var file probeFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return probeFile{}, fmt.Errorf("%s isn't a probe file: %w", path, err)
	}

	return file, nil
}
