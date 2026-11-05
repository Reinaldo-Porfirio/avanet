package ptbr

import (
	"testing"
	"time"
)

func TestFormats(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{Millis(0), "0,0 ms"},
		{Millis(590 * time.Microsecond), "0,6 ms"},
		{Millis(12077 * time.Microsecond), "12,1 ms"},
		{Mbps(512.34), "512,3 Mbps"},
		{MB(97_000_000), "97,0 MB"},
		{Percent(33.33), "33%"},
		{Decimal(2.4, 1), "2,4"},
		{Hours(14*time.Hour + 5*time.Minute + 20*time.Second), "14h05"},
		{Hours(45 * time.Minute), "0h45"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}
