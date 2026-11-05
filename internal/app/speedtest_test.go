package app

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

var testNow = time.Date(2026, 11, 5, 21, 0, 0, 0, time.Local)

func openUsageDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "avanet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

// speedFake returns sources for a network of the given kind/cost whose test
// transfers the given bytes; interface counters grow by counterGrowth.
func speedFake(t *testing.T, network domain.Network, down, up int64, counterGrowth uint64) (speedSources, *[]domain.TransferLimits) {
	t.Helper()
	var calls []domain.TransferLimits
	var counted uint64
	return speedSources{
		network: func() (domain.Network, error) { return network, nil },
		counters: func(uint32) (domain.InterfaceCounters, error) {
			c := domain.InterfaceCounters{RxBytes: 1_000*domain.MB + counted}
			counted += counterGrowth
			return c, nil
		},
		run: func(_ context.Context, limits domain.TransferLimits) (domain.SpeedResult, error) {
			calls = append(calls, limits)
			return domain.SpeedResult{Server: "Cloudflare TST", DownloadMbps: 512.34, UploadMbps: 38.9,
				IdleLatency: 13 * time.Millisecond, BytesDown: down, BytesUp: up}, nil
		},
		now: func() time.Time { return testNow },
	}, &calls
}

var wifiHome = domain.Network{Kind: domain.ConnectionWiFi, Cost: domain.CostUnmetered}

func TestMeasureSpeedChargesTheBudget(t *testing.T) {
	db := openUsageDB(t)
	src, calls := speedFake(t, wifiHome, 86*domain.MB, 11*domain.MB, 100*domain.MB)

	out, err := measureSpeed(context.Background(), src, db, domain.DefaultDataBudget(), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != domain.DefaultDataBudget().Fixed.PerTest {
		t.Errorf("test ran with %v, want the fixed per-test limits once", *calls)
	}
	used, err := db.DataUsage(context.Background(), domain.DayOf(testNow), domain.DataFixed)
	if err != nil {
		t.Fatal(err)
	}
	if used != 97*domain.MB || out.usedToday != 97*domain.MB {
		t.Errorf("charged %d / reported %d bytes, want 97 MB", used, out.usedToday)
	}
	if out.result.LocalTraffic {
		t.Error("counters grew by about the test size: no local traffic expected")
	}
}

func TestMeasureSpeedFlagsLocalTraffic(t *testing.T) {
	src, _ := speedFake(t, wifiHome, 50*domain.MB, 10*domain.MB, 200*domain.MB)
	out, err := measureSpeed(context.Background(), src, openUsageDB(t), domain.DefaultDataBudget(), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.result.LocalTraffic {
		t.Error("interface moved 200 MB during a 60 MB test: local traffic expected")
	}
}

func TestMeasureSpeedRefusesMobileDataUnlessAllowed(t *testing.T) {
	hotspot := domain.Network{Kind: domain.ConnectionWiFi, Cost: domain.CostMetered}
	src, calls := speedFake(t, hotspot, domain.MB, domain.MB, 0)

	_, err := measureSpeed(context.Background(), src, openUsageDB(t), domain.DefaultDataBudget(), &bytes.Buffer{})
	if !errors.Is(err, domain.ErrMobileDataDisabled) {
		t.Fatalf("error = %v, want ErrMobileDataDisabled", err)
	}
	if len(*calls) != 0 {
		t.Fatal("speed test ran on mobile data without consent")
	}

	allowed := domain.DefaultDataBudget()
	allowed.MobileEnabled = true
	if _, err := measureSpeed(context.Background(), src, openUsageDB(t), allowed, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != allowed.Mobile.PerTest {
		t.Errorf("mobile test ran with %v, want the mobile limits", *calls)
	}
}

func TestMeasureSpeedStopsWhenBudgetIsUsed(t *testing.T) {
	db := openUsageDB(t)
	budget := domain.DefaultDataBudget()
	if err := db.AddDataUsage(context.Background(), domain.DayOf(testNow), domain.DataFixed, budget.Fixed.DailyBytes); err != nil {
		t.Fatal(err)
	}
	src, calls := speedFake(t, wifiHome, domain.MB, domain.MB, 0)

	_, err := measureSpeed(context.Background(), src, db, budget, &bytes.Buffer{})
	if !errors.Is(err, domain.ErrBudgetExhausted) || len(*calls) != 0 {
		t.Errorf("error = %v, calls = %d; want ErrBudgetExhausted and no test", err, len(*calls))
	}
}

func TestMeasureSpeedChargesPartialTransfersOnFailure(t *testing.T) {
	db := openUsageDB(t)
	src, _ := speedFake(t, wifiHome, 0, 0, 0)
	boom := errors.New("connection reset")
	src.run = func(context.Context, domain.TransferLimits) (domain.SpeedResult, error) {
		return domain.SpeedResult{BytesDown: 15 * domain.MB}, boom
	}

	// Even a cancelled context (Ctrl+C mid-test) must not skip the charge.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := measureSpeed(ctx, src, cancelBlindReads{db}, domain.DefaultDataBudget(), &bytes.Buffer{}); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
	used, err := db.DataUsage(context.Background(), domain.DayOf(testNow), domain.DataFixed)
	if err != nil {
		t.Fatal(err)
	}
	if used != 15*domain.MB {
		t.Errorf("charged %d bytes, want the 15 MB already transferred", used)
	}
}

// cancelBlindReads ignores cancellation when reading usage, so the test can
// reach the speed run with an already-cancelled context.
type cancelBlindReads struct{ db *storage.DB }

func (u cancelBlindReads) DataUsage(ctx context.Context, day domain.Day, class domain.DataClass) (int64, error) {
	return u.db.DataUsage(context.WithoutCancel(ctx), day, class)
}

func (u cancelBlindReads) AddDataUsage(ctx context.Context, day domain.Day, class domain.DataClass, n int64) error {
	return u.db.AddDataUsage(ctx, day, class, n)
}

func TestRenderSpeed(t *testing.T) {
	out := speedOutcome{
		network: wifiHome, class: domain.DataFixed, usedToday: 194 * domain.MB, dailyLimit: 2000 * domain.MB,
		result: domain.SpeedResult{Server: "Cloudflare TST", DownloadMbps: 512.34, UploadMbps: 38.9,
			IdleLatency: 13 * time.Millisecond, BytesDown: 86 * domain.MB, BytesUp: 11 * domain.MB,
			DownloadCapped: true, LocalTraffic: true},
	}
	var buf bytes.Buffer
	renderSpeed(&buf, &out)
	for _, want := range []string{
		"Cloudflare TST · Wi-Fi",
		"512,3 Mbps",
		"38,9 Mbps",
		"13,0 ms",
		"97,0 MB · hoje: 194,0 MB de 2000,0 MB (rede fixa)",
		"teto de dados",
		"outros programas usaram a rede",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, buf.String())
		}
	}
}

func TestExplainSpeedError(t *testing.T) {
	for _, err := range []error{domain.ErrMobileDataDisabled, domain.ErrBudgetExhausted} {
		var buf bytes.Buffer
		if !explainSpeedError(&buf, err) || buf.Len() == 0 {
			t.Errorf("%v: not explained", err)
		}
	}
	if explainSpeedError(&bytes.Buffer{}, errors.New("unexpected")) {
		t.Error("unexpected errors must reach Main's generic handler")
	}
}

func TestSpeedTestRejectsArguments(t *testing.T) {
	r := runMain(context.Background(), []string{"speedtest", "extra"}, nil)
	if r.code != ExitUsage {
		t.Errorf("code %d, want %d", r.code, ExitUsage)
	}
}
