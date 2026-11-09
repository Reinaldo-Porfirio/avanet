package netinfo

import (
	"errors"
	"log/slog"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// TestCurrentOnThisMachine exercises the real Windows APIs. CI runners and
// developer machines are online; an offline machine must yield ErrNoConnection,
// never a crash or a half-filled result.
func TestCurrentOnThisMachine(t *testing.T) {
	network, err := Reader{Logger: slog.New(slog.DiscardHandler)}.Current()
	if errors.Is(err, ErrNoConnection) {
		t.Skip("machine is offline:", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if network.InterfaceName == "" {
		t.Error("InterfaceName is empty")
	}
	t.Logf("kind=%v gateway=%v link=%d Mbps cost=%v", network.Kind, network.Gateway, network.LinkSpeedMbps, network.Cost)

	counters, err := Reader{}.Counters(network.InterfaceIndex)
	if err != nil {
		t.Fatal(err)
	}
	if counters.RxBytes == 0 {
		t.Error("an interface that reaches the internet must have received bytes")
	}
}

func TestKindFromIfType(t *testing.T) {
	tests := []struct {
		ifType uint32
		want   domain.ConnectionKind
	}{
		{windows.IF_TYPE_ETHERNET_CSMACD, domain.ConnectionEthernet},
		{windows.IF_TYPE_IEEE80211, domain.ConnectionWiFi},
		{ifTypeWWANPP, domain.ConnectionMobile},
		{ifTypeWWANPP2, domain.ConnectionMobile},
		{windows.IF_TYPE_PPP, domain.ConnectionOther},
	}
	for _, tt := range tests {
		if got := kindFromIfType(tt.ifType); got != tt.want {
			t.Errorf("kindFromIfType(%d) = %v, want %v", tt.ifType, got, tt.want)
		}
	}
}

func TestCostFromFlags(t *testing.T) {
	tests := []struct {
		flags uint32
		want  domain.ConnectionCost
	}{
		{0, domain.CostUnknown},
		{0x1, domain.CostUnmetered},           // unrestricted
		{0x1 | 0x20000, domain.CostUnmetered}, // unrestricted but congested
		{costVariable, domain.CostMetered},    // metered (e.g. phone hotspot)
		{costFixed | costApproachingDataLimit, domain.CostMetered},
		{0x1 | costRoaming, domain.CostMetered},
	}
	for _, tt := range tests {
		if got := costFromFlags(tt.flags); got != tt.want {
			t.Errorf("costFromFlags(0x%X) = %v, want %v", tt.flags, got, tt.want)
		}
	}
}
