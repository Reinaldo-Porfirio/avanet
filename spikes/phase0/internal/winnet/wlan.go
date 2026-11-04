//go:build windows

package winnet

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	wlanapi                = windows.NewLazySystemDLL("wlanapi.dll")
	wlanOpenHandle         = wlanapi.NewProc("WlanOpenHandle")
	wlanCloseHandle        = wlanapi.NewProc("WlanCloseHandle")
	wlanEnumInterfaces     = wlanapi.NewProc("WlanEnumInterfaces")
	wlanQueryInterface     = wlanapi.NewProc("WlanQueryInterface")
	wlanFreeMemory         = wlanapi.NewProc("WlanFreeMemory")
	errNoConnectedWireless = errors.New("no connected Wi-Fi interface")
)

const (
	wlanClientVersion2              = 2
	wlanInterfaceStateConnected     = 1
	wlanIntfOpcodeCurrentConnection = 7
	wlanIntfOpcodeChannelNumber     = 8
	wlanIntfOpcodeRSSI              = 0x10000102
)

// Layouts below mirror wlanapi.h; Go's natural alignment matches the C one.
type wlanInterfaceInfo struct {
	InterfaceGUID windows.GUID
	Description   [256]uint16
	State         uint32
}

type wlanInterfaceInfoList struct {
	NumberOfItems uint32
	Index         uint32
	InterfaceInfo [1]wlanInterfaceInfo
}

type dot11SSID struct {
	Length uint32
	SSID   [32]byte
}

type wlanAssociationAttributes struct {
	SSID          dot11SSID
	BSSType       uint32
	BSSID         [6]byte
	PhyType       uint32
	PhyIndex      uint32
	SignalQuality uint32 // 0-100, linear from -100 dBm to -50 dBm
	RxRateKbps    uint32
	TxRateKbps    uint32
}

type wlanConnectionAttributes struct {
	State       uint32
	Mode        uint32
	ProfileName [256]uint16
	Association wlanAssociationAttributes
	// security attributes follow; not needed
}

// WiFiInfo is the state of the connected wireless link.
type WiFiInfo struct {
	SSID          string
	SignalPercent uint32
	RSSI          int32
	Channel       uint32
	PhyType       uint32
	RxMbps        uint32
	TxMbps        uint32
}

// CurrentWiFi reads the connected Wi-Fi link through the native WLAN API,
// which, unlike netsh, does not depend on the Windows display language.
func CurrentWiFi() (WiFiInfo, error) {
	var negotiated uint32
	var handle windows.Handle
	if rc, _, _ := wlanOpenHandle.Call(wlanClientVersion2, 0,
		uintptr(unsafe.Pointer(&negotiated)), uintptr(unsafe.Pointer(&handle))); rc != 0 {
		return WiFiInfo{}, fmt.Errorf("WlanOpenHandle: %w", windows.Errno(rc))
	}
	defer wlanCloseHandle.Call(uintptr(handle), 0)

	var list *wlanInterfaceInfoList
	if rc, _, _ := wlanEnumInterfaces.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&list))); rc != 0 {
		return WiFiInfo{}, fmt.Errorf("WlanEnumInterfaces: %w", windows.Errno(rc))
	}
	defer wlanFreeMemory.Call(uintptr(unsafe.Pointer(list)))

	interfaces := unsafe.Slice(&list.InterfaceInfo[0], list.NumberOfItems)
	for i := range interfaces {
		if interfaces[i].State != wlanInterfaceStateConnected {
			continue
		}
		return queryConnected(handle, &interfaces[i].InterfaceGUID)
	}
	return WiFiInfo{}, errNoConnectedWireless
}

func queryConnected(handle windows.Handle, guid *windows.GUID) (WiFiInfo, error) {
	var info WiFiInfo

	var conn *wlanConnectionAttributes
	if err := query(handle, guid, wlanIntfOpcodeCurrentConnection, unsafe.Pointer(&conn)); err != nil {
		return info, fmt.Errorf("current connection: %w", err)
	}
	a := conn.Association
	info.SSID = string(a.SSID.SSID[:min(a.SSID.Length, 32)])
	info.SignalPercent = a.SignalQuality
	info.PhyType = a.PhyType
	info.RxMbps = a.RxRateKbps / 1000
	info.TxMbps = a.TxRateKbps / 1000
	wlanFreeMemory.Call(uintptr(unsafe.Pointer(conn)))

	var rssi *int32
	if err := query(handle, guid, wlanIntfOpcodeRSSI, unsafe.Pointer(&rssi)); err == nil {
		info.RSSI = *rssi
		wlanFreeMemory.Call(uintptr(unsafe.Pointer(rssi)))
	}
	var channel *uint32
	if err := query(handle, guid, wlanIntfOpcodeChannelNumber, unsafe.Pointer(&channel)); err == nil {
		info.Channel = *channel
		wlanFreeMemory.Call(uintptr(unsafe.Pointer(channel)))
	}
	return info, nil
}

// query calls WlanQueryInterface; out must point to a pointer variable that
// receives memory allocated by the API (freed by the caller).
func query(handle windows.Handle, guid *windows.GUID, opcode uint32, out unsafe.Pointer) error {
	var size, valueType uint32
	rc, _, _ := wlanQueryInterface.Call(uintptr(handle), uintptr(unsafe.Pointer(guid)), uintptr(opcode), 0,
		uintptr(unsafe.Pointer(&size)), uintptr(out), uintptr(unsafe.Pointer(&valueType)))
	if rc != 0 {
		return windows.Errno(rc)
	}
	return nil
}
