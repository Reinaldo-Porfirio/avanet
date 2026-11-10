package report

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/analysis"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

type fakeNotifier struct {
	titles, bodies []string
	err            error
}

func (f *fakeNotifier) Notify(title, body string) error {
	if f.err != nil {
		return f.err
	}
	f.titles, f.bodies = append(f.titles, title), append(f.bodies, body)
	return nil
}

type harness struct {
	t        *testing.T
	db       *storage.DB
	notifier *fakeNotifier
	profile  domain.ProfileID
	svc      *Service
}

func newHarness(t *testing.T) *harness {
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
	h := &harness{t: t, db: db, notifier: &fakeNotifier{}, profile: domain.ProfileBasic}
	h.svc = &Service{Store: db, Notifier: h.notifier, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Profile: func(context.Context) (domain.Profile, error) {
			p, _ := domain.ProfileByID(h.profile)
			return p, nil
		}}
	return h
}

// date builds a local time on November of the test year.
func date(day, hour, minute int) time.Time {
	return time.Date(2026, 11, day, hour, minute, 0, 0, time.Local)
}

// measure records a round at the given time.
func (h *harness) measure(at time.Time) {
	h.t.Helper()
	stats, err := domain.SummarizeLatency([]time.Duration{12 * time.Millisecond, 12 * time.Millisecond}, 2)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.db.SaveLatency(context.Background(), at, 0, []domain.TargetLatency{{Target: "Quad9", Stats: stats}}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) tick(at time.Time) {
	h.t.Helper()
	if err := h.svc.Tick(context.Background(), at); err != nil {
		h.t.Fatal(err)
	}
}

func TestReportIsDueOnlyAfterEightAndOncePerDay(t *testing.T) {
	h := newHarness(t)
	h.measure(date(4, 20, 0))

	h.tick(date(5, 7, 59))
	if len(h.notifier.titles) != 0 {
		t.Fatal("notified before 8h")
	}
	h.tick(date(5, 8, 0))
	if len(h.notifier.titles) != 1 || h.notifier.titles[0] != "Seu boletim de ontem está pronto" {
		t.Fatalf("titles = %q", h.notifier.titles)
	}
	if !strings.Contains(h.notifier.bodies[0], "Dia firme.") || !strings.Contains(h.notifier.bodies[0], "boa") {
		t.Errorf("body = %q", h.notifier.bodies[0])
	}

	h.tick(date(5, 8, 1))
	h.tick(date(5, 23, 0))
	if len(h.notifier.titles) != 1 {
		t.Errorf("notified %d times on the same day", len(h.notifier.titles))
	}

	stored, found, err := h.db.DailyReport(context.Background(), "2026-11-04")
	if err != nil || !found || stored.NotifiedAt.IsZero() {
		t.Errorf("stored = %+v, found=%v, err=%v", stored, found, err)
	}
}

func TestComputerOffAtEightGetsTheReportLater(t *testing.T) {
	h := newHarness(t)
	h.measure(date(4, 20, 0))
	h.tick(date(5, 19, 30)) // first round of the day, turned on in the evening
	if len(h.notifier.titles) != 1 {
		t.Errorf("notified %d times, want 1", len(h.notifier.titles))
	}
}

func TestSeveralMissingDaysAnnounceOnlyTheLatest(t *testing.T) {
	h := newHarness(t)
	h.measure(date(2, 20, 0))
	h.measure(date(3, 20, 0))
	h.tick(date(9, 10, 0)) // computer was off from the 4th to the 8th

	if len(h.notifier.titles) != 1 || h.notifier.titles[0] != "Seu boletim de 03/11 está pronto" {
		t.Fatalf("titles = %q", h.notifier.titles)
	}
	for _, day := range []domain.Day{"2026-11-02", "2026-11-03"} {
		stored, found, err := h.db.DailyReport(context.Background(), day)
		if err != nil || !found || stored.NotifiedAt.IsZero() {
			t.Errorf("%s: found=%v notified=%v err=%v", day, found, stored.NotifiedAt, err)
		}
	}

	// A new service (agent restarted) must not announce them again.
	h.svc.doneDay = ""
	h.tick(date(9, 11, 0))
	if len(h.notifier.titles) != 1 {
		t.Errorf("announced again after restart: %q", h.notifier.titles)
	}
}

func TestDayWithoutMeasurementsHasNoReport(t *testing.T) {
	h := newHarness(t)
	h.tick(date(5, 9, 0))
	if len(h.notifier.titles) != 0 {
		t.Errorf("notified about an empty day: %q", h.notifier.titles)
	}
	if _, err := h.svc.Load(context.Background(), "2026-11-04", date(5, 9, 0)); !errors.Is(err, analysis.ErrNoData) {
		t.Errorf("error = %v, want ErrNoData", err)
	}
}

func TestFailedNotificationIsRetriedLater(t *testing.T) {
	h := newHarness(t)
	h.measure(date(4, 20, 0))
	h.notifier.err = errors.New("toast failed")

	if err := h.svc.Tick(context.Background(), date(5, 8, 0)); err == nil {
		t.Fatal("failure not reported")
	}
	h.notifier.err = nil
	h.tick(date(5, 8, 30)) // within the retry pause
	if len(h.notifier.titles) != 0 {
		t.Fatal("retried too soon")
	}
	h.tick(date(5, 9, 1))
	if len(h.notifier.titles) != 1 {
		t.Errorf("notified %d times after recovering, want 1", len(h.notifier.titles))
	}
}

func TestLoadRebuildsWhenProfileChanges(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.measure(date(4, 20, 0))
	h.tick(date(5, 8, 0))

	first, err := h.svc.Load(ctx, "2026-11-04", date(5, 9, 0))
	if err != nil || first.ProfileID != string(domain.ProfileBasic) {
		t.Fatalf("first = %+v, err = %v", first, err)
	}

	h.profile = domain.ProfileGaming
	second, err := h.svc.Load(ctx, "2026-11-04", date(5, 9, 0))
	if err != nil || second.ProfileID != string(domain.ProfileGaming) {
		t.Fatalf("second = %+v, err = %v", second, err)
	}
	stored, _, err := h.db.DailyReport(ctx, "2026-11-04")
	if err != nil || stored.Profile != domain.ProfileGaming || stored.NotifiedAt.IsZero() {
		t.Errorf("stored = %+v, err = %v; rebuilt report must keep the notification mark", stored, err)
	}
}

func TestLoadKeepsReportAfterRawDataIsGone(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.measure(date(4, 20, 0))
	h.tick(date(5, 8, 0))

	later := date(5, 9, 0).AddDate(0, 0, retentionDays+2)
	if err := h.db.PruneRawData(ctx, later); err != nil {
		t.Fatal(err)
	}
	h.profile = domain.ProfileGaming
	report, err := h.svc.Load(ctx, "2026-11-04", later)
	if err != nil {
		t.Fatal(err)
	}
	if report.ProfileID != string(domain.ProfileBasic) {
		t.Errorf("profile = %s; without raw data the stored report must be kept as it is", report.ProfileID)
	}
}

func TestLoadTodayIsNeverStored(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.measure(date(5, 8, 0))

	report, err := h.svc.Load(ctx, "2026-11-05", date(5, 10, 0))
	if err != nil || !report.InProgress {
		t.Fatalf("report = %+v, err = %v", report, err)
	}
	if _, found, err := h.db.DailyReport(ctx, "2026-11-05"); err != nil || found {
		t.Errorf("a day in progress must not be stored (found=%v, err=%v)", found, err)
	}

	if _, err := h.svc.Load(ctx, "2026-11-20", date(5, 10, 0)); !errors.Is(err, analysis.ErrNoData) {
		t.Errorf("future day: error = %v, want ErrNoData", err)
	}
	if _, err := h.svc.Load(ctx, "ontem", date(5, 10, 0)); !errors.Is(err, domain.ErrInvalidSample) {
		t.Errorf("bad day: error = %v", err)
	}
}

func TestDaysListsStoredAndUnbuiltDays(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	now := date(9, 10, 0)
	h.measure(date(6, 12, 0))
	h.measure(date(7, 12, 0))
	h.measure(date(9, 9, 0))
	if _, err := h.svc.Load(ctx, "2026-11-06", now); err != nil { // stores it
		t.Fatal(err)
	}

	days, err := h.svc.Days(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	var got []domain.Day
	for _, d := range days {
		got = append(got, d.Day)
	}
	if want := []domain.Day{"2026-11-09", "2026-11-07", "2026-11-06"}; !slices.Equal(got, want) {
		t.Fatalf("days = %v, want %v (newest first, each once)", got, want)
	}
	if days[2].Headline == "" || days[2].Grade == "" {
		t.Errorf("a stored report must bring its summary: %+v", days[2])
	}
	if days[1].Headline != "" {
		t.Errorf("a day not built yet has no summary: %+v", days[1])
	}
}

func TestInsightsLookOnlyAtFinishedDays(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	now := date(9, 10, 0)
	for day := 1; day <= 9; day++ {
		h.measure(date(day, 12, 0))
	}

	got, err := h.svc.Insights(ctx, "2026-11-09", now, 500, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Until != "2026-11-08" {
		t.Errorf("until = %s: today is not finished", got.Until)
	}
	if got.Patterns.Days != analysis.PatternWindowDays {
		t.Errorf("pattern days = %d, want the last week only", got.Patterns.Days)
	}
	if got.Plan == nil || got.Plan.PlanDownloadMbps != 500 {
		t.Errorf("plan = %+v", got.Plan)
	}

	if got, err = h.svc.Insights(ctx, "2026-11-03", now, 0, 0); err != nil || got.Until != "2026-11-03" || got.Plan != nil {
		t.Errorf("past day: %+v, %v", got, err)
	}
	if got.Patterns.Days != 3 {
		t.Errorf("days up to the 3rd: %d", got.Patterns.Days)
	}
	if _, err := h.svc.Insights(ctx, "nope", now, 0, 0); !errors.Is(err, domain.ErrInvalidSample) {
		t.Errorf("bad day: %v", err)
	}
}

func TestTickPrunesOldRawData(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	now := date(9, 9, 0)
	old := now.AddDate(0, 0, -retentionDays-1)
	recent := now.AddDate(0, 0, -retentionDays+1)
	h.measure(old)
	h.measure(recent)
	h.tick(now)

	check := func(at time.Time, want bool) {
		t.Helper()
		got, err := h.db.HasMeasurements(ctx, at.Add(-time.Minute), at.Add(time.Minute))
		if err != nil || got != want {
			t.Errorf("measurements at %v: %v, %v; want %v", at, got, err, want)
		}
	}
	check(old, false)
	check(recent, true)
}
