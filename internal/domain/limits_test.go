package domain

import (
	"testing"
	"time"
)

func TestLimitsGrade(t *testing.T) {
	const ms = time.Millisecond
	latency := func(l, j time.Duration, loss float64) Measured {
		return Measured{HasLatency: true, Latency: l, Jitter: j, LossPercent: loss}
	}
	withDown := func(m Measured, v float64) Measured { m.HasDownload, m.DownloadMbps = true, v; return m }
	withUp := func(m Measured, v float64) Measured { m.HasUpload, m.UploadMbps = true, v; return m }
	withOutage := func(m Measured, d time.Duration) Measured { m.Downtime = d; return m }
	healthy := latency(12*ms, 3*ms, 0)

	tests := []struct {
		name    string
		profile ProfileID
		m       Measured
		want    Quality
	}{
		{"nothing measured", ProfileGaming, Measured{}, QualityUnknown},

		{"gaming: fast and stable", ProfileGaming, healthy, QualityGood},
		{"gaming: 70 ms is playable, not good", ProfileGaming, latency(70*ms, 3*ms, 0), QualityFair},
		{"gaming: jitter ruins it", ProfileGaming, latency(20*ms, 25*ms, 0), QualityFair},
		{"gaming: 120 ms is poor", ProfileGaming, latency(120*ms, 3*ms, 0), QualityPoor},
		{"gaming: loss above 2%", ProfileGaming, latency(20*ms, 3*ms, 2.5), QualityPoor},

		{"home office: enough upload", ProfileHomeOffice, withUp(healthy, 40), QualityGood},
		{"home office: 2 Mbps up is tight", ProfileHomeOffice, withUp(healthy, 2), QualityFair},
		{"home office: 1 Mbps up is poor", ProfileHomeOffice, withUp(healthy, 1), QualityPoor},

		{"streaming: 4K capable", ProfileStreaming, withDown(healthy, 100), QualityGood},
		{"streaming: HD only", ProfileStreaming, withDown(healthy, 10), QualityFair},
		{"streaming: below HD", ProfileStreaming, withDown(healthy, 3), QualityPoor},
		{"streaming: 200 ms does not matter", ProfileStreaming, latency(200*ms, 40*ms, 0), QualityGood},

		{"basic: short drop caps at fair", ProfileBasic, withOutage(healthy, time.Minute), QualityFair},
		{"any profile: long outage is poor", ProfileBasic, withOutage(healthy, 6*time.Minute), QualityPoor},

		{"complete: every metric counts", ProfileComplete, withUp(withDown(healthy, 300), 2), QualityFair},
		{"complete: all healthy", ProfileComplete, withUp(withDown(healthy, 300), 40), QualityGood},
	}
	for _, tt := range tests {
		p, ok := ProfileByID(tt.profile)
		if !ok {
			t.Fatalf("%s: unknown profile", tt.name)
		}
		if got := p.Limits.Grade(tt.m); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestEveryProfileHasLimits(t *testing.T) {
	for _, p := range Profiles() {
		if p.Limits == (Limits{}) {
			t.Errorf("%q has no limits", p.ID)
		}
	}
}
