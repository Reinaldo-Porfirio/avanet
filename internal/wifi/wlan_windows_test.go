package wifi

import (
	"errors"
	"testing"
	"unsafe"
)

// TestStructLayouts pins the sizes of the C structs we mirror. A mismatch
// would make every field after the first difference garbage.
func TestStructLayouts(t *testing.T) {
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"DOT11_SSID", unsafe.Sizeof(dot11SSID{}), 36},
		{"WLAN_INTERFACE_INFO", unsafe.Sizeof(interfaceInfo{}), 532},
		{"WLAN_ASSOCIATION_ATTRIBUTES", unsafe.Sizeof(associationAttributes{}), 68},
		{"WLAN_BSS_ENTRY", unsafe.Sizeof(bssEntry{}), 360},
		{"WLAN_BSS_ENTRY.ulChCenterFrequency offset", unsafe.Offsetof(bssEntry{}.ChCenterFrequency), 92},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d bytes, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestSameSSID(t *testing.T) {
	ssid := func(name string, junk byte) dot11SSID {
		s := dot11SSID{Length: uint32(len(name))}
		copy(s.SSID[:], name)
		for i := len(name); i < maxSSIDLength; i++ {
			s.SSID[i] = junk
		}
		return s
	}
	if !sameSSID(ssid("casa", 0), ssid("casa", 0xFF)) {
		t.Error("bytes beyond Length must be ignored")
	}
	if sameSSID(ssid("casa", 0), ssid("casa5g", 0)) {
		t.Error("different names reported equal")
	}
	if sameSSID(dot11SSID{Length: 99}, dot11SSID{Length: 99}) {
		t.Error("invalid length must not compare equal")
	}
}

// TestCurrentOnThisMachine calls the real API. CI runners have no Wi-Fi, so
// "no adapter" and "not connected" are expected outcomes there.
func TestCurrentOnThisMachine(t *testing.T) {
	info, err := Current()
	switch {
	case errors.Is(err, ErrNoAdapter), errors.Is(err, ErrNotConnected), errors.Is(err, ErrAccessDenied):
		t.Skip("no usable Wi-Fi here:", err)
	case err != nil:
		t.Fatal(err)
	}
	if info.SignalPercent < 0 || info.SignalPercent > 100 {
		t.Errorf("SignalPercent = %d, want 0-100", info.SignalPercent)
	}
	t.Logf("signal=%d%% rssi=%d dBm channel=%d band=%v standard=%q rx=%d Mbps",
		info.SignalPercent, info.RSSIdBm, info.Channel, info.Band, info.Standard, info.RxRateMbps)
}
