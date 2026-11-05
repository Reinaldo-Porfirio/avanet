package domain

import "time"

// DegradationDetector decides when a sudden latency increase deserves an
// extra speed test, to confirm (and record) how bad things got.
//
// It keeps a slowly moving baseline of the internet latency and fires when a
// round is both twice the baseline and at least 30 ms above it, at most once
// per cooldown. Degraded rounds do not move the baseline, so a long bad
// period is not "learned" as normal.
type DegradationDetector struct {
	baseline    time.Duration
	samples     int
	lastTrigger time.Time
}

const (
	degradationWarmup    = 5 // rounds before the baseline is trusted
	degradationFactor    = 2
	degradationMinExcess = 30 * time.Millisecond
	degradationCooldown  = time.Hour
	baselineWeight       = 0.1 // exponential moving average weight of a new round
)

// Observe feeds one round's internet latency (median of the best target) and
// reports whether an extra speed test should run now.
func (d *DegradationDetector) Observe(at time.Time, latency time.Duration, reachable bool) bool {
	if !reachable || latency <= 0 {
		return false // outages are tracked separately
	}

	degraded := d.samples >= degradationWarmup &&
		latency >= degradationFactor*d.baseline &&
		latency-d.baseline >= degradationMinExcess
	if degraded {
		if d.lastTrigger.IsZero() || at.Sub(d.lastTrigger) >= degradationCooldown {
			d.lastTrigger = at
			return true
		}
		return false
	}

	if d.samples == 0 {
		d.baseline = latency
	} else {
		d.baseline += time.Duration(baselineWeight * float64(latency-d.baseline))
	}
	d.samples++
	return false
}

// Baseline returns the current reference latency (zero before any sample).
func (d *DegradationDetector) Baseline() time.Duration { return d.baseline }
