package domain

import (
	"math"
	"time"
)

// SpeedSchedule is the input for deciding when the next speed test runs.
type SpeedSchedule struct {
	Now      time.Time
	Last     time.Time     // last speed test; zero if none
	Interval time.Duration // the profile's desired spacing
	// RemainingBytes left in today's budget; TestCost is one test's worst case.
	RemainingBytes int64
	TestCost       int64
	EndOfDay       time.Time // when the budget resets (local midnight)
}

// NextSpeedTest returns when the next scheduled speed test should run.
//
// It keeps the profile's interval while the budget can afford it, and
// otherwise spreads the affordable tests evenly over the rest of the day, so
// the budget is not spent in the morning and the evening goes unmeasured. With
// no test affordable it waits for the budget to reset at EndOfDay.
func NextSpeedTest(s SpeedSchedule) time.Time {
	if s.TestCost <= 0 || s.RemainingBytes < s.TestCost {
		return s.EndOfDay
	}
	affordable := s.RemainingBytes / s.TestCost

	spacing := s.Interval
	if left := s.EndOfDay.Sub(s.Now); left > 0 {
		// The spacing that fits `affordable` tests into the time left today.
		needed := time.Duration(math.Ceil(float64(left) / float64(affordable)))
		spacing = max(spacing, needed)
	}

	if s.Last.IsZero() {
		return s.Now
	}
	if next := s.Last.Add(spacing); next.After(s.Now) {
		return next
	}
	return s.Now
}

// EndOfLocalDay returns the next local midnight after t.
func EndOfLocalDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, t.Location())
}

// StartOfLocalDay returns the local midnight that starts t's day.
func StartOfLocalDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
