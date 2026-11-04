//go:build windows

// Package winnet holds Windows networking helpers shared by the Phase 0 spikes.
package winnet

import (
	"fmt"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Adapter is an active network interface with an IPv4 default gateway.
type Adapter struct {
	Name        string
	Description string
	Type        string
	Gateway     netip.Addr
	LinkMbps    uint64
	Metric      uint32
}

// ActiveAdapters lists interfaces that are up and have an IPv4 gateway,
// ordered as Windows returns them.
func ActiveAdapters() ([]Adapter, error) {
	const flags = windows.GAA_FLAG_INCLUDE_GATEWAYS | windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_SKIP_DNS_SERVER

	size := uint32(16 << 10)
	for range 3 {
		buf := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_INET, flags, 0, first, &size)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue // size now holds the required length
		}
		if err != nil {
			return nil, fmt.Errorf("GetAdaptersAddresses: %w", err)
		}
		return collect(first), nil
	}
	return nil, fmt.Errorf("GetAdaptersAddresses: buffer kept growing")
}

func collect(first *windows.IpAdapterAddresses) []Adapter {
	var out []Adapter
	for a := first; a != nil; a = a.Next {
		if a.OperStatus != windows.IfOperStatusUp || a.FirstGatewayAddress == nil {
			continue
		}
		gw, ok := netip.AddrFromSlice(a.FirstGatewayAddress.Address.IP())
		if !ok {
			continue
		}
		out = append(out, Adapter{
			Name:        windows.UTF16PtrToString(a.FriendlyName),
			Description: windows.UTF16PtrToString(a.Description),
			Type:        ifTypeName(a.IfType),
			Gateway:     gw.Unmap(),
			LinkMbps:    a.ReceiveLinkSpeed / 1_000_000,
			Metric:      a.Ipv4Metric,
		})
	}
	return out
}

func ifTypeName(t uint32) string {
	switch t {
	case windows.IF_TYPE_ETHERNET_CSMACD:
		return "ethernet"
	case windows.IF_TYPE_IEEE80211:
		return "wifi"
	case 243, 244: // IF_TYPE_WWANPP, IF_TYPE_WWANPP2: mobile broadband modem
		return "mobile"
	default:
		return fmt.Sprintf("other(%d)", t)
	}
}
