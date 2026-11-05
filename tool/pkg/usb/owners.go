package usb

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Owner is a process that has a device open.
type Owner struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`

	// Exclusive is set when it has an interface to itself, which is what
	// stops anyone else claiming it.
	Exclusive bool `json:"exclusive,omitempty"`
}

func (o Owner) String() string {
	if o.Exclusive {
		return fmt.Sprintf("%s (pid %d, exclusive)", o.Name, o.PID)
	}

	return fmt.Sprintf("%s (pid %d)", o.Name, o.PID)
}

// DeviceOwners says which processes have a device open, where the platform
// can tell. It only reads. The second value is a short summary of where each
// was found, for the debug log; it leaves out serial numbers and every other
// property. On macOS it asks ioreg; elsewhere it returns nothing.
func DeviceOwners(ctx context.Context, info DeviceInfo) ([]Owner, string, error) {
	return deviceOwners(ctx, info)
}

var (
	ioregDeviceStart = regexp.MustCompile(`<class (IOUSBHostDevice|IOUSBDevice),`)
	ioregObject      = regexp.MustCompile(`\+-o (.+?)\s+<class (\w+),`)
	ioregVendor      = regexp.MustCompile(`"idVendor" = (\d+)`)
	ioregProduct     = regexp.MustCompile(`"idProduct" = (\d+)`)

	// UsbExclusiveOwner is set on a device or interface a process has
	// opened exclusively; IOUserClientCreator on each connection a process
	// has made to it. Both read "pid 123, name", the name cut to 16
	// characters.
	ioregOwner = regexp.MustCompile(`"(UsbExclusiveOwner|IOUserClientCreator)" = "pid (\d+), ([^"]*)"`)
)

// ParseIORegOwners reads the output of `ioreg -r -c IOUSBHostDevice -l -w0`
// and returns the processes that have the given device, or its interfaces,
// open, with a one line summary of each place one was found.
func ParseIORegOwners(out string, vendorID, productID uint16) ([]Owner, string) {
	for _, block := range ioregDevices(out) {
		if matchesID(ioregVendor, block, vendorID) && matchesID(ioregProduct, block, productID) {
			return blockOwners(block)
		}
	}

	return nil, ""
}

// ioregDevices splits ioreg output into one string per USB device.
func ioregDevices(out string) []string {
	var blocks []string

	for line := range strings.Lines(out) {
		if ioregDeviceStart.MatchString(line) || len(blocks) == 0 {
			blocks = append(blocks, "")
		}

		blocks[len(blocks)-1] += line
	}

	return blocks
}

func blockOwners(block string) ([]Owner, string) {
	var (
		owners  []Owner
		summary []string
		object  string
		class   string
	)

	for line := range strings.Lines(block) {
		if m := ioregObject.FindStringSubmatch(line); m != nil {
			object, class = m[1], m[2]

			continue
		}

		m := ioregOwner.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		pid, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}

		// A connection's object carries the full process name, which the
		// property cuts short.
		name := m[3]
		if strings.HasSuffix(class, "UserClient") && strings.HasPrefix(object, name) {
			name = object
		}

		exclusive := m[1] == "UsbExclusiveOwner"
		owners = addOwner(owners, Owner{PID: pid, Name: name, Exclusive: exclusive})

		entry := fmt.Sprintf("%s %s: %s", class, object, Owner{PID: pid, Name: name, Exclusive: exclusive})
		if len(summary) == 0 || summary[len(summary)-1] != entry {
			summary = append(summary, entry)
		}
	}

	return owners, strings.Join(summary, "\n")
}

// addOwner adds o, or marks the one already there exclusive if o is.
func addOwner(owners []Owner, o Owner) []Owner {
	for i, have := range owners {
		if have.PID == o.PID {
			owners[i].Exclusive = have.Exclusive || o.Exclusive
			if len(o.Name) > len(have.Name) {
				owners[i].Name = o.Name
			}

			return owners
		}
	}

	return append(owners, o)
}

func matchesID(re *regexp.Regexp, text string, want uint16) bool {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return false
	}

	v, err := strconv.ParseUint(m[1], 10, 16)

	return err == nil && uint16(v) == want
}
