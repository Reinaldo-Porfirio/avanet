// Package probe runs measurements: each probe sends traffic, waits for the
// result and returns domain values. Probes never store anything; scheduling
// and persistence belong to their callers.
package probe

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// Echoer sends ICMP echo requests ("pings") to one address.
type Echoer interface {
	Echo(ctx context.Context, target netip.Addr, opts EchoOptions) (EchoResult, error)
}

// EchoOptions controls one burst of echo requests.
type EchoOptions struct {
	Count    int           // requests to send
	Interval time.Duration // pause between requests
	Wait     time.Duration // how long to wait for the last reply
}

// DefaultEchoOptions takes ~3 s per round and a few KB of traffic.
var DefaultEchoOptions = EchoOptions{Count: 10, Interval: 200 * time.Millisecond, Wait: time.Second}

// Validate rejects options that would make a round meaningless or endless.
func (o EchoOptions) Validate() error {
	switch {
	case o.Count < 1 || o.Count > 100:
		return fmt.Errorf("echo count must be between 1 and 100, got %d", o.Count)
	case o.Interval < 10*time.Millisecond:
		return fmt.Errorf("echo interval must be at least 10ms, got %v", o.Interval)
	case o.Wait <= 0:
		return fmt.Errorf("echo wait must be positive, got %v", o.Wait)
	}
	return nil
}

// EchoResult holds the round trips of the replies, in the order the requests
// were sent; lost requests are absent.
type EchoResult struct {
	Sent int
	RTTs []time.Duration
}

// Target is a host pinged in every round.
type Target struct {
	Name string
	Addr netip.Addr
}

// InternetTargets returns the fixed public hosts used to measure internet
// latency: anycast DNS resolvers from three independent operators, so one
// operator's routing problem is not blamed on the user's provider. The list
// is fixed in code on purpose: AvaNet only talks to known hosts.
func InternetTargets() []Target {
	return []Target{
		{Name: "Cloudflare", Addr: netip.AddrFrom4([4]byte{1, 1, 1, 1})},
		{Name: "Google", Addr: netip.AddrFrom4([4]byte{8, 8, 8, 8})},
		{Name: "Quad9", Addr: netip.AddrFrom4([4]byte{9, 9, 9, 9})},
	}
}

// TargetResult is the outcome for one target in a round.
type TargetResult struct {
	Target Target
	Stats  domain.LatencyStats
	Err    error // the probe itself failed (not packet loss, which is in Stats)
}

// Round is the outcome of probing the router and the internet targets once.
type Round struct {
	Start    time.Time
	Gateway  *TargetResult // nil when the network has no gateway
	Internet []TargetResult
}

// Best returns the internet target with the lowest median latency among
// those that answered. It is the round's "internet latency": the user's
// connection is at least this good.
func (r Round) Best() (TargetResult, bool) {
	var best TargetResult
	found := false
	for _, res := range r.Internet {
		if res.Err != nil || !res.Stats.Reachable() {
			continue
		}
		if !found || res.Stats.Median < best.Stats.Median {
			best, found = res, true
		}
	}
	return best, found
}

// InternetReachable reports whether any internet target answered.
func (r Round) InternetReachable() bool {
	_, ok := r.Best()
	return ok
}

// Latency probes the router and the internet targets in parallel.
type Latency struct {
	Echoer  Echoer
	Options EchoOptions
	Now     func() time.Time // injectable clock; nil means time.Now
}

// Run performs one round. A zero gateway skips the router. Per-target
// failures are reported in the results; Run itself fails only when the
// options are invalid or ctx is cancelled.
func (l Latency) Run(ctx context.Context, gateway netip.Addr, targets []Target) (Round, error) {
	if err := l.Options.Validate(); err != nil {
		return Round{}, err
	}
	now := l.Now
	if now == nil {
		now = time.Now
	}

	all := targets
	if gateway.IsValid() {
		all = append([]Target{{Name: "gateway", Addr: gateway}}, targets...)
	}

	results := make([]TargetResult, len(all))
	var wg sync.WaitGroup
	start := now()
	for i, target := range all {
		wg.Go(func() { results[i] = l.probe(ctx, target) })
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return Round{}, fmt.Errorf("latency round interrupted: %w", err)
	}

	round := Round{Start: start, Internet: results}
	if gateway.IsValid() {
		round.Gateway = &results[0]
		round.Internet = results[1:]
	}
	return round, nil
}

func (l Latency) probe(ctx context.Context, target Target) TargetResult {
	res := TargetResult{Target: target}
	echo, err := l.Echoer.Echo(ctx, target.Addr, l.Options)
	if err != nil {
		res.Err = err
		return res
	}
	res.Stats, res.Err = domain.SummarizeLatency(echo.RTTs, echo.Sent)
	return res
}

// errNoEcho is returned by the ICMP echoer when nothing could be sent.
var errNoEcho = errors.New("no echo request could be sent")
