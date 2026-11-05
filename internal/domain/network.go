package domain

import "net/netip"

// ConnectionKind is the physical type of the interface used to reach the internet.
type ConnectionKind int

// Connection kinds.
const (
	ConnectionOther ConnectionKind = iota
	ConnectionEthernet
	ConnectionWiFi
	ConnectionMobile // USB/4G modem; phone hotspots appear as Wi-Fi marked metered
)

func (k ConnectionKind) String() string {
	switch k {
	case ConnectionEthernet:
		return "ethernet"
	case ConnectionWiFi:
		return "wifi"
	case ConnectionMobile:
		return "mobile"
	case ConnectionOther:
		return "other"
	}
	return "unknown"
}

// ConnectionCost is how the operating system classifies the connection's price.
type ConnectionCost int

// Connection costs.
const (
	CostUnknown ConnectionCost = iota
	CostUnmetered
	CostMetered // the user pays per byte or has a data cap
)

func (c ConnectionCost) String() string {
	switch c {
	case CostUnmetered:
		return "unmetered"
	case CostMetered:
		return "metered"
	case CostUnknown:
		return "unknown"
	}
	return "unknown"
}

// Network describes the interface currently used to reach the internet.
type Network struct {
	InterfaceIndex uint32 // operating system's interface number
	Kind           ConnectionKind
	InterfaceName  string     // e.g. "Wi-Fi", as shown by Windows
	Description    string     // adapter model
	Gateway        netip.Addr // zero value when the interface has no gateway
	LinkSpeedMbps  uint64     // negotiated link speed, not internet speed
	Cost           ConnectionCost
}

// IsMobileData reports whether measurements on this network must respect the
// mobile data budget: a mobile modem, or any connection Windows marks metered
// (which it does automatically for phone hotspots).
func (n Network) IsMobileData() bool {
	return n.Kind == ConnectionMobile || n.Cost == CostMetered
}
