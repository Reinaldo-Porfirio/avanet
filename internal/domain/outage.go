package domain

import (
	"fmt"
	"time"
)

// Outage is a period without internet connectivity.
type Outage struct {
	Start time.Time
	End   time.Time // zero while the outage is ongoing
}

// OutageEventKind tells what changed after an observation.
type OutageEventKind int

// Outage event kinds.
const (
	OutageNoChange OutageEventKind = iota
	OutageStarted
	OutageEnded
)

// OutageEvent is returned by OutageDetector.Observe.
type OutageEvent struct {
	Kind   OutageEventKind
	Outage Outage
}

// OutageDetector turns a sequence of connectivity checks into outages.
//
// A single failed check is not an outage (one lost round can be a hiccup):
// an outage starts only after `threshold` consecutive failures, and is dated
// from the first of them. It ends at the first successful check.
type OutageDetector struct {
	threshold    int
	failures     int
	firstFailure time.Time
	last         time.Time
	ongoing      bool
}

// NewOutageDetector returns a detector requiring threshold (>= 1)
// consecutive failures before declaring an outage.
func NewOutageDetector(threshold int) (*OutageDetector, error) {
	if threshold < 1 {
		return nil, fmt.Errorf("%w: outage threshold must be >= 1, got %d", ErrInvalidSample, threshold)
	}
	return &OutageDetector{threshold: threshold}, nil
}

// Observe records one connectivity check. Checks must be given in time order.
func (d *OutageDetector) Observe(at time.Time, reachable bool) (OutageEvent, error) {
	if !d.last.IsZero() && at.Before(d.last) {
		return OutageEvent{}, fmt.Errorf("%w: check at %v is before previous check at %v", ErrInvalidSample, at, d.last)
	}
	d.last = at

	if reachable {
		if !d.ongoing {
			d.failures = 0
			return OutageEvent{}, nil
		}
		event := OutageEvent{Kind: OutageEnded, Outage: Outage{Start: d.firstFailure, End: at}}
		d.ongoing, d.failures = false, 0
		return event, nil
	}

	d.failures++
	if d.failures == 1 {
		d.firstFailure = at
	}
	if !d.ongoing && d.failures >= d.threshold {
		d.ongoing = true
		return OutageEvent{Kind: OutageStarted, Outage: Outage{Start: d.firstFailure}}, nil
	}
	return OutageEvent{}, nil
}
