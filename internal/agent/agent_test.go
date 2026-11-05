package agent

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/netip"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // read-only checks open their own connection

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/probe"
)

// fakeClock jumps forward instead of sleeping, so hours of monitoring run
// instantly. After fires immediately after advancing.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits int
	// sleeps adds extra time to the n-th wait (1-based): the computer
	// sleeping between two rounds.
	sleeps map[int]time.Duration
	// onWait runs at every wait with its number (used to end scenarios the
	// way a real shutdown happens: while waiting between rounds).
	onWait func(n int)
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.waits++
	n := c.waits
	d += c.sleeps[n]
	c.mu.Unlock()
	c.advance(d)
	if c.onWait != nil {
		c.onWait(n)
	}
	ch := make(chan time.Time, 1)
	ch <- c.Now()
	return ch
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// scenario drives the agent for a number of rounds.
type scenario struct {
	rounds      int
	reachable   func(round int) bool  // nil: always reachable
	sleepBefore map[int]time.Duration // round index -> sleep during the wait before it
	onStart     func(a *Agent)        // called before Run
}

type runResult struct {
	db         *dbView
	speedCalls int
	status     Status
}

func run(t *testing.T, sc scenario) runResult {
	t.Helper()
	db, path := openDBAt(t)
	clock := &fakeClock{now: time.Date(2026, 11, 5, 8, 0, 0, 0, time.Local), sleeps: map[int]time.Duration{}}
	for round, d := range sc.sleepBefore {
		clock.sleeps[round] = d // the wait before round i is the i-th wait
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock.onWait = func(n int) {
		if n == sc.rounds {
			cancel() // all scripted rounds done: stop while waiting
		}
	}

	speed, calls := fakeSpeed(nil, 86*domain.MB, 11*domain.MB, 97*domain.MB)
	round := 0
	src := Sources{
		Network: func() (domain.Network, error) {
			n := homeWiFi
			n.Gateway = netip.MustParseAddr("192.168.0.1")
			return n, nil
		},
		WiFi: func() (domain.WiFi, error) { return domain.WiFi{SSID: "Casa", SignalPercent: 90}, nil },
		Latency: func(_ context.Context, gw netip.Addr, targets []probe.Target) (probe.Round, error) {
			r := probe.Round{Start: clock.Now(), Gateway: &probe.TargetResult{
				Target: probe.Target{Name: "gateway", Addr: gw}, Stats: stats(t, 2, time.Millisecond, time.Millisecond)}}
			for _, target := range targets {
				s := stats(t, 2)
				if sc.reachable == nil || sc.reachable(round) {
					s = stats(t, 2, 12*time.Millisecond, 13*time.Millisecond)
				}
				r.Internet = append(r.Internet, probe.TargetResult{Target: target, Stats: s})
			}
			round++
			return r, nil
		},
		Speed: speed,
	}
	profile, _ := domain.ProfileByID(domain.ProfileComplete) // a round per minute
	a, err := New(Config{Profile: profile, Logger: discardLogger(), Clock: clock}, src, db)
	if err != nil {
		t.Fatal(err)
	}
	if sc.onStart != nil {
		sc.onStart(a)
	}
	if err := a.Run(ctx); err != nil {
		t.Fatal(err)
	}
	return runResult{db: newDBView(t, path), speedCalls: len(*calls), status: a.Status()}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func stats(t *testing.T, sent int, rtts ...time.Duration) domain.LatencyStats {
	t.Helper()
	s, err := domain.SummarizeLatency(rtts, sent)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAgentRecordsRoundsAndSchedulesSpeedTests(t *testing.T) {
	res := run(t, scenario{rounds: 180}) // 3 hours

	if got := res.db.count("SELECT COUNT(DISTINCT at_ms) FROM latency_samples"); got != 180 {
		t.Errorf("recorded %d rounds, want 180", got)
	}
	if got := res.db.count("SELECT COUNT(*) FROM latency_samples WHERE target = 'gateway'"); got != 180 {
		t.Errorf("recorded %d gateway samples, want 180", got)
	}
	if got := res.db.count("SELECT COUNT(*) FROM wifi_snapshots"); got != 180 {
		t.Errorf("recorded %d wi-fi snapshots, want 180", got)
	}
	if got := res.db.count("SELECT COUNT(*) FROM networks"); got != 1 {
		t.Errorf("recorded %d networks, want 1", got)
	}

	// 2 GB budget and 145 MB per test leave 13 tests for the 16 h left at
	// 08:00, so the hourly profile is stretched to one test every ~74 min:
	// 08:00, 09:14 and 10:28 (with 97 MB actually used each time, the
	// spacing shrinks a little as the day goes on, but not below 1 h).
	if res.speedCalls != 3 {
		t.Errorf("ran %d speed tests in 3 hours, want 3", res.speedCalls)
	}
	if got := res.db.count("SELECT COUNT(*) FROM speed_tests WHERE trigger = 'scheduled'"); got != int64(res.speedCalls) {
		t.Errorf("saved %d speed tests, ran %d", got, res.speedCalls)
	}
	if got := res.db.count("SELECT bytes FROM data_usage"); got != int64(res.speedCalls)*97*domain.MB {
		t.Errorf("charged %d bytes for %d tests", got, res.speedCalls)
	}

	if got := res.db.count("SELECT COUNT(*) FROM agent_sessions"); got != 1 {
		t.Errorf("%d sessions, want 1", got)
	}
	if got := res.db.count("SELECT (last_ms - started_ms) / 60000 FROM agent_sessions"); got < 179 {
		t.Errorf("session covers %d minutes, want ~180", got)
	}
	if res.status.Quality != domain.QualityGood || res.status.BestTarget == "" {
		t.Errorf("status = %+v, want a good, attributed round", res.status)
	}
}

func TestAgentDetectsOutages(t *testing.T) {
	// Rounds 10-14 unreachable: an outage from round 10 until round 15.
	res := run(t, scenario{rounds: 30, reachable: func(i int) bool { return i < 10 || i >= 15 }})

	if got := res.db.count("SELECT COUNT(*) FROM outages"); got != 1 {
		t.Fatalf("%d outages, want 1", got)
	}
	if got := res.db.count("SELECT (ended_ms - started_ms) / 60000 FROM outages"); got != 5 {
		t.Errorf("outage lasted %d minutes, want 5", got)
	}
}

func TestAgentTreatsSleepAsGapNotOutage(t *testing.T) {
	res := run(t, scenario{
		rounds:      20,
		reachable:   func(i int) bool { return i != 9 && i != 10 },
		sleepBefore: map[int]time.Duration{10: 2 * time.Hour}, // laptop lid closed
	})

	if got := res.db.count("SELECT COUNT(*) FROM agent_sessions"); got != 2 {
		t.Errorf("%d sessions, want 2 (before and after sleep)", got)
	}
	if got := res.db.count("SELECT COUNT(*) FROM outages"); got != 0 {
		t.Errorf("%d outages: a failed round before sleep and one after must not join into an outage", got)
	}
	if got := res.db.count("SELECT SUM(last_ms - started_ms) / 60000 FROM agent_sessions"); got > 25 {
		t.Errorf("coverage of %d minutes includes the sleep", got)
	}
}

func TestAgentRunsRequestedSpeedTest(t *testing.T) {
	res := run(t, scenario{rounds: 3, onStart: func(a *Agent) { a.RequestSpeedTest(); a.RequestSpeedTest() }})

	// Round 1 runs the day's first scheduled test; the two requests (the
	// second ignored while one is pending) add one manual test.
	if got := res.db.count("SELECT COUNT(*) FROM speed_tests WHERE trigger = 'manual'"); got != 1 {
		t.Errorf("%d manual tests, want 1", got)
	}
	if got := res.db.count("SELECT COUNT(*) FROM speed_tests WHERE trigger = 'scheduled'"); got != 1 {
		t.Errorf("%d scheduled tests, want 1 (manual tests must not shift the schedule)", got)
	}
	if res.status.LastSpeed == nil {
		t.Error("status must expose the latest speed test")
	}
}

func TestAgentClosesOutageLeftOpenByPreviousRun(t *testing.T) {
	db, path := openDBAt(t)
	ctx := context.Background()
	start := time.Date(2026, 11, 5, 7, 0, 0, 0, time.Local)
	session, err := db.StartSession(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Beat(ctx, session, start.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartOutage(ctx, 0, start.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	speed, _ := fakeSpeed(nil, 0, 0, 0)
	a, err := New(Config{Profile: domain.Profiles()[0], Logger: discardLogger()}, Sources{
		Network: func() (domain.Network, error) { return domain.Network{}, nil },
		WiFi:    func() (domain.WiFi, error) { return domain.WiFi{}, nil },
		Latency: func(context.Context, netip.Addr, []probe.Target) (probe.Round, error) { return probe.Round{}, nil },
		Speed:   speed,
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.recoverPreviousRun(ctx); err != nil {
		t.Fatal(err)
	}
	if got := newDBView(t, path).count("SELECT (ended_ms - started_ms) / 60000 FROM outages"); got != 5 {
		t.Errorf("open outage closed after %d minutes, want 5 (at the last heartbeat)", got)
	}
}

func TestNewRejectsIncompleteConfiguration(t *testing.T) {
	if _, err := New(Config{}, Sources{}, nil); err == nil {
		t.Error("empty configuration accepted")
	}
}

// dbView runs read-only checks through its own connection, so the storage
// package needs no test-only query method.
type dbView struct {
	t  *testing.T
	db *sql.DB
}

func newDBView(t *testing.T, path string) *dbView {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return &dbView{t: t, db: db}
}

func (v *dbView) count(query string) int64 {
	v.t.Helper()
	var n sql.NullInt64
	if err := v.db.QueryRowContext(context.Background(), query).Scan(&n); err != nil {
		v.t.Fatalf("%s: %v", query, err)
	}
	return n.Int64
}
