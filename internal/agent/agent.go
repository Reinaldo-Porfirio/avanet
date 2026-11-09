// Package agent is AvaNet's background monitor: it runs latency rounds at
// the profile's interval, schedules speed tests within the data budget,
// detects outages and records everything, plus how long it was running
// ("coverage").
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/netinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/probe"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

const (
	// outageThreshold consecutive unreachable rounds make an outage.
	outageThreshold = 2
	// A pause longer than gapFactor intervals means the computer slept or
	// hibernated: a new session starts, and it is not counted as an outage.
	gapFactor = 3
)

// Sources are the measurements the agent combines.
type Sources struct {
	Network func() (domain.Network, error)
	WiFi    func() (domain.WiFi, error)
	Latency func(ctx context.Context, gateway netip.Addr, targets []probe.Target) (probe.Round, error)
	Speed   SpeedRunner
}

// Store is the persistence the agent needs (implemented by *storage.DB).
type Store interface {
	UsageStore
	EnsureNetwork(ctx context.Context, n domain.Network, ssid string, at time.Time) (storage.NetworkID, error)
	SaveLatency(ctx context.Context, at time.Time, network storage.NetworkID, samples []domain.TargetLatency) error
	SaveWiFi(ctx context.Context, at time.Time, network storage.NetworkID, w domain.WiFi) error
	SaveSpeedTest(ctx context.Context, network storage.NetworkID, trigger storage.SpeedTrigger, r *domain.SpeedResult) error
	LastSpeedTest(ctx context.Context) (time.Time, bool, error)
	StartOutage(ctx context.Context, network storage.NetworkID, start time.Time) (int64, error)
	EndOutage(ctx context.Context, id int64, end time.Time) error
	CloseOpenOutages(ctx context.Context, at time.Time) error
	StartSession(ctx context.Context, at time.Time) (int64, error)
	Beat(ctx context.Context, session int64, at time.Time) error
	LastBeat(ctx context.Context) (time.Time, bool, error)
}

// Clock abstracts time so tests can run days of monitoring in milliseconds.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Config tunes the agent.
type Config struct {
	Profile domain.Profile
	Logger  *slog.Logger
	Clock   Clock // nil means the real clock
	// OnRound, if set, receives the status after every round (the console
	// mode prints it). It runs on the agent's goroutine and must be quick.
	OnRound func(Status)
	// Reports, if set, is told the time after every round so it can do its
	// daily work when due.
	Reports ReportTicker
}

// ReportTicker does the daily report work when it is due.
type ReportTicker interface {
	Tick(ctx context.Context, now time.Time) error
}

// Status is a snapshot of the latest measurements, for the tray.
type Status struct {
	At         time.Time
	Network    domain.Network
	Internet   domain.LatencyStats // best internet target of the last round
	BestTarget string
	Quality    domain.Quality
	Outage     bool
	LastSpeed  *domain.SpeedResult
	// SpeedNotice explains why the latest requested speed test did not run.
	SpeedNotice string
}

// Agent is the background monitor. Create it with New.
type Agent struct {
	cfg   Config
	src   Sources
	store Store
	clock Clock
	log   *slog.Logger

	manualSpeed chan struct{}

	mu     sync.Mutex
	status Status

	// Loop state, only touched by Run's goroutine.
	session     int64
	lastBeat    time.Time
	outages     *domain.OutageDetector
	outageID    int64 // open outage row, 0 if none
	degradation domain.DegradationDetector
	lastSpeed   time.Time // latest automatic speed test
}

