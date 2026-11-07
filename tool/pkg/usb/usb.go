// Package usb provides the small amount of USB plumbing openshuttercount
// needs in order to speak PTP to a camera: enumerate still image devices,
// claim the interface and move bulk transfers in both directions.
//
// It runs on macOS, through libusb-1.0 loaded at run time with purego, so
// there's no cgo (usb_darwin.go). Elsewhere it builds but finds no camera
// (usb_other.go), so the rest of the code can be tested anywhere.
package usb

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	// ClassStillImage is the USB interface class for PTP devices (PIMA
	// 15740). Cameras expose their PTP endpoints on an interface with this
	// class, which is how we tell them apart from the mass storage or
	// audio interfaces a body may also advertise.
	ClassStillImage uint8 = 0x06
)

var (
	// ErrNoDevice is returned when no matching camera is connected.
	ErrNoDevice = errors.New("no still image device found")

	// ErrBusy is returned when the interface is claimed by something else:
	// macOS's ptpcamerad, or an app using the camera through it.
	ErrBusy = errors.New("device is claimed by another process")

	// ErrNotSupported is returned by backends that are not implemented on
	// the current platform.
	ErrNotSupported = errors.New("not supported on this platform")

	// ErrTimeout is returned when a transfer moved nothing before its
	// timeout. Backends that can tell a timeout apart wrap it.
	ErrTimeout = errors.New("transfer timed out")

	// ErrNoStillImage is returned when a device offers no still image (PTP)
	// interface, which is how a camera in a webcam or other USB mode looks.
	ErrNoStillImage = errors.New("the device offers no still image (PTP) interface")
)

// Resetter is implemented by backends that can send the still image class
// Device Reset request, which returns the camera to its idle state: no
// session open and nothing pending. It changes no settings or files.
type Resetter interface {
	ResetPTP() error
}

// DeviceInfo identifies a device before it is opened.
type DeviceInfo struct {
	// Path names the device for the CLI's --device: an opaque bus/address
	// pair.
	Path string `json:"path"`

	Bus     uint16 `json:"bus"`
	Address uint16 `json:"address"`

	VendorID  uint16 `json:"vendor_id"`
	ProductID uint16 `json:"product_id"`

	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
	Serial       string `json:"serial,omitempty"`

	// Interfaces describes what the device offers, filled in by backends
	// that read the configuration descriptor when opening it.
	Interfaces string `json:"interfaces,omitempty"`
}

// String renders a device for the CLI's device list.
func (d DeviceInfo) String() string {
	name := d.Product
	if name == "" {
		name = "unknown device"
	}

	if d.Manufacturer != "" {
		name = d.Manufacturer + " " + name
	}

	return fmt.Sprintf("%04x:%04x  %s  (%s)", d.VendorID, d.ProductID, name, d.Path)
}

// Device is a claimed still image interface, ready for PTP containers.
//
// Read and Write map onto the interface's bulk IN and bulk OUT endpoints.
// They are not safe for concurrent use: PTP is a strictly alternating
// request/response protocol and the caller drives it one transaction at a
// time.
type Device interface {
	// Info returns the identity of the opened device.
	Info() DeviceInfo

	// Read performs a single bulk IN transfer. A short read is normal and
	// is how the device terminates a data phase, so callers must pass a
	// buffer large enough for the biggest container they expect rather
	// than sizing it to the bytes still outstanding.
	Read(p []byte, timeout time.Duration) (int, error)

	// Write performs a single bulk OUT transfer.
	Write(p []byte, timeout time.Duration) (int, error)

	// ClearHalt clears a stall condition on both bulk endpoints. A crashed
	// run can leave an endpoint halted, and every subsequent transfer will
	// fail until it is cleared.
	ClearHalt() error

	// Close releases the interface and the underlying handle.
	Close() error
}

// List enumerates the still image devices currently attached.
func List() ([]DeviceInfo, error) {
	return list()
}

// ListVendors enumerates still image devices belonging to any of the given
// vendors. With no vendors given it returns nothing, on the grounds that a
// caller which knows no manufacturers has nothing to talk to.
func ListVendors(vendorIDs ...uint16) ([]DeviceInfo, error) {
	if len(vendorIDs) == 0 {
		return nil, nil
	}

	all, err := list()
	if err != nil {
		return nil, err
	}

	wanted := make(map[uint16]struct{}, len(vendorIDs))
	for _, id := range vendorIDs {
		wanted[id] = struct{}{}
	}

	out := make([]DeviceInfo, 0, len(all))

	for _, d := range all {
		if _, ok := wanted[d.VendorID]; ok {
			out = append(out, d)
		}
	}

	return out, nil
}

// Open claims the still image interface of the given device.
func Open(info DeviceInfo) (Device, error) {
	return open(info)
}

// ReleaseSystemCameraDaemon stops the operating system service that claims
// cameras as soon as they are plugged in, so the interface can be taken.
//
// ptpcamerad grabs every camera, and launchd restarts it on demand once we
// are done.
func ReleaseSystemCameraDaemon(ctx context.Context) error {
	return releaseSystemCameraDaemon(ctx)
}
