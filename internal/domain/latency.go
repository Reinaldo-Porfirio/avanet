package domain

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// ErrInvalidSample is returned when measurement input is inconsistent.
var ErrInvalidSample = errors.New("invalid sample")

// LatencyStats summarizes one round of echo requests to a single target.
type LatencyStats struct {
	Sent     int
	Received int
	Min      time.Duration
	Median   time.Duration
	Max      time.Duration
	// Jitter is the mean absolute difference between consecutive round trips
	// (the RFC 3550 idea, without its smoothing): how much the delay varies
	// from one packet to the next, which is what makes calls and games stutter.
	Jitter time.Duration
}

// TargetLatency is the outcome for one target of a latency round, as stored.
type TargetLatency struct {
	Target string // "gateway" or an internet target's name
	Stats  LatencyStats
}

// GatewayTarget is the Target name used for the router.
const GatewayTarget = "gateway"

// Reachable reports whether the target answered at least once.
func (s LatencyStats) Reachable() bool { return s.Received > 0 }

// LossPercent is the share of requests that got no reply, from 0 to 100.
func (s LatencyStats) LossPercent() float64 {
	if s.Sent == 0 {
		return 0
	}
	return 100 * float64(s.Sent-s.Received) / float64(s.Sent)
}

// SummarizeLatency computes the statistics of one round. rtts must be in the
// order the requests were sent (not the order replies arrived), with lost
// requests simply absent.
func SummarizeLatency(rtts []time.Duration, sent int) (LatencyStats, error) {
	if sent < len(rtts) {
		return LatencyStats{}, fmt.Errorf("%w: %d replies for %d requests", ErrInvalidSample, len(rtts), sent)
	}
	for _, rtt := range rtts {
		if rtt < 0 {
			return LatencyStats{}, fmt.Errorf("%w: negative round trip %v", ErrInvalidSample, rtt)
		}
	}

	stats := LatencyStats{Sent: sent, Received: len(rtts)}
	if len(rtts) == 0 {
		return stats, nil
	}

	stats.Jitter = meanAbsoluteDifference(rtts)

	sorted := slices.Clone(rtts)
	slices.Sort(sorted)
	stats.Min = sorted[0]
	stats.Max = sorted[len(sorted)-1]
	stats.Median = median(sorted)
	return stats, nil
}

// median expects a sorted, non-empty slice.
func median(sorted []time.Duration) time.Duration {
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func meanAbsoluteDifference(values []time.Duration) time.Duration {
	if len(values) < 2 {
		return 0
	}
	var total time.Duration
	for i := 1; i < len(values); i++ {
		diff := values[i] - values[i-1]
		total += max(diff, -diff)
	}
	return total / time.Duration(len(values)-1)
}
