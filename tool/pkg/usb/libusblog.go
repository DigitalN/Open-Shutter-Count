package usb

import (
	"regexp"
	"strings"
	"sync"
)

// libusb only says why a transfer failed in its log: its error codes are too
// coarse to tell, and LIBUSB_ERROR_IO covers anything IOKit can answer (issue
// 39). So the macOS backend turns libusb's log on at warning level and keeps
// what libusb says during each transfer, to add to that transfer's error.
//
// Nothing else libusb says is kept. It warns on every failed open or claim,
// and the open is retried every few milliseconds while ptpcamerad has the
// camera, which would flood the app's log, and the issue link that carries it.
//
// The log is here, not in usb_darwin.go, so it can be tested anywhere.

// libusb's log levels (enum libusb_log_level in libusb.h). Only errors and
// warnings are kept: with LIBUSB_DEBUG set libusb ignores the level it's given
// and sends everything.
const (
	libusbLogLevelError   = 1
	libusbLogLevelWarning = 2
)

// maxSaid is how many messages are kept for one transfer. libusb says one or
// two when a transfer fails; the cap keeps a flood out of the error.
const maxSaid = 4

// libusbLog keeps what libusb said since the current transfer started. libusb
// can log from a thread of its own, so it's safe for concurrent use. A nil
// libusbLog keeps nothing.
type libusbLog struct {
	mu        sync.Mutex
	recording bool
	said      []string
}

// start forgets anything said before, and keeps what's said from now on.
func (l *libusbLog) start() {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.recording, l.said = true, nil
}

// stop returns what was said since start, and keeps nothing more.
func (l *libusbLog) stop() []string {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	said := l.said
	l.recording, l.said = false, nil

	return said
}

// add is libusb's log callback: a message at a level, as libusb formats it.
func (l *libusbLog) add(level int32, message string) {
	if l == nil || level < libusbLogLevelError || level > libusbLogLevelWarning {
		return
	}

	message = tidyLibusbMessage(message)
	if message == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.recording && len(l.said) < maxSaid {
		l.said = append(l.said, message)
	}
}

// libusbPrefix is what libusb puts before each message: "libusb: warning
// [darwin_transfer_status] ", and at debug level a timestamp and thread before
// that.
var libusbPrefix = regexp.MustCompile(`^(?:\[[^\]]*\]\s*)*libusb: [a-z]+ \[[^\]]*\]\s*`)

// tidyLibusbMessage takes libusb's prefix and line ending off a message.
func tidyLibusbMessage(message string) string {
	return strings.TrimSpace(libusbPrefix.ReplaceAllString(strings.TrimSpace(message), ""))
}

// explained is a failed transfer's error, with what libusb said about it.
type explained struct {
	err    error
	reason string
}

func (e *explained) Error() string { return e.err.Error() + "; " + e.reason }
func (e *explained) Unwrap() error { return e.err }

// explain adds what libusb said during a failed transfer to its error, or
// that it said nothing: an aborted transfer is the one IOKit answer libusb
// doesn't log.
func explain(err error, said []string) error {
	if err == nil {
		return nil
	}

	reason := "libusb gave no reason"
	if len(said) > 0 {
		reason = "libusb said: " + strings.Join(said, "; ")
	}

	return &explained{err: err, reason: reason}
}

// codeError is a libusb error code, named, that matches a sentinel such as
// ErrIO without saying so: the code's name already does.
type codeError struct {
	text     string
	sentinel error
}

func (e *codeError) Error() string { return e.text }
func (e *codeError) Unwrap() error { return e.sentinel }
