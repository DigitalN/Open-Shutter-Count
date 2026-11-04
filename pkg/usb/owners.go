package usb

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Owner is a process that has a device open.
type Owner struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

func (o Owner) String() string { return fmt.Sprintf("%s (pid %d)", o.Name, o.PID) }

// DeviceOwners says which processes have a device open, where the platform
// can tell. It only reads. The second value is the raw listing it was worked
// out from, for the debug log. On macOS it asks ioreg; elsewhere it returns
// nothing.
func DeviceOwners(ctx context.Context, info DeviceInfo) ([]Owner, string, error) {
	return deviceOwners(ctx, info)
}

var (
	ioregDeviceStart = regexp.MustCompile(`<class (IOUSBHostDevice|IOUSBDevice),`)
	ioregVendor      = regexp.MustCompile(`"idVendor" = (\d+)`)
	ioregProduct     = regexp.MustCompile(`"idProduct" = (\d+)`)

	// UsbExclusiveOwner is set on a device or interface a process has
	// opened exclusively; IOUserClientCreator on each connection a process
	// has made to it. Both read "pid 123, name".
	ioregOwner = regexp.MustCompile(`"(?:UsbExclusiveOwner|IOUserClientCreator)" = "pid (\d+), ([^"]*)"`)
)

// ParseIORegOwners reads the output of `ioreg -r -c IOUSBHostDevice -l -w0`
// and returns the processes that have the given device, or its interfaces,
// open, along with that device's part of the output.
func ParseIORegOwners(out string, vendorID, productID uint16) ([]Owner, string) {
	var blocks [][]string

	for line := range strings.Lines(out) {
		if ioregDeviceStart.MatchString(line) || len(blocks) == 0 {
			blocks = append(blocks, nil)
		}

		blocks[len(blocks)-1] = append(blocks[len(blocks)-1], line)
	}

	for _, block := range blocks {
		text := strings.Join(block, "")

		if !matchesID(ioregVendor, text, vendorID) || !matchesID(ioregProduct, text, productID) {
			continue
		}

		var owners []Owner

		for _, m := range ioregOwner.FindAllStringSubmatch(text, -1) {
			pid, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}

			if o := (Owner{PID: pid, Name: m[2]}); !slices.Contains(owners, o) {
				owners = append(owners, o)
			}
		}

		return owners, text
	}

	return nil, ""
}

func matchesID(re *regexp.Regexp, text string, want uint16) bool {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return false
	}

	v, err := strconv.ParseUint(m[1], 10, 16)

	return err == nil && uint16(v) == want
}
