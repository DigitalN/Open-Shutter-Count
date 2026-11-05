//go:build !darwin

package usb

import "context"

// Open Shutter Count reads cameras on macOS only. This lets everything else
// build and run its tests on other machines, where no camera is ever found.

func list() ([]DeviceInfo, error) {
	return nil, ErrNotSupported
}

func open(_ DeviceInfo) (Device, error) {
	return nil, ErrNotSupported
}

func releaseSystemCameraDaemon(_ context.Context) error { return nil }

func deviceOwners(_ context.Context, _ DeviceInfo) ([]Owner, string, error) { return nil, "", nil }
