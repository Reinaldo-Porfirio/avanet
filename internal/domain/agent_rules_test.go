package domain

import (
	"testing"
	"time"
)

func TestProfiles(t *testing.T) {
	seen := map[ProfileID]bool{}
	for _, p := range Profiles() {
		if seen[p.ID] {
			t.Errorf("duplicate profile %q", p.ID)
		}
		seen[p.ID] = true
		if p.Name == "" || p.Description == "" {
			t.Errorf("%q: users need a name and a description", p.ID)
		}
		if p.LightInterval < time.Minute || p.SpeedInterval < p.LightInterval {
			t.Errorf("%q: implausible intervals %v / %v", p.ID, p.LightInterval, p.SpeedInterval)
		}
	}
	if _, ok := ProfileByID(DefaultProfile); !ok {
		t.Error("default profile does not exist")
	}
	if _, ok := ProfileByID("inexistente"); ok {
		t.Error("unknown profile found")
	}
}

func TestQuickQuality(t *testing.T) {
	stats := func(sent int, rtts ...time.Duration) LatencyStats {
		s, err := SummarizeLatency(rtts, sent)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	ms := func(v int) time.Duration { return time.Duration(v) * time.Millisecond }

	tests := []struct {
		name  string
		stats LatencyStats
		want  Quality
	}{
		{"no data", LatencyStats{}, QualityUnknown},
		{"fast and stable", stats(4, ms(12), ms(13), ms(12), ms(14)), QualityGood},
		{"slowish", stats(3, ms(60), ms(65), ms(62)), QualityFair},
		{"very slow", stats(3, ms(150), ms(160), ms(155)), QualityPoor},
		{"jittery", stats(3, ms(10), ms(50), ms(10)), QualityFair},
		{"unreachable", stats(5), QualityPoor},
		{"some loss (2%)", LatencyStats{Sent: 100, Received: 98, Median: ms(12)}, QualityFair},
		{"heavy loss (10%)", LatencyStats{Sent: 10, Received: 9, Median: ms(12)}, QualityPoor},
	}
	for _, tt := range tests {
		if got := QuickQuality(tt.stats); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestNextSpeedTest(t *testing.T) {
	loc := time.FixedZone("BRT", -3*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 11, 5, h, m, 0, 0, loc) }
	eod := at(24, 0)
	const cost = 145 * MB

	tests := []struct {
		name string
		s    SpeedSchedule
		want time.Time
	}{
		{
			name: "first test of the day runs now",
			s:    SpeedSchedule{Now: at(8, 0), Interval: time.Hour, RemainingBytes: 2000 * MB, TestCost: cost, EndOfDay: eod},
			want: at(8, 0),
		},
		{
			name: "plenty of budget keeps the profile's interval",
			s:    SpeedSchedule{Now: at(20, 10), Last: at(20, 0), Interval: time.Hour, RemainingBytes: 1500 * MB, TestCost: cost, EndOfDay: eod},
			want: at(21, 0),
		},
		{
			name: "tight budget spreads the remaining tests",
			// 6 tests affordable (900/145) in 14h left: one every 2h20.
			s:    SpeedSchedule{Now: at(10, 0), Last: at(9, 0), Interval: time.Hour, RemainingBytes: 900 * MB, TestCost: cost, EndOfDay: eod},
			want: at(11, 20),
		},
		{
			name: "overdue test runs now",
			s:    SpeedSchedule{Now: at(15, 0), Last: at(9, 0), Interval: time.Hour, RemainingBytes: 2000 * MB, TestCost: cost, EndOfDay: eod},
			want: at(15, 0),
		},
		{
			name: "budget exhausted waits for midnight",
			s:    SpeedSchedule{Now: at(15, 0), Last: at(14, 0), Interval: time.Hour, RemainingBytes: cost - 1, TestCost: cost, EndOfDay: eod},
			want: eod,
		},
	}
	for _, tt := range tests {
		if got := NextSpeedTest(tt.s); !got.Equal(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got.Format("15:04"), tt.want.Format("15:04"))
		}
	}
}

func TestLocalDayBounds(t *testing.T) {
	loc := time.FixedZone("BRT", -3*3600)
	at := time.Date(2026, 11, 5, 21, 30, 0, 0, loc)
	if got := StartOfLocalDay(at); !got.Equal(time.Date(2026, 11, 5, 0, 0, 0, 0, loc)) {
		t.Errorf("StartOfLocalDay = %v", got)
	}
	if got := EndOfLocalDay(at); !got.Equal(time.Date(2026, 11, 6, 0, 0, 0, 0, loc)) {
		t.Errorf("EndOfLocalDay = %v", got)
	}
}

func TestDegradationDetector(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC)
	ms := func(v int) time.Duration { return time.Duration(v) * time.Millisecond }
	var d DegradationDetector
	minute := 0
	observe := func(latency time.Duration, reachable bool) bool {
		minute++
		return d.Observe(t0.Add(time.Duration(minute)*time.Minute), latency, reachable)
	}

	// Warm-up: even a spike is ignored while the baseline is untrusted.
	for _, v := range []int{12, 11, 200, 13, 12} {
		if observe(ms(v), true) {
			t.Fatalf("triggered during warm-up at %d ms", v)
		}
	}
	base := d.Baseline()

	if observe(ms(30), true) {
		t.Error("30 ms is not twice the baseline plus 30 ms: no trigger expected")
	}
	if !observe(ms(150), true) {
		t.Errorf("150 ms against a ~%v baseline must trigger", base)
	}
	if observe(ms(160), true) {
		t.Error("second spike within the cooldown must not trigger")
	}
	if d.Baseline() > base+ms(5) {
		t.Errorf("degraded rounds moved the baseline from %v to %v", base, d.Baseline())
	}
	if observe(0, false) {
		t.Error("unreachable rounds are outages, not degradation")
	}

	minute += 61 // past the cooldown
	if !observe(ms(150), true) {
		t.Error("spike after the cooldown must trigger again")
	}
}
