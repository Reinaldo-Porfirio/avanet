package probe

import "time"

// progressPoint is the cumulative payload received at some time after the
// transfer started.
type progressPoint struct {
	at    time.Duration
	bytes int64
}

const (
	// warmupFraction of each transfer is ignored: during it TCP is still
	// growing its congestion window ("slow start") and the rate is not yet
	// what the link can carry. Phase 0 showed a 50 MB download still
	// accelerating on a ~600 Mbps link without this correction.
	warmupFraction = 0.3
	// Transfers shorter than this are too short for a steady window; their
	// average rate is used instead.
	minSteadyDuration = 200 * time.Millisecond
)

// throughputMbps returns the steady-state rate of a transfer, in megabits per
// second, from its progress points (which must start at {0, 0} and grow).
func throughputMbps(points []progressPoint) float64 {
	if len(points) < 2 {
		return 0
	}
	end := points[len(points)-1]
	if end.at <= 0 {
		return 0
	}
	if end.at < minSteadyDuration {
		return mbps(end.bytes, end.at)
	}

	warmEnd := time.Duration(float64(end.at) * warmupFraction)
	for _, p := range points {
		if p.at >= warmEnd && p.at < end.at {
			return mbps(end.bytes-p.bytes, end.at-p.at)
		}
	}
	return mbps(end.bytes, end.at)
}

func mbps(bytes int64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	return float64(bytes) * 8 / elapsed.Seconds() / 1e6
}
