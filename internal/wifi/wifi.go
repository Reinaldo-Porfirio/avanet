// Package wifi reads the state of the current Wi-Fi link (signal, channel,
// band, standard) from the operating system's native wireless API.
//
// On Windows it uses wlanapi.dll instead of parsing `netsh` output, which
// changes with the display language. Other platforms return ErrUnsupported.
package wifi

import "errors"

var (
	// ErrNotConnected means there is a Wi-Fi adapter but it is not connected.
	ErrNotConnected = errors.New("wi-fi is not connected")
	// ErrNoAdapter means there is no Wi-Fi adapter or the WLAN service is off.
	ErrNoAdapter = errors.New("no wi-fi adapter available")
	// ErrAccessDenied means Windows refused access to Wi-Fi details. From
	// Windows 11 24H2 this happens when desktop apps are not allowed to use
	// the location (Settings > Privacy & security > Location).
	ErrAccessDenied = errors.New("access to wi-fi details denied")
	// ErrUnsupported means this platform has no implementation yet.
	ErrUnsupported = errors.New("wi-fi information is not supported on this platform")
)
