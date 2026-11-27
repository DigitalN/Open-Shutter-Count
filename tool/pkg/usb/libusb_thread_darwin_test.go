//go:build darwin

package usb

import (
	"io"
	"os"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bundledLibusb is the libusb the app ships, from this package's folder.
const bundledLibusb = "../../mac/libusb-1.0.dylib"

// TestLibusbCallsTheLogCallbackFromItsOwnThread guards the path a failing
// camera depends on: the bundled libusb calling api.logged, through purego,
// from a thread of its own. The fake libusb tests call it from Go, so they
// can't reach it.
//
// libusb's event thread logs at debug level as a context starts and stops,
// so this starts one at that level and stops it, with echo on so api.logged
// copies each line to stderr. Nothing else is called: no device is listed,
// opened, claimed or seized.
func TestLibusbCallsTheLogCallbackFromItsOwnThread(t *testing.T) {
	if _, err := os.Stat(bundledLibusb); err != nil {
		t.Skipf("no bundled libusb: %v", err)
	}

	lib, err := purego.Dlopen(bundledLibusb, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	require.NoError(t, err)

	var (
		initContext func(ctx *uintptr, options *libusbInitOption, numOptions int32) int32
		setLogCB    func(ctx uintptr, cb uintptr, mode int32)
		exit        func(ctx uintptr)
	)

	purego.RegisterLibFunc(&initContext, lib, "libusb_init_context")
	purego.RegisterLibFunc(&setLogCB, lib, "libusb_set_log_cb")
	purego.RegisterLibFunc(&exit, lib, "libusb_exit")

	read, write, err := os.Pipe()
	require.NoError(t, err)

	stderr := os.Stderr
	os.Stderr = write

	t.Cleanup(func() {
		setLogCB(0, 0, libusbLogCBGlobal)

		os.Stderr = stderr
	})

	// Read as it's written, so a full pipe can't stall libusb's thread.
	echoed := make(chan string)

	go func() {
		all, _ := io.ReadAll(read)
		echoed <- string(all)
	}()

	api := &libusbAPI{said: &libusbLog{}, echo: true}
	setLogCB(0, purego.NewCallback(api.logged), libusbLogCBGlobal)

	const libusbLogLevelDebug = 4

	options := [...]libusbInitOption{{option: libusbOptionLogLevel, value: libusbLogLevelDebug}}

	var ctx uintptr

	require.Equal(t, libusbSuccess, initContext(&ctx, &options[0], int32(len(options))))
	exit(ctx)

	setLogCB(0, 0, libusbLogCBGlobal)
	require.NoError(t, write.Close())

	assert.Contains(t, <-echoed, "[darwin_event_thread_main]", "libusb's event thread reached api.logged")
	assert.Empty(t, api.said.stop(), "debug lines are dropped, and no transfer was recording")
}
