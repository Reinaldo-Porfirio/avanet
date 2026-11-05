package domain

import "testing"

func TestBandFromFrequencyKHz(t *testing.T) {
	tests := []struct {
		khz  uint32
		want WiFiBand
	}{
		{2_412_000, Band2GHz}, // channel 1
		{2_484_000, Band2GHz}, // channel 14
		{5_220_000, Band5GHz}, // channel 44
		{5_825_000, Band5GHz}, // channel 165
		{5_955_000, Band6GHz}, // 6 GHz channel 1
		{7_115_000, Band6GHz}, // 6 GHz channel 233
		{0, BandUnknown},
		{60_480_000, BandUnknown}, // 60 GHz (802.11ad) is not a supported band
	}
	for _, tt := range tests {
		if got := BandFromFrequencyKHz(tt.khz); got != tt.want {
			t.Errorf("BandFromFrequencyKHz(%d) = %v, want %v", tt.khz, got, tt.want)
		}
	}
}

func TestIsMobileData(t *testing.T) {
	tests := []struct {
		network Network
		want    bool
	}{
		{Network{Kind: ConnectionEthernet, Cost: CostUnmetered}, false},
		{Network{Kind: ConnectionWiFi, Cost: CostUnknown}, false},
		{Network{Kind: ConnectionWiFi, Cost: CostMetered}, true}, // phone hotspot
		{Network{Kind: ConnectionMobile, Cost: CostUnknown}, true},
	}
	for _, tt := range tests {
		if got := tt.network.IsMobileData(); got != tt.want {
			t.Errorf("%v/%v: IsMobileData = %v, want %v", tt.network.Kind, tt.network.Cost, got, tt.want)
		}
	}
}
