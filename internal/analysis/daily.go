// Package analysis turns one day's raw measurements into a daily report: pure
// computation with no I/O, so every rule is covered by tests.
package analysis

import (
	"errors"
	"fmt"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// Version identifies the format of Daily. Bump it when the fields or their
// meaning change: stored reports of older versions are rebuilt.
const Version = 1

// Number of hours and of parts of the day in a report.
const (
	hoursInDay = 24
	periods    = 4
)

// ErrNoData means nothing was measured in the day, so there is no report.
var ErrNoData = errors.New("no measurements in this day")

// Input is what a report is built from.
type Input struct {
	Day     domain.Day
	Start   time.Time // local midnight that starts the day
	End     time.Time // local midnight that ends it
	Now     time.Time // when the report is built; before End for a day in progress
	Profile domain.Profile
	Data    *domain.DayData
}

// Daily is the report of one day. Durations are in milliseconds and rates in
// Mbps, so the stored JSON can be used directly by the panel.
type Daily struct {
	Version     int    `json:"version"`
	Day         string `json:"day"`
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	InProgress  bool   `json:"in_progress"` // the day is not over yet
	Network     string `json:"network,omitempty"`

	Grade    domain.Quality `json:"grade"`
	Headline string         `json:"headline"` // the day in one sentence
	Lede     string         `json:"lede"`     // what happened, in plain words

	Coverage Coverage         `json:"coverage"`
	Hours    [hoursInDay]Hour `json:"hours"`
	Periods  [periods]Period  `json:"periods"`
	Best     []HourRange      `json:"best"`  // longest good stretch
	Worst    []HourRange      `json:"worst"` // worst stretches, longest first

	Download    Stat    `json:"download_mbps"`
	Upload      Stat    `json:"upload_mbps"`
	Latency     Stat    `json:"latency_ms"`
	Jitter      Stat    `json:"jitter_ms"`
	LossPercent float64 `json:"loss_percent"`

	Outages    []OutageSpan `json:"outages"`
	DowntimeMs float64      `json:"downtime_ms"`

	Speed []SpeedPoint `json:"speed"`
	// IgnoredSpeedTests ran while other programs used the network; they are
	// shown but left out of the statistics and grades.
	IgnoredSpeedTests int `json:"ignored_speed_tests"`

	Diagnosis     Diagnosis `json:"diagnosis"`
	DataUsedBytes int64     `json:"data_used_bytes"`
}

// Coverage says how much of the day was actually observed.
type Coverage struct {
	MonitoredMs float64 `json:"monitored_ms"`
	WindowMs    float64 `json:"window_ms"` // 24 h, or the elapsed part of today
	Rounds      int     `json:"rounds"`
	SpeedTests  int     `json:"speed_tests"`
}

// Hour summarizes one hour of the day. Rounds == 0 means it was not monitored.
type Hour struct {
	Grade        domain.Quality `json:"grade"`
	Rounds       int            `json:"rounds"`
	LatencyMs    float64        `json:"latency_ms"`
	JitterMs     float64        `json:"jitter_ms"`
	LossPercent  float64        `json:"loss_percent"`
	GatewayMs    float64        `json:"gateway_ms"`
	DownloadMbps float64        `json:"download_mbps"` // 0 when no speed test ran
	UploadMbps   float64        `json:"upload_mbps"`
	DowntimeMs   float64        `json:"downtime_ms"`
}

// Period is a part of the day, described like a weather forecast.
type Period struct {
	Name         string         `json:"name"`
	FromHour     int            `json:"from_hour"`
	ToHour       int            `json:"to_hour"`
	Grade        domain.Quality `json:"grade"`
	Condition    string         `json:"condition"`
	LatencyMs    float64        `json:"latency_ms"`
	DownloadMbps float64        `json:"download_mbps"`
	UploadMbps   float64        `json:"upload_mbps"`
	Outages      int            `json:"outages"`
}

// HourRange is a stretch of consecutive hours; To is exclusive.
type HourRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// Stat summarizes one metric over the day.
type Stat struct {
	Count  int       `json:"count"`
	Median float64   `json:"median"`
	P10    float64   `json:"p10"`
	P90    float64   `json:"p90"`
	Min    float64   `json:"min"`
	MinAt  time.Time `json:"min_at"`
	Max    float64   `json:"max"`
	MaxAt  time.Time `json:"max_at"`
}

// OutageSpan is an outage clamped to the day.
type OutageSpan struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// SpeedPoint is one speed test, for the chart.
type SpeedPoint struct {
	At           time.Time `json:"at"`
	DownloadMbps float64   `json:"download_mbps"`
	UploadMbps   float64   `json:"upload_mbps"`
	LocalTraffic bool      `json:"local_traffic"`
}

// Build computes the report of a day.
func Build(in *Input) (*Daily, error) {
	if !in.Day.Valid() || !in.End.After(in.Start) || in.Data == nil {
		return nil, fmt.Errorf("%w: report input for day %q", domain.ErrInvalidSample, in.Day)
	}
	rounds := groupRounds(in.Data.Latency)
	if len(rounds) == 0 && len(in.Data.Speed) == 0 {
		return nil, ErrNoData
	}

	windowEnd := in.End
	if in.Now.Before(windowEnd) {
		windowEnd = in.Now
	}

	d := &Daily{
		Version: Version, Day: string(in.Day),
		ProfileID: string(in.Profile.ID), ProfileName: in.Profile.Name,
		InProgress:    in.Now.Before(in.End),
		Network:       networkLabel(in.Data),
		DataUsedBytes: in.Data.DataUsed,
		Coverage: Coverage{
			MonitoredMs: millis(covered(in.Data.Sessions, in.Start, windowEnd)),
			WindowMs:    millis(windowEnd.Sub(in.Start)),
			Rounds:      len(rounds),
			SpeedTests:  len(in.Data.Speed),
		},
	}

	loc := in.Start.Location()
	buckets := fillBuckets(rounds, in.Data, loc)
	d.Outages, d.DowntimeMs = applyOutages(&buckets, in.Data.Outages, in.Start, windowEnd)

	limits := in.Profile.Limits
	for h := range buckets {
		d.Hours[h] = buckets[h].summary(limits)
	}
	d.Periods = buildPeriods(&buckets, &d.Hours, d.Outages, loc)
	d.Grade = rollup(d.Hours[:])
	d.Best, d.Worst = hourRanges(&d.Hours)

	d.Latency, d.Jitter, d.LossPercent = latencyStats(rounds)
	d.Download, d.Upload, d.Speed, d.IgnoredSpeedTests = speedStats(in.Data.Speed)

	d.Diagnosis = diagnose(&buckets, &d.Hours, in.Data)
	d.Headline = headline(d)
	d.Lede = lede(d, &buckets, in.Profile.ID)
	return d, nil
}

func networkLabel(data *domain.DayData) string {
	if !data.HasNetwork {
		return ""
	}
	switch data.Network.Kind {
	case domain.ConnectionWiFi:
		if data.Network.SSID != "" {
			return "Wi-Fi “" + data.Network.SSID + "”"
		}
		return "Wi-Fi"
	case domain.ConnectionEthernet:
		return "Cabo"
	case domain.ConnectionMobile:
		return "Modem móvel"
	case domain.ConnectionOther:
		return "Outra conexão"
	}
	return ""
}

// covered sums the part of each session inside [from, to).
func covered(sessions []domain.TimeSpan, from, to time.Time) time.Duration {
	var total time.Duration
	for _, s := range sessions {
		start, end := s.Start, s.End
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		if end.After(start) {
			total += end.Sub(start)
		}
	}
	return total
}

func millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
