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
		{DateLong(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)), "terça-feira, 3 de novembro"},
		{DateLong(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)), "domingo, 1 de março"},
		{DateShort(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)), "01/03"},
		{Count(1, "teste", "testes"), "1 teste"},
		{Count(0, "teste", "testes"), "0 testes"},
		{Count(15, "teste", "testes"), "15 testes"},
		{Interval(time.Minute), "1 minuto"},
		{Interval(5 * time.Minute), "5 minutos"},
		{Interval(time.Hour), "1 hora"},
		{Interval(3 * time.Hour), "3 horas"},
		{Interval(90 * time.Minute), "1h30"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}