// New validates the configuration and returns an agent ready to Run.
func New(cfg *Config, src Sources, store Store) (*Agent, error) {
	switch {
	case cfg.Profile.LightInterval <= 0 || cfg.Profile.SpeedInterval <= 0:
		return nil, fmt.Errorf("invalid profile intervals: %+v", cfg.Profile)
	case cfg.Logger == nil:
		return nil, errors.New("agent needs a logger")
	case src.Network == nil || src.WiFi == nil || src.Latency == nil || src.Speed.Run == nil || src.Speed.Counters == nil:
		return nil, errors.New("agent needs every measurement source")
	case store == nil:
		return nil, errors.New("agent needs a store")
	}
	if err := src.Speed.Budget.Validate(); err != nil {
		return nil, err
	}

	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	src.Speed.Usage = store
	src.Speed.Now = clock.Now

	outages, err := domain.NewOutageDetector(outageThreshold)
	if err != nil {
		return nil, err
	}
	return &Agent{
		cfg: *cfg, src: src, store: store, clock: clock, log: cfg.Logger,
		manualSpeed: make(chan struct{}, 1),
		outages:     outages,
	}, nil
}

// Status returns the latest snapshot. Safe for concurrent use.
func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// RequestSpeedTest asks for a speed test as soon as possible (it still
// respects the data budget). Safe for concurrent use; extra requests while
// one is pending are ignored.
func (a *Agent) RequestSpeedTest() {
	select {
	case a.manualSpeed <- struct{}{}:
	default:
	}
}

// Run monitors until ctx is cancelled. It only returns an error if the agent
// cannot start; problems during monitoring are logged and retried.
func (a *Agent) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.recoverPreviousRun(ctx); err != nil {
		return err
	}
	if err := a.startSession(ctx, a.clock.Now()); err != nil {
		return err
	}
	last, ok, err := a.store.LastSpeedTest(ctx)
	if err != nil {
		return err
	}
	if ok {
		a.lastSpeed = last
	}
	a.log.Info("monitoring started", "profile", a.cfg.Profile.ID,
		"light_interval", a.cfg.Profile.LightInterval, "speed_interval", a.cfg.Profile.SpeedInterval)

	for ctx.Err() == nil {
		started := a.clock.Now()
		if err := a.round(ctx); err != nil && ctx.Err() == nil {
			a.log.Error("measurement round failed", "error", err)
		}

		// A pending request is served before waiting: someone clicked the button.
		select {
		case <-a.manualSpeed:
			a.runManualSpeed(ctx)
		default:
		}

		wait := a.cfg.Profile.LightInterval - a.clock.Now().Sub(started)
		select {
		case <-ctx.Done():
		case <-a.clock.After(max(wait, 0)):
		case <-a.manualSpeed:
			a.runManualSpeed(ctx)
		}
	}

	a.shutdown(ctx)
	return nil
}

// recoverPreviousRun closes outages a previous run left open, dating their
// end at its last heartbeat (the last moment anything was observed).
func (a *Agent) recoverPreviousRun(ctx context.Context) error {
	last, ok, err := a.store.LastBeat(ctx)
	if err != nil || !ok {
		return err
	}
	return a.store.CloseOpenOutages(ctx, last)
}

func (a *Agent) startSession(ctx context.Context, at time.Time) error {
	id, err := a.store.StartSession(ctx, at)
	if err != nil {
		return err
	}
	a.session, a.lastBeat = id, at
	return nil
}

func (a *Agent) round(ctx context.Context) error {
	now := a.clock.Now()
	if err := a.handleGap(ctx, now); err != nil {
		return err
	}

	network, netErr := a.src.Network()
	if netErr != nil && !errors.Is(netErr, netinfo.ErrNoConnection) {
		a.log.Warn("network information unavailable", "error", netErr)
	}
	var netID storage.NetworkID
	if netErr == nil {
		var err error
		if netID, err = a.recordNetwork(ctx, network, now); err != nil {
			return err
		}
	}

	round, err := a.src.Latency(ctx, network.Gateway, probe.InternetTargets())
	if err != nil {
		return err
	}
	if err := a.store.SaveLatency(ctx, round.Start, netID, toSamples(round)); err != nil {
		return err
	}

	best, reachable := round.Best()
	if err := a.trackOutage(ctx, round.Start, reachable, netID); err != nil {
		return err
	}
	degraded := a.degradation.Observe(round.Start, best.Stats.Median, reachable)
	a.updateStatus(func(s *Status) {
		s.At, s.Network, s.Outage = round.Start, network, a.outageID != 0
		s.Internet, s.BestTarget, s.Quality = best.Stats, best.Target.Name, domain.QuickQuality(best.Stats)
		if !reachable {
			s.Quality = domain.QualityPoor
		}
	})

	if err := a.beat(ctx, a.clock.Now()); err != nil {
		return err
	}
	if netErr == nil && reachable {
		a.maybeSpeedTest(ctx, network, netID, degraded)
	}
	if a.cfg.OnRound != nil {
		a.cfg.OnRound(a.Status())
	}
	if a.cfg.Reports != nil {
		// A report problem must never stop the measurements.
		if err := a.cfg.Reports.Tick(ctx, a.clock.Now()); err != nil && ctx.Err() == nil {
			a.log.Error("daily report failed", "error", err)
		}
	}
	return nil
}

