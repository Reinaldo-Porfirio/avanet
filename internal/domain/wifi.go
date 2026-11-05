package domain

// WiFiBand is the radio band of a Wi-Fi connection.
type WiFiBand int

// Wi-Fi bands.
const (
	BandUnknown WiFiBand = iota
	Band2GHz             // 2.4 GHz: longer range, slower, crowded
	Band5GHz
	Band6GHz // Wi-Fi 6E/7
)

func (b WiFiBand) String() string {
	switch b {
	case Band2GHz:
		return "2.4 GHz"
	case Band5GHz:
		return "5 GHz"
	case Band6GHz:
		return "6 GHz"
	case BandUnknown:
		return "unknown"
	}
	return "unknown"
}

// BandFromFrequencyKHz maps a channel center frequency to its band.
func BandFromFrequencyKHz(khz uint32) WiFiBand {
	switch {
	case khz >= 2_400_000 && khz < 2_500_000:
		return Band2GHz
	case khz >= 5_150_000 && khz < 5_925_000:
		return Band5GHz
	case khz >= 5_925_000 && khz < 7_125_000:
		return Band6GHz
	default:
		return BandUnknown
	}
}

// WiFi describes the current wireless link between the computer and the router.
type WiFi struct {
	// SSID may be empty: hidden networks, or Windows withholding it from apps
	// without location permission (Windows 11 24H2+).
	SSID          string
	SignalPercent int // 0-100, as shown by Windows
	RSSIdBm       int // received signal strength; closer to 0 is stronger
	Channel       int
	Band          WiFiBand
	Standard      string // e.g. "Wi-Fi 6 (802.11ax)"; empty when unknown
	RxRateMbps    int    // link rate negotiated with the router
	TxRateMbps    int
}
