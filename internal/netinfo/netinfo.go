// Package netinfo reports which network interface the computer uses to reach
// the internet, its router (gateway) and how the operating system classifies
// its cost (metered or not).
//
// The implementation is operating-system specific (netinfo_windows.go); other
// platforms return ErrUnsupported so the rest of the code still builds and
// tests everywhere.
package netinfo

import (
	"errors"
	"log/slog"
)

var (
	// ErrNoConnection means no interface currently routes to the internet.
	ErrNoConnection = errors.New("no active internet connection")
	// ErrUnsupported means this platform has no implementation yet.
	ErrUnsupported = errors.New("network information is not supported on this platform")
)

// Reader reads the current network state. The zero value is not usable: set
// Logger (use slog.New(slog.DiscardHandler) to discard).
type Reader struct {
	// Logger receives non-fatal problems, such as the connection cost being
	// unavailable; those are reported as unknown values instead of errors.
	Logger *slog.Logger
}
