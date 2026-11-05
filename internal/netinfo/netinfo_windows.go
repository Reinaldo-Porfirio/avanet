package netinfo

import (
	"errors"
	"fmt"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// probeAddress is only used to ask Windows which interface it would route
// through; no packet is sent to it.
var probeAddress = [4]byte{1, 1, 1, 1}

// Interface types from ipifcons.h that x/sys does not name.
const (
	ifTypeWWANPP  = 243 // mobile broadband (GSM)
	ifTypeWWANPP2 = 244 // mobile broadband (CDMA)
)

// Current returns the interface Windows uses to reach the internet.
func (r Reader) Current() (domain.Network, error) {
	index, err := bestInterfaceIndex()
	if err != nil {
		return domain.Network{}, err
	}

	network, err := describeInterface(index)
	if err != nil {
		return domain.Network{}, err
	}

	network.Cost, err = connectionCost()
	if err != nil {
		r.Logger.Warn("connection cost unavailable", "error", err)
		network.Cost = domain.CostUnknown
	}
	return network, nil
}

func bestInterfaceIndex() (uint32, error) {
	var index uint32
	if err := windows.GetBestInterfaceEx(&windows.SockaddrInet4{Addr: probeAddress}, &index); err != nil {
		// ERROR_NETWORK_UNREACHABLE and similar: there is no route at all.
		return 0, fmt.Errorf("%w: %w", ErrNoConnection, err)
	}
	return index, nil
}

// Counters returns the cumulative bytes moved by an interface, used to detect
// other traffic during a speed test.
func (Reader) Counters(index uint32) (domain.InterfaceCounters, error) {
	row := windows.MibIfRow2{InterfaceIndex: index}
	if err := windows.GetIfEntry2Ex(windows.MibIfEntryNormal, &row); err != nil {
		return domain.InterfaceCounters{}, fmt.Errorf("GetIfEntry2Ex(%d): %w", index, err)
	}
	return domain.InterfaceCounters{RxBytes: row.InOctets, TxBytes: row.OutOctets}, nil
}

func describeInterface(index uint32) (domain.Network, error) {
	adapters, err := adapterAddresses()
	if err != nil {
		return domain.Network{}, err
	}

	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&adapters[0])); a != nil; a = a.Next {
		if a.IfIndex != index {
			continue
		}
		network := domain.Network{
			InterfaceIndex: index,
			Kind:           kindFromIfType(a.IfType),
			InterfaceName:  windows.UTF16PtrToString(a.FriendlyName),
			Description:    windows.UTF16PtrToString(a.Description),
			LinkSpeedMbps:  a.ReceiveLinkSpeed / 1_000_000,
		}
		if a.FirstGatewayAddress != nil {
			if gw, ok := netip.AddrFromSlice(a.FirstGatewayAddress.Address.IP()); ok {
				network.Gateway = gw.Unmap()
			}
		}
		return network, nil
	}
	return domain.Network{}, fmt.Errorf("%w: interface %d not found among adapters", ErrNoConnection, index)
}

// adapterAddresses returns the raw IPv4 adapter list. The buffer must stay
// alive while the linked list inside it is read.
func adapterAddresses() ([]byte, error) {
	const flags = windows.GAA_FLAG_INCLUDE_GATEWAYS | windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_SKIP_DNS_SERVER
	const attempts = 3 // adapters can appear between the size query and the call

	size := uint32(16 << 10)
	for range attempts {
		buf := make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_INET, flags, 0, (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		switch {
		case err == nil:
			return buf, nil
		case errors.Is(err, windows.ERROR_BUFFER_OVERFLOW):
			continue // size now holds the required length
		case errors.Is(err, windows.ERROR_NO_DATA):
			return nil, fmt.Errorf("%w: no network adapters", ErrNoConnection)
		default:
			return nil, fmt.Errorf("GetAdaptersAddresses: %w", err)
		}
	}
	return nil, errors.New("GetAdaptersAddresses: adapter list kept growing")
}

func kindFromIfType(ifType uint32) domain.ConnectionKind {
	switch ifType {
	case windows.IF_TYPE_ETHERNET_CSMACD:
		return domain.ConnectionEthernet
	case windows.IF_TYPE_IEEE80211:
		return domain.ConnectionWiFi
	case ifTypeWWANPP, ifTypeWWANPP2:
		return domain.ConnectionMobile
	default:
		return domain.ConnectionOther
	}
}
