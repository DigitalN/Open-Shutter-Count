package cmd

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/mvisonneau/openshuttercounter/internal/app"
	"github.com/mvisonneau/openshuttercounter/pkg/ptp"
	"github.com/mvisonneau/openshuttercounter/pkg/usb"
)

// probeFile is what `probe` saves: what the camera says it supports, and
// every property it lists, read with standard PTP read requests only
// (GetDeviceInfo, GetDevicePropDesc, GetDevicePropValue). Two of them taken a
// known number of frames apart show whether any property is a shutter count.
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

	if err := session.saveTrace(ctx); err != nil {
		return err
	}

	if probeErr != nil {
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

	return nil
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

// Compare shows what changed between two probes, and whether anything went
// up by exactly the number of frames taken between them.
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

	frames := int64(cmd.Int("frames"))
	if frames <= 0 {
		return errors.New("--frames must be the number of photos taken between the two probes")
	}

	if before.Device.ProductID != after.Device.ProductID || before.Device.Serial != after.Device.Serial {
		fmt.Println("\n  These two probes are from different cameras.")
	}

	previous := make(map[uint16]probedProperty, len(before.Properties))
	for _, p := range before.Properties {
		previous[p.Code] = p
	}

	fmt.Printf("\n  Comparing %d properties, %d photos apart.\n\n", len(after.Properties), frames)

	changed, exact := 0, 0

	for _, p := range after.Properties {
		b, ok := previous[p.Code]
		if !ok || b.Value == p.Value {
			continue
		}

		changed++

		line, isExact := describeChange(b, p, frames)
		if isExact {
			exact++
		}

		fmt.Printf("  %#04x  %-34s %s\n", p.Code, p.Name, line)
	}

	fmt.Printf("\n  %d of %d properties changed.\n", changed, len(after.Properties))

	if exact > 0 {
		fmt.Printf("  Found %d that went up by exactly %d: that's what a shutter count does.\n\n", exact, frames)
	} else {
		fmt.Printf("  Nothing went up by exactly %d, so the shutter count isn't in the\n", frames)
		fmt.Print("  properties this camera lists for standard reads.\n\n")
	}

	return nil
}

// describeChange explains how a property changed. Values of an integer type
// are compared as numbers; anything else is searched for a 16 or 32 bit
// little endian field that went up by exactly frames.
func describeChange(before, after probedProperty, frames int64) (string, bool) {
	b, errB := hex.DecodeString(before.Value)
	a, errA := hex.DecodeString(after.Value)

	if errB != nil || errA != nil {
		return "changed (undecodable)", false
	}

	nb, okB := ptp.Number(before.DataType, b)
	na, okA := ptp.Number(after.DataType, a)

	if okB && okA {
		mark := ""
		if na-nb == frames {
			mark = fmt.Sprintf("   <== up by exactly %d", frames)
		}

		return fmt.Sprintf("%d -> %d (%+d)%s", nb, na, na-nb, mark), na-nb == frames
	}

	var hits []string

	if len(a) == len(b) {
		for off := 0; off+2 <= len(a); off++ {
			if int64(binary.LittleEndian.Uint16(a[off:]))-int64(binary.LittleEndian.Uint16(b[off:])) == frames {
				hits = append(hits, fmt.Sprintf("16 bits at byte %d", off))
			}

			if off+4 <= len(a) &&
				int64(binary.LittleEndian.Uint32(a[off:]))-int64(binary.LittleEndian.Uint32(b[off:])) == frames {
				hits = append(hits, fmt.Sprintf("32 bits at byte %d", off))
			}
		}
	}

	if len(hits) > 0 {
		return fmt.Sprintf("changed; up by exactly %d in: %s   <==", frames, strings.Join(hits, ", ")), true
	}

	return fmt.Sprintf("changed (%d -> %d bytes)", len(b), len(a)), false
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
