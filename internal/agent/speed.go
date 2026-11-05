package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// accountingTimeout bounds the write that charges the data budget. It runs
// even after cancellation: bytes already transferred must always be counted.
const accountingTimeout = 5 * time.Second

// UsageStore persists the daily data usage.
type UsageStore interface {
	DataUsage(ctx context.Context, day domain.Day, class domain.DataClass) (int64, error)
	AddDataUsage(ctx context.Context, day domain.Day, class domain.DataClass, bytes int64) error
}

// SpeedRunner runs speed tests within the data budget. It is shared by the
// agent and the `avanet speedtest` command, so both follow the same rules.
type SpeedRunner struct {
	Run      func(ctx context.Context, limits domain.TransferLimits) (domain.SpeedResult, error)
	Counters func(index uint32) (domain.InterfaceCounters, error)
	Usage    UsageStore
	Budget   domain.DataBudget
	Now      func() time.Time
}

// SpeedOutcome is a budgeted speed test's result and the budget after it.
type SpeedOutcome struct {
	Class      domain.DataClass
	Limits     domain.TransferLimits
	Result     domain.SpeedResult
	UsedToday  int64 // including this test
	DailyLimit int64
}

// Plan returns the limits the next test on this network would get, or why it
// may not run (domain.ErrMobileDataDisabled, domain.ErrBudgetExhausted).
func (s SpeedRunner) Plan(ctx context.Context, network domain.Network) (domain.TransferLimits, int64, error) {
	if err := s.Budget.Validate(); err != nil {
		return domain.TransferLimits{}, 0, err
	}
	class := domain.ClassOf(network)
	used, err := s.Usage.DataUsage(ctx, domain.DayOf(s.Now()), class)
	if err != nil {
		return domain.TransferLimits{}, 0, err
	}
	limits, err := s.Budget.Plan(class, used)
	return limits, used, err
}

// Measure checks the budget, runs the test, charges the bytes used (even when
// the test fails or is cancelled) and flags local traffic. onStart, if not
// nil, is called with the limits right before the transfers begin.
func (s SpeedRunner) Measure(ctx context.Context, network domain.Network, onStart func(domain.TransferLimits)) (SpeedOutcome, error) {
	limits, used, err := s.Plan(ctx, network)
	if err != nil {
		return SpeedOutcome{}, err
	}
	out := SpeedOutcome{Class: domain.ClassOf(network), Limits: limits, DailyLimit: s.Budget.Daily(domain.ClassOf(network))}
	day := domain.DayOf(s.Now())

	before, countersErr := s.Counters(network.InterfaceIndex)
	if onStart != nil {
		onStart(limits)
	}
	result, runErr := s.Run(ctx, limits)

	if result.BytesUsed() > 0 {
		chargeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accountingTimeout)
		chargeErr := s.Usage.AddDataUsage(chargeCtx, day, out.Class, result.BytesUsed())
		cancel()
		if chargeErr != nil {
			return SpeedOutcome{}, errors.Join(runErr, fmt.Errorf("charge data budget: %w", chargeErr))
		}
	}
	if runErr != nil {
		return SpeedOutcome{}, fmt.Errorf("speed test: %w", runErr)
	}

	if countersErr == nil {
		if after, err := s.Counters(network.InterfaceIndex); err == nil {
			result.LocalTraffic = domain.LocalTrafficSuspected(before, after, result.BytesUsed())
		}
	}
	out.Result = result
	out.UsedToday = used + result.BytesUsed()
	return out, nil
}
