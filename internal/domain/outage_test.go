package domain

import (
	"errors"
	"testing"
	"time"
)

func TestOutageDetector(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 21, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }

	type step struct {
		minute    int
		reachable bool
		want      OutageEvent
	}
	tests := []struct {
		name      string
		threshold int
		steps     []step
	}{
		{
			name:      "isolated failure is not an outage",
			threshold: 2,
			steps: []step{
				{0, true, OutageEvent{}},
				{1, false, OutageEvent{}},
				{2, true, OutageEvent{}},
				{3, false, OutageEvent{}},
				{4, true, OutageEvent{}},
			},
		},
		{
			name:      "outage is dated from the first failure",
			threshold: 2,
			steps: []step{
				{0, true, OutageEvent{}},
				{1, false, OutageEvent{}},
				{2, false, OutageEvent{Kind: OutageStarted, Outage: Outage{Start: at(1)}}},
				{3, false, OutageEvent{}}, // already ongoing: no new event
				{4, true, OutageEvent{Kind: OutageEnded, Outage: Outage{Start: at(1), End: at(4)}}},
				{5, true, OutageEvent{}},
			},
		},
		{
			name:      "threshold one starts immediately",
			threshold: 1,
			steps: []step{
				{0, false, OutageEvent{Kind: OutageStarted, Outage: Outage{Start: at(0)}}},
				{1, true, OutageEvent{Kind: OutageEnded, Outage: Outage{Start: at(0), End: at(1)}}},
			},
		},
		{
			name:      "second outage gets its own start",
			threshold: 2,
			steps: []step{
				{0, false, OutageEvent{}},
				{1, false, OutageEvent{Kind: OutageStarted, Outage: Outage{Start: at(0)}}},
				{2, true, OutageEvent{Kind: OutageEnded, Outage: Outage{Start: at(0), End: at(2)}}},
				{3, false, OutageEvent{}},
				{4, false, OutageEvent{Kind: OutageStarted, Outage: Outage{Start: at(3)}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewOutageDetector(tt.threshold)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range tt.steps {
				got, err := d.Observe(at(s.minute), s.reachable)
				if err != nil {
					t.Fatalf("minute %d: %v", s.minute, err)
				}
				if got != s.want {
					t.Errorf("minute %d (reachable=%v): got %+v, want %+v", s.minute, s.reachable, got, s.want)
				}
			}
		})
	}
}

func TestOutageDetectorRejectsInvalidUse(t *testing.T) {
	if _, err := NewOutageDetector(0); !errors.Is(err, ErrInvalidSample) {
		t.Errorf("threshold 0: error = %v, want ErrInvalidSample", err)
	}

	d, err := NewOutageDetector(2)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := d.Observe(now, true); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Observe(now.Add(-time.Second), true); !errors.Is(err, ErrInvalidSample) {
		t.Errorf("out-of-order check: error = %v, want ErrInvalidSample", err)
	}
}