// handleGap starts a new session after sleep or hibernation. The pause is
// neither coverage nor an outage: an ongoing outage ends at the last beat.
func (a *Agent) handleGap(ctx context.Context, now time.Time) error {
	if now.Sub(a.lastBeat) <= gapFactor*a.cfg.Profile.LightInterval {
		return nil
	}
	a.log.Info("monitoring gap detected (sleep or hibernation)", "since", a.lastBeat, "gap", now.Sub(a.lastBeat).Round(time.Second))
	if a.outageID != 0 {
		if err := a.store.EndOutage(ctx, a.outageID, a.lastBeat); err != nil {
			return err
		}
		a.outageID = 0
	}
	outages, err := domain.NewOutageDetector(outageThreshold)
	if err != nil {
		return err
	}
	a.outages = outages
	return a.startSession(ctx, now)
}

func (a *Agent) recordNetwork(ctx context.Context, network domain.Network, now time.Time) (storage.NetworkID, error) {
	var wifi *domain.WiFi
	if network.Kind == domain.ConnectionWiFi {
		if w, err := a.src.WiFi(); err == nil {
			wifi = &w
		} else {
			a.log.Debug("wi-fi details unavailable", "error", err)
		}
	}
	ssid := ""
	if wifi != nil {
		ssid = wifi.SSID
	}
	id, err := a.store.EnsureNetwork(ctx, network, ssid, now)
	if err != nil {
		return 0, err
	}
	if wifi != nil {
		if err := a.store.SaveWiFi(ctx, now, id, *wifi); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (a *Agent) trackOutage(ctx context.Context, at time.Time, reachable bool, netID storage.NetworkID) error {
	event, err := a.outages.Observe(at, reachable)
	if err != nil {
		return err
	}
	switch event.Kind {
	case domain.OutageStarted:
		a.log.Warn("internet outage started", "since", event.Outage.Start)
		id, err := a.store.StartOutage(ctx, netID, event.Outage.Start)
		if err != nil {
			return err
		}
		a.outageID = id
	case domain.OutageEnded:
		a.log.Info("internet outage ended", "duration", event.Outage.End.Sub(event.Outage.Start).Round(time.Second))
		if a.outageID != 0 {
			if err := a.store.EndOutage(ctx, a.outageID, event.Outage.End); err != nil {
				return err
			}
		}
		a.outageID = 0
	case domain.OutageNoChange:
	}
	return nil
}

func (a *Agent) beat(ctx context.Context, at time.Time) error {
	if err := a.store.Beat(ctx, a.session, at); err != nil {
		return err
	}
	a.lastBeat = at
	return nil
}

// maybeSpeedTest runs a scheduled or adaptive test when one is due.
func (a *Agent) maybeSpeedTest(ctx context.Context, network domain.Network, netID storage.NetworkID, degraded bool) {
	now := a.clock.Now()
	limits, used, planErr := a.src.Speed.Plan(ctx, network)
	if planErr != nil {
		if !errors.Is(planErr, domain.ErrMobileDataDisabled) && !errors.Is(planErr, domain.ErrBudgetExhausted) {
			a.log.Error("speed test planning failed", "error", planErr)
		}
		return
	}

	class := domain.ClassOf(network)
	next := domain.NextSpeedTest(domain.SpeedSchedule{
		Now: now, Last: a.lastSpeed, Interval: a.cfg.Profile.SpeedInterval,
		RemainingBytes: a.src.Speed.Budget.Daily(class) - used, TestCost: limits.Total(),
		EndOfDay: domain.EndOfLocalDay(now),
	})
	switch {
	case !now.Before(next):
		a.runSpeedTest(ctx, network, netID, storage.TriggerScheduled)
	case degraded:
		a.log.Info("latency degraded; running an extra speed test", "baseline", a.degradation.Baseline())
		a.runSpeedTest(ctx, network, netID, storage.TriggerAdaptive)
	}
}

func (a *Agent) runManualSpeed(ctx context.Context) {
	network, err := a.src.Network()
	if err != nil {
		a.setSpeedNotice("Sem conexão: teste de velocidade não executado.")
		return
	}
	netID, err := a.recordNetwork(ctx, network, a.clock.Now())
	if err != nil {
		a.log.Error("record network failed", "error", err)
	}
	a.runSpeedTest(ctx, network, netID, storage.TriggerManual)
}

func (a *Agent) runSpeedTest(ctx context.Context, network domain.Network, netID storage.NetworkID, trigger storage.SpeedTrigger) {
	startedAt := a.clock.Now()
	out, err := a.src.Speed.Measure(ctx, network, nil)
	if trigger != storage.TriggerManual {
		// The schedule follows the agent's clock. Even a failed attempt moves
		// it, so a broken server is not hammered every round.
		a.lastSpeed = startedAt
	}
	switch {
	case errors.Is(err, domain.ErrMobileDataDisabled):
		a.setSpeedNotice("Dados móveis: teste de velocidade desligado para economizar sua franquia.")
		return
	case errors.Is(err, domain.ErrBudgetExhausted):
		a.setSpeedNotice("Orçamento de dados de hoje esgotado; volta à meia-noite.")
		return
	case err != nil:
		if ctx.Err() == nil {
			a.log.Warn("speed test failed", "trigger", trigger, "error", err)
			a.setSpeedNotice("O teste de velocidade falhou; tentaremos de novo mais tarde.")
		}
		return
	}

	if err := a.store.SaveSpeedTest(ctx, netID, trigger, &out.Result); err != nil {
		a.log.Error("save speed test failed", "error", err)
	}
	a.log.Info("speed test", "trigger", trigger, "down_mbps", out.Result.DownloadMbps,
		"up_mbps", out.Result.UploadMbps, "bytes", out.Result.BytesUsed())
	result := out.Result
	a.updateStatus(func(s *Status) { s.LastSpeed, s.SpeedNotice = &result, "" })
}

func (a *Agent) setSpeedNotice(msg string) {
	a.updateStatus(func(s *Status) { s.SpeedNotice = msg })
}

func (a *Agent) updateStatus(change func(*Status)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	change(&a.status)
}

// shutdown records the final heartbeat. It runs after cancellation, so it
// detaches from ctx with a short deadline of its own.
func (a *Agent) shutdown(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accountingTimeout)
	defer cancel()
	if err := a.beat(ctx, a.clock.Now()); err != nil {
		a.log.Error("final heartbeat failed", "error", err)
	}
	a.log.Info("monitoring stopped")
}

func toSamples(r probe.Round) []domain.TargetLatency {
	samples := make([]domain.TargetLatency, 0, len(r.Internet)+1)
	if r.Gateway != nil {
		samples = append(samples, domain.TargetLatency{Target: domain.GatewayTarget, Stats: r.Gateway.Stats})
	}
	for _, res := range r.Internet {
		samples = append(samples, domain.TargetLatency{Target: res.Target.Name, Stats: res.Stats})
	}
	return samples
}
