package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

var testNow = time.Date(2026, 11, 5, 21, 0, 0, 0, time.Local)

func openDB(t *testing.T) *storage.DB {
	t.Helper()
	db, _ := openDBAt(t)
	return db
}

// openDBAt also returns the file path, for read-only checks with a separate
// connection (see dbView).
func openDBAt(t *testing.T) (db *storage.DB, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "avanet.db")
	db, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db, path
}

var homeWiFi = domain.Network{Kind: domain.ConnectionWiFi, InterfaceName: "Wi-Fi", Cost: domain.CostUnmetered}

// fakeSpeed returns a runner whose test transfers the given bytes while the
// interface counters grow by counterGrowth, and the limits it was called with.
func fakeSpeed(usage UsageStore, down, up int64, counterGrowth uint64) (SpeedRunner, *[]domain.TransferLimits) {
	var calls []domain.TransferLimits
	var counted uint64
	return SpeedRunner{
		Run: func(_ context.Context, limits domain.TransferLimits) (domain.SpeedResult, error) {
			calls = append(calls, limits)
			return domain.SpeedResult{Start: testNow, Server: "Cloudflare TST", DownloadMbps: 512, UploadMbps: 39,
				IdleLatency: 13 * time.Millisecond, BytesDown: down, BytesUp: up}, nil
		},
		Counters: func(uint32) (domain.InterfaceCounters, error) {
			c := domain.InterfaceCounters{RxBytes: 1_000*domain.MB + counted}
			counted += counterGrowth
			return c, nil
		},
		Usage:  usage,
		Budget: domain.DefaultDataBudget(),
		Now:    func() time.Time { return testNow },
	}, &calls
}

func TestMeasureChargesTheBudget(t *testing.T) {
	db := openDB(t)
	runner, calls := fakeSpeed(db, 86*domain.MB, 11*domain.MB, 100*domain.MB)

	var started domain.TransferLimits
	out, err := runner.Measure(context.Background(), homeWiFi, func(l domain.TransferLimits) { started = l })
	if err != nil {
		t.Fatal(err)
	}
	want := domain.DefaultDataBudget().Fixed.PerTest
	if len(*calls) != 1 || (*calls)[0] != want || started != want {
		t.Errorf("test ran with %v (onStart %v), want the fixed per-test limits once", *calls, started)
	}
	used, err := db.DataUsage(context.Background(), domain.DayOf(testNow), domain.DataFixed)
	if err != nil {
		t.Fatal(err)
	}
	if used != 97*domain.MB || out.UsedToday != 97*domain.MB {
		t.Errorf("charged %d / reported %d bytes, want 97 MB", used, out.UsedToday)
	}
	if out.Result.LocalTraffic {
		t.Error("counters grew by about the test size: no local traffic expected")
	}
}

func TestMeasureFlagsLocalTraffic(t *testing.T) {
	runner, _ := fakeSpeed(openDB(t), 50*domain.MB, 10*domain.MB, 200*domain.MB)
	out, err := runner.Measure(context.Background(), homeWiFi, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Result.LocalTraffic {
		t.Error("interface moved 200 MB during a 60 MB test: local traffic expected")
	}
}

func TestMeasureRefusesMobileDataUnlessAllowed(t *testing.T) {
	hotspot := domain.Network{Kind: domain.ConnectionWiFi, Cost: domain.CostMetered}
	runner, calls := fakeSpeed(openDB(t), domain.MB, domain.MB, 0)

	if _, err := runner.Measure(context.Background(), hotspot, nil); !errors.Is(err, domain.ErrMobileDataDisabled) {
		t.Fatalf("error = %v, want ErrMobileDataDisabled", err)
	}
	if len(*calls) != 0 {
		t.Fatal("speed test ran on mobile data without consent")
	}

	runner.Budget.MobileEnabled = true
	if _, err := runner.Measure(context.Background(), hotspot, nil); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != runner.Budget.Mobile.PerTest {
		t.Errorf("mobile test ran with %v, want the mobile limits", *calls)
	}
}

func TestMeasureStopsWhenBudgetIsUsed(t *testing.T) {
	db := openDB(t)
	if err := db.AddDataUsage(context.Background(), domain.DayOf(testNow), domain.DataFixed, domain.DefaultDataBudget().Fixed.DailyBytes); err != nil {
		t.Fatal(err)
	}
	runner, calls := fakeSpeed(db, domain.MB, domain.MB, 0)
	if _, err := runner.Measure(context.Background(), homeWiFi, nil); !errors.Is(err, domain.ErrBudgetExhausted) || len(*calls) != 0 {
		t.Errorf("error = %v, calls = %d; want ErrBudgetExhausted and no test", err, len(*calls))
	}
}

func TestMeasureChargesPartialTransfersOnFailure(t *testing.T) {
	db := openDB(t)
	runner, _ := fakeSpeed(cancelBlindReads{db}, 0, 0, 0)
	boom := errors.New("connection reset")
	runner.Run = func(context.Context, domain.TransferLimits) (domain.SpeedResult, error) {
		return domain.SpeedResult{BytesDown: 15 * domain.MB}, boom
	}

	// Even a cancelled context (Ctrl+C mid-test) must not skip the charge.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runner.Measure(ctx, homeWiFi, nil); !errors.Is(err, boom) {
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
