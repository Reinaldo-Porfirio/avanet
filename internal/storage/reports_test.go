package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

const reportDay = domain.Day("2026-11-05")

func TestDailyReportRoundTripKeepsNotification(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if _, found, err := db.DailyReport(ctx, reportDay); err != nil || found {
		t.Fatalf("empty database: found=%v err=%v", found, err)
	}

	first := StoredReport{Day: reportDay, Profile: domain.ProfileGaming, Version: 1, JSON: `{"a":1}`, GeneratedAt: t0}
	if err := db.SaveDailyReport(ctx, &first); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkReportNotified(ctx, reportDay, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Rebuilding (e.g. after a profile change) must not notify again.
	rebuilt := StoredReport{Day: reportDay, Profile: domain.ProfileComplete, Version: 2, JSON: `{"a":2}`, GeneratedAt: t0.Add(time.Hour)}
	if err := db.SaveDailyReport(ctx, &rebuilt); err != nil {
		t.Fatal(err)
	}

	got, found, err := db.DailyReport(ctx, reportDay)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if got.Profile != domain.ProfileComplete || got.Version != 2 || got.JSON != `{"a":2}` || !got.GeneratedAt.Equal(rebuilt.GeneratedAt) {
		t.Errorf("rebuilt report = %+v", got)
	}
	if !got.NotifiedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("NotifiedAt = %v; rebuilding must keep it", got.NotifiedAt)
	}
}

func TestDailyReportRejectsInvalidInput(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.SaveDailyReport(ctx, &StoredReport{Day: "ontem"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad day: error = %v, want ErrInvalidInput", err)
	}
	if err := db.MarkReportNotified(ctx, reportDay, t0); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("missing report: error = %v, want ErrInvalidInput", err)
	}
}

func saveRound(t *testing.T, db *DB, at time.Time, net NetworkID) {
	t.Helper()
	stats, err := domain.SummarizeLatency([]time.Duration{12 * time.Millisecond}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveLatency(context.Background(), at, net, []domain.TargetLatency{{Target: "Quad9", Stats: stats}}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDay(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	from, to := t0, t0.Add(24*time.Hour)

	home, err := db.EnsureNetwork(ctx, domain.Network{Kind: domain.ConnectionWiFi, InterfaceName: "Wi-Fi"}, "Casa", t0)
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.EnsureNetwork(ctx, domain.Network{Kind: domain.ConnectionEthernet, InterfaceName: "Ethernet"}, "", t0)
	if err != nil {
		t.Fatal(err)
	}

	saveRound(t, db, from.Add(-time.Minute), home) // previous day
	saveRound(t, db, from, home)
	saveRound(t, db, from.Add(time.Hour), home)
	saveRound(t, db, from.Add(2*time.Hour), other)
	saveRound(t, db, to, home) // next day: `to` is exclusive

	if err := db.SaveWiFi(ctx, from.Add(time.Hour), home, domain.WiFi{SignalPercent: 80}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSpeedTest(ctx, home, TriggerScheduled, &domain.SpeedResult{Start: from.Add(time.Hour), DownloadMbps: 700, LocalTraffic: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddDataUsage(ctx, reportDay, domain.DataFixed, 90); err != nil {
		t.Fatal(err)
	}
	if err := db.AddDataUsage(ctx, reportDay, domain.DataMobile, 10); err != nil {
		t.Fatal(err)
	}

	// Crosses midnight into the day; entirely before it; still open.
	for _, o := range []struct {
		start time.Time
		end   time.Duration
	}{{from.Add(-time.Minute), 3 * time.Minute}, {from.Add(-time.Hour), time.Minute}, {from.Add(5 * time.Hour), 0}} {
		id, err := db.StartOutage(ctx, home, o.start)
		if err != nil {
			t.Fatal(err)
		}
		if o.end > 0 {
			if err := db.EndOutage(ctx, id, o.start.Add(o.end)); err != nil {
				t.Fatal(err)
			}
		}
	}

	session, err := db.StartSession(ctx, from.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Beat(ctx, session, from.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	d, err := db.LoadDay(ctx, reportDay, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Latency) != 3 || !d.Latency[0].At.Equal(from) || d.Latency[0].Stats.Median != 12*time.Millisecond {
		t.Errorf("latency records = %+v", d.Latency)
	}
	if len(d.Speed) != 1 || d.Speed[0].DownloadMbps != 700 || !d.Speed[0].LocalTraffic {
		t.Errorf("speed records = %+v", d.Speed)
	}
	if len(d.WiFi) != 1 || d.WiFi[0].SignalPercent != 80 {
		t.Errorf("wifi records = %+v", d.WiFi)
	}
	if len(d.Outages) != 2 || !d.Outages[1].End.IsZero() {
		t.Errorf("outages = %+v, want the one crossing midnight and the open one", d.Outages)
	}
	if len(d.Sessions) != 1 {
		t.Errorf("sessions = %+v", d.Sessions)
	}
	if d.DataUsed != 100 {
		t.Errorf("DataUsed = %d, want the sum of both classes", d.DataUsed)
	}
	if !d.HasNetwork || d.Network.SSID != "Casa" || d.Network.Kind != domain.ConnectionWiFi {
		t.Errorf("primary network = %+v (has=%v), want the one with most rounds", d.Network, d.HasNetwork)
	}
}

func TestHasMeasurementsAndPrune(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	saveRound(t, db, t0, 0)
	if err := db.SaveWiFi(ctx, t0, 0, domain.WiFi{SignalPercent: 50}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSpeedTest(ctx, 0, TriggerManual, &domain.SpeedResult{Start: t0}); err != nil {
		t.Fatal(err)
	}

	check := func(from, to time.Time, want bool) {
		t.Helper()
		got, err := db.HasMeasurements(ctx, from, to)
		if err != nil || got != want {
			t.Errorf("HasMeasurements(%v, %v) = %v, %v; want %v", from, to, got, err, want)
		}
	}
	check(t0, t0.Add(time.Hour), true)
	check(t0.Add(time.Minute), t0.Add(time.Hour), false)

	if err := db.PruneRawData(ctx, t0); err != nil { // cutoff is exclusive
		t.Fatal(err)
	}
	check(t0, t0.Add(time.Hour), true)

	if err := db.PruneRawData(ctx, t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	check(t0, t0.Add(time.Hour), false)
	var wifi, speed int
	if err := db.sql.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM wifi_snapshots), (SELECT COUNT(*) FROM speed_tests)").Scan(&wifi, &speed); err != nil {
		t.Fatal(err)
	}
	if wifi != 0 || speed != 1 {
		t.Errorf("after prune: wifi=%d speed=%d; speed tests must stay", wifi, speed)
	}
}
