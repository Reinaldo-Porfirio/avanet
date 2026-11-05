package probe

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// fakeEchoer returns canned results per address and records what it was asked.
type fakeEchoer struct {
	mu      sync.Mutex
	results map[netip.Addr]EchoResult
	errs    map[netip.Addr]error
	asked   []netip.Addr
}

func (f *fakeEchoer) Echo(_ context.Context, target netip.Addr, _ EchoOptions) (EchoResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, target)
	if err := f.errs[target]; err != nil {
		return EchoResult{}, err
	}
	return f.results[target], nil
}

func millis(values ...int) []time.Duration {
	out := make([]time.Duration, len(values))
	for i, v := range values {
		out[i] = time.Duration(v) * time.Millisecond
	}
	return out
}

var (
	gw         = netip.MustParseAddr("192.168.0.1")
	cloudflare = netip.MustParseAddr("1.1.1.1")
	google     = netip.MustParseAddr("8.8.8.8")
	quad9      = netip.MustParseAddr("9.9.9.9")
)

func TestLatencyRoundPicksFastestReachableTarget(t *testing.T) {
	echoer := &fakeEchoer{
		results: map[netip.Addr]EchoResult{
			gw:         {Sent: 3, RTTs: millis(1, 1, 2)},
			cloudflare: {Sent: 3, RTTs: millis(12, 11, 13)},
			google:     {Sent: 3, RTTs: millis(58, 60, 59)},
			quad9:      {Sent: 3}, // everything lost
		},
	}
	fixed := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	l := Latency{Echoer: echoer, Options: DefaultEchoOptions, Now: func() time.Time { return fixed }}

	round, err := l.Run(context.Background(), gw, InternetTargets())
	if err != nil {
		t.Fatal(err)
	}

	if !round.Start.Equal(fixed) {
		t.Errorf("Start = %v, want %v", round.Start, fixed)
	}
	if round.Gateway == nil || round.Gateway.Stats.Median != time.Millisecond {
		t.Fatalf("gateway result = %+v", round.Gateway)
	}
	if len(round.Internet) != 3 {
		t.Fatalf("got %d internet results, want 3", len(round.Internet))
	}
	best, ok := round.Best()
	if !ok || best.Target.Name != "Cloudflare" || best.Stats.Median != 12*time.Millisecond {
		t.Errorf("Best = %+v, %v; want Cloudflare at 12ms", best, ok)
	}
	if !round.InternetReachable() {
		t.Error("InternetReachable = false")
	}
	if loss := round.Internet[2].Stats.LossPercent(); loss != 100 {
		t.Errorf("Quad9 loss = %v, want 100", loss)
	}
}

func TestLatencyRoundWithoutGateway(t *testing.T) {
	echoer := &fakeEchoer{results: map[netip.Addr]EchoResult{}}
	round, err := Latency{Echoer: echoer, Options: DefaultEchoOptions}.Run(context.Background(), netip.Addr{}, InternetTargets())
	if err != nil {
		t.Fatal(err)
	}
	if round.Gateway != nil {
		t.Error("Gateway should be nil without a gateway address")
	}
	for _, asked := range echoer.asked {
		if asked == gw {
			t.Error("router was pinged although no gateway was given")
		}
	}
	if round.InternetReachable() {
		t.Error("no target answered, InternetReachable must be false")
	}
}

func TestLatencyRoundKeepsPerTargetErrors(t *testing.T) {
	boom := errors.New("socket failure")
	echoer := &fakeEchoer{
		results: map[netip.Addr]EchoResult{google: {Sent: 2, RTTs: millis(30, 30)}},
		errs:    map[netip.Addr]error{cloudflare: boom},
	}
	round, err := Latency{Echoer: echoer, Options: DefaultEchoOptions}.Run(context.Background(), netip.Addr{}, InternetTargets())
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(round.Internet[0].Err, boom) {
		t.Errorf("Cloudflare error = %v, want %v", round.Internet[0].Err, boom)
	}
	if best, ok := round.Best(); !ok || best.Target.Name != "Google" {
		t.Errorf("Best = %+v, %v; failed target must be ignored", best, ok)
	}
}

func TestLatencyRoundReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Latency{Echoer: &fakeEchoer{}, Options: DefaultEchoOptions}.Run(ctx, gw, InternetTargets())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestEchoOptionsValidate(t *testing.T) {
	invalid := []EchoOptions{
		{Count: 0, Interval: time.Second, Wait: time.Second},
		{Count: 101, Interval: time.Second, Wait: time.Second},
		{Count: 5, Interval: time.Millisecond, Wait: time.Second},
		{Count: 5, Interval: time.Second, Wait: 0},
	}
	for _, o := range invalid {
		if o.Validate() == nil {
			t.Errorf("%+v: Validate succeeded, want error", o)
		}
	}
	if err := DefaultEchoOptions.Validate(); err != nil {
		t.Errorf("defaults invalid: %v", err)
	}
}

func TestInternetTargetsReturnsFreshSlice(t *testing.T) {
	a := InternetTargets()
	a[0].Name = "changed"
	if InternetTargets()[0].Name == "changed" {
		t.Error("InternetTargets shares state between callers")
	}
}
