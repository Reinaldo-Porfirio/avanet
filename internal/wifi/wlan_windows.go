package wifi

import (
	"bytes"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

var (
	// NewLazySystemDLL loads only from System32, preventing DLL hijacking.
	wlanapi                   = windows.NewLazySystemDLL("wlanapi.dll")
	procWlanOpenHandle        = wlanapi.NewProc("WlanOpenHandle")
	procWlanCloseHandle       = wlanapi.NewProc("WlanCloseHandle")
	procWlanEnumInterfaces    = wlanapi.NewProc("WlanEnumInterfaces")
	procWlanQueryInterface    = wlanapi.NewProc("WlanQueryInterface")
	procWlanGetNetworkBssList = wlanapi.NewProc("WlanGetNetworkBssList")
	procWlanFreeMemory        = wlanapi.NewProc("WlanFreeMemory")
)

// Constants from wlanapi.h / windot11.h.
const (
	wlanClientVersion = 2 // Windows Vista and later

	wlanInterfaceStateConnected = 1

	opcodeCurrentConnection = 7
	opcodeChannelNumber     = 8
	opcodeRSSI              = 0x10000102

	bssTypeInfrastructure = 1
	maxSSIDLength         = 32

	errServiceNotActive = windows.Errno(1062) // WLAN AutoConfig service stopped or absent
)

// The structs below mirror the C layouts in wlanapi.h. Go aligns fields the
// same way the C compiler does, so the offsets match; bssEntry is additionally
// sanity-checked at runtime because its layout is the most intricate.

type interfaceInfo struct {
	InterfaceGUID windows.GUID
	Description   [256]uint16
	State         uint32
}

type interfaceInfoList struct {
	NumberOfItems uint32
	Index         uint32
	Items         [1]interfaceInfo // variable length
}

type dot11SSID struct {
	Length uint32
	SSID   [maxSSIDLength]byte
}

type associationAttributes struct {
	SSID          dot11SSID
	BSSType       uint32
	BSSID         [6]byte
	PhyType       uint32
	PhyIndex      uint32
	SignalQuality uint32 // 0-100
	RxRateKbps    uint32
	TxRateKbps    uint32
}

type connectionAttributes struct {
	State       uint32
	Mode        uint32
	ProfileName [256]uint16
	Association associationAttributes
	// WLAN_SECURITY_ATTRIBUTES follows; not read.
}

type rateSet struct {
	Length uint32
	Rates  [126]uint16
}

type bssEntry struct {
	SSID              dot11SSID
	PhyID             uint32
	BSSID             [6]byte
	BSSType           uint32
	PhyType           uint32
	RSSI              int32
	LinkQuality       uint32
	InRegDomain       uint8
	BeaconPeriod      uint16
	Timestamp         uint64
	HostTimestamp     uint64
	CapabilityInfo    uint16
	ChCenterFrequency uint32 // kHz
	RateSet           rateSet
	IEOffset          uint32
	IESize            uint32
}

type bssList struct {
	TotalSize     uint32
	NumberOfItems uint32
	Items         [1]bssEntry // variable length
}

// Current returns the state of the connected Wi-Fi link.
func Current() (info domain.WiFi, err error) {
	var negotiated uint32
	var client windows.Handle
	if rc, _, _ := procWlanOpenHandle.Call(wlanClientVersion, 0,
		uintptr(unsafe.Pointer(&negotiated)), uintptr(unsafe.Pointer(&client))); rc != 0 {
		return domain.WiFi{}, translate("WlanOpenHandle", windows.Errno(rc))
	}
	defer func() {
		if rc, _, _ := procWlanCloseHandle.Call(uintptr(client), 0); rc != 0 {
			err = errors.Join(err, fmt.Errorf("WlanCloseHandle: %w", windows.Errno(rc)))
		}
	}()

	var list *interfaceInfoList
	if rc, _, _ := procWlanEnumInterfaces.Call(uintptr(client), 0, uintptr(unsafe.Pointer(&list))); rc != 0 {
		return domain.WiFi{}, translate("WlanEnumInterfaces", windows.Errno(rc))
	}
	defer freeWLAN(unsafe.Pointer(list))

	if list.NumberOfItems == 0 {
		return domain.WiFi{}, ErrNoAdapter
	}
	interfaces := unsafe.Slice(&list.Items[0], list.NumberOfItems)
	for i := range interfaces {
		if interfaces[i].State == wlanInterfaceStateConnected {
			return describe(client, &interfaces[i].InterfaceGUID)
		}
	}
	return domain.WiFi{}, ErrNotConnected
}

func describe(client windows.Handle, iface *windows.GUID) (domain.WiFi, error) {
	var conn *connectionAttributes
	if err := query(client, iface, opcodeCurrentConnection, unsafe.Pointer(&conn)); err != nil {
		return domain.WiFi{}, translate("query current connection", err)
	}
	assoc := conn.Association // copy before the API memory is freed
	freeWLAN(unsafe.Pointer(conn))

	info := domain.WiFi{
		SSID:          string(assoc.SSID.SSID[:min(assoc.SSID.Length, maxSSIDLength)]),
		SignalPercent: int(assoc.SignalQuality),
		Standard:      standardName(assoc.PhyType),
		RxRateMbps:    int(assoc.RxRateKbps / 1000),
		TxRateMbps:    int(assoc.TxRateKbps / 1000),
	}

	// The remaining details are best effort: a driver that does not report
	// one of them leaves that field at its zero value ("unknown").
	var rssi *int32
	if query(client, iface, opcodeRSSI, unsafe.Pointer(&rssi)) == nil {
		info.RSSIdBm = int(*rssi)
		freeWLAN(unsafe.Pointer(rssi))
	}
	var channel *uint32
	if query(client, iface, opcodeChannelNumber, unsafe.Pointer(&channel)) == nil {
		info.Channel = int(*channel)
		freeWLAN(unsafe.Pointer(channel))
	}
	info.Band = band(client, iface, &assoc)
	return info, nil
}

// band finds the connected access point in the scan list to read its center
// frequency, the only unambiguous way to tell 5 GHz from 6 GHz.
func band(client windows.Handle, iface *windows.GUID, assoc *associationAttributes) domain.WiFiBand {
	// No SSID filter: with one, the API also filters on "security enabled",
	// which we would have to read from the connection's security attributes.
	// The list is short; matching by BSSID (the access point's MAC) is exact.
	var list *bssList
	rc, _, _ := procWlanGetNetworkBssList.Call(uintptr(client), uintptr(unsafe.Pointer(iface)),
		0, bssTypeInfrastructure, 0, 0, uintptr(unsafe.Pointer(&list)))
	if rc != 0 || list == nil || list.NumberOfItems == 0 {
		return domain.BandUnknown
	}
	defer freeWLAN(unsafe.Pointer(list))

	entries := unsafe.Slice(&list.Items[0], list.NumberOfItems)
	for i := range entries {
		entry := &entries[i] // 360-byte entries: index instead of copying
		if entry.BSSID != assoc.BSSID {
			continue
		}
		// The matching entry must also carry our SSID; if it does not, the
		// struct layout is wrong and nothing read from it can be trusted.
		if !sameSSID(entry.SSID, assoc.SSID) {
			return domain.BandUnknown
		}
		return domain.BandFromFrequencyKHz(entry.ChCenterFrequency)
	}
	return domain.BandUnknown
}

// sameSSID compares only the meaningful bytes: the rest of the array may
// hold leftovers.
func sameSSID(a, b dot11SSID) bool {
	if a.Length != b.Length || a.Length > maxSSIDLength {
		return false
	}
	return bytes.Equal(a.SSID[:a.Length], b.SSID[:b.Length])
}

// query calls WlanQueryInterface. out must point to a pointer variable that
// receives memory allocated by the API; the caller frees it with freeWLAN.
func query(client windows.Handle, iface *windows.GUID, opcode uint32, out unsafe.Pointer) error {
	var size, valueType uint32
	rc, _, _ := procWlanQueryInterface.Call(uintptr(client), uintptr(unsafe.Pointer(iface)), uintptr(opcode), 0,
		uintptr(unsafe.Pointer(&size)), uintptr(out), uintptr(unsafe.Pointer(&valueType)))
	if rc != 0 {
		return windows.Errno(rc)
	}
	return nil
}

// freeWLAN releases memory allocated by the WLAN API. WlanFreeMemory returns
// void, so the call's results carry no information.
func freeWLAN(p unsafe.Pointer) { _, _, _ = procWlanFreeMemory.Call(uintptr(p)) }

func translate(op string, err error) error {
	switch {
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return fmt.Errorf("%w: %s: %w", ErrAccessDenied, op, err)
	case errors.Is(err, errServiceNotActive):
		return fmt.Errorf("%w: %s: %w", ErrNoAdapter, op, err)
	case errors.Is(err, windows.ERROR_INVALID_STATE):
		return fmt.Errorf("%w: %s: %w", ErrNotConnected, op, err)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}

// standardName maps DOT11_PHY_TYPE to the name shown to users.
func standardName(phyType uint32) string {
	switch phyType {
	case 4:
		return "802.11a"
	case 5:
		return "802.11b"
	case 6:
		return "802.11g"
	case 7:
		return "Wi-Fi 4 (802.11n)"
	case 8:
		return "Wi-Fi 5 (802.11ac)"
	case 10:
		return "Wi-Fi 6 (802.11ax)"
	case 11:
		return "Wi-Fi 7 (802.11be)"
	default:
		return ""
	}
}
