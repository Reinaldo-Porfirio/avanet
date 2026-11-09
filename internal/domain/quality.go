package domain

import (
	"fmt"
	"time"
)

// Quality is a plain-language grade of a measurement.
type Quality int

// Quality grades.
const (
	QualityUnknown Quality = iota // no data yet
	QualityGood
	QualityFair
	QualityPoor
)

func (q Quality) String() string {
	switch q {
	case QualityGood:
		return "good"
	case QualityFair:
		return "fair"
	case QualityPoor:
		return "poor"
	case QualityUnknown:
		return "unknown"
	}
	return "unknown"
}

// MarshalText stores a grade by name, so saved reports stay readable and do
// not depend on the order of the constants.
func (q Quality) MarshalText() ([]byte, error) { return []byte(q.String()), nil }

// UnmarshalText reads a grade written by MarshalText.
func (q *Quality) UnmarshalText(text []byte) error {
	for _, candidate := range []Quality{QualityUnknown, QualityGood, QualityFair, QualityPoor} {
		if candidate.String() == string(text) {
			*q = candidate
			return nil
		}
	}
	return fmt.Errorf("%w: quality %q", ErrInvalidSample, text)
}

// Generic thresholds for the "right now" indicator (tray icon). The daily
// report grades by the user's profile instead (Phase 5).
const (
	fairLatency = 50 * time.Millisecond
	poorLatency = 100 * time.Millisecond
	fairJitter  = 30 * time.Millisecond
	fairLoss    = 1.0 // percent
	poorLoss    = 3.0
)

// QuickQuality grades one round's internet latency for the tray indicator.
func QuickQuality(s LatencyStats) Quality {
	switch {
	case s.Sent == 0:
		return QualityUnknown
	case !s.Reachable() || s.LossPercent() > poorLoss || s.Median > poorLatency:
		return QualityPoor
	case s.LossPercent() > fairLoss || s.Median > fairLatency || s.Jitter > fairJitter:
		return QualityFair
	default:
		return QualityGood
	}
}
