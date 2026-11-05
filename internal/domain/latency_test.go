package domain

import (
	"errors"
	"testing"
	"time"
)

func ms(values ...float64) []time.Duration {
	out := make([]time.Duration, len(values))
	for i, v := range values {
		out[i] = time.Duration(v * float64(time.Millisecond))
	}
	return out
}

func TestSummarizeLatency(t *testing.T) {
	tests := []struct {
		name     string
		rtts     []time.Duration
		sent     int
		want     LatencyStats
		wantLoss float64
	}{
		{
			name:     "all lost",
			rtts:     nil,
			sent:     5,
			want:     LatencyStats{Sent: 5},
			wantLoss: 100,
		},
		{
			name: "single reply has no jitter",
			rtts: ms(12),
			sent: 1,
			want: LatencyStats{Sent: 1, Received: 1, Min: ms(12)[0], Median: ms(12)[0], Max: ms(12)[0]},
		},
		{
			name: "odd count: median is the middle value",
			rtts: ms(10, 30, 20),
			sent: 3,
			// jitter: |30-10| + |20-30| = 30 over 2 differences = 15
			want: LatencyStats{Sent: 3, Received: 3, Min: ms(10)[0], Median: ms(20)[0], Max: ms(30)[0], Jitter: ms(15)[0]},
		},
		{
			name: "even count: median averages the two middle values",
			rtts: ms(10, 20, 30, 40),
			sent: 4,
			want: LatencyStats{Sent: 4, Received: 4, Min: ms(10)[0], Median: ms(25)[0], Max: ms(40)[0], Jitter: ms(10)[0]},
		},
		{
			name:     "jitter uses send order, not sorted order",
			rtts:     ms(10, 40, 10, 40),
			sent:     5, // one request lost
			want:     LatencyStats{Sent: 5, Received: 4, Min: ms(10)[0], Median: ms(25)[0], Max: ms(40)[0], Jitter: ms(30)[0]},
			wantLoss: 20,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SummarizeLatency(tt.rtts, tt.sent)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
			if loss := got.LossPercent(); loss != tt.wantLoss {
				t.Errorf("LossPercent = %v, want %v", loss, tt.wantLoss)
			}
			if got.Reachable() != (tt.want.Received > 0) {
				t.Errorf("Reachable = %v", got.Reachable())
			}
		})
	}
}

func TestSummarizeLatencyDoesNotModifyInput(t *testing.T) {
	rtts := ms(30, 10, 20)
	if _, err := SummarizeLatency(rtts, 3); err != nil {
		t.Fatal(err)
	}
	if rtts[0] != ms(30)[0] {
		t.Error("input slice was reordered")
	}
}

func TestSummarizeLatencyRejectsInconsistentInput(t *testing.T) {
	for name, tc := range map[string]struct {
		rtts []time.Duration
		sent int
	}{
		"more replies than requests": {ms(1, 2), 1},
		"negative round trip":        {[]time.Duration{-time.Millisecond}, 1},
	} {
		if _, err := SummarizeLatency(tc.rtts, tc.sent); !errors.Is(err, ErrInvalidSample) {
			t.Errorf("%s: error = %v, want ErrInvalidSample", name, err)
		}
	}
}

func TestLossPercentWithNothingSent(t *testing.T) {
	if loss := (LatencyStats{}).LossPercent(); loss != 0 {
		t.Errorf("LossPercent = %v, want 0", loss)
	}
}
