package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

var t0 = time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC)

func TestEnsureNetworkIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	wifi := domain.Network{Kind: domain.ConnectionWiFi, InterfaceName: "Wi-Fi"}

	a, err := db.EnsureNetwork(ctx, wifi, "Casa", t0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.EnsureNetwork(ctx, wifi, "Casa", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.EnsureNetwork(ctx, wifi, "Trabalho", t0)
	if err != nil {
		t.Fatal(err)
	}
	if a == 0 || a != b {
		t.Errorf("same network got ids %d and %d", a, b)
	}
	if other == a {
		t.Error("different SSIDs must be different networks")
	}
}

func TestSaveMeasurements(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	net, err := db.EnsureNetwork(ctx, domain.Network{Kind: domain.ConnectionEthernet, InterfaceName: "Ethernet"}, "", t0)
	if err != nil {
		t.Fatal(err)
	}

	reached, err := domain.SummarizeLatency([]time.Duration{12 * time.Millisecond, 14 * time.Millisecond}, 2)
	if err != nil {
		t.Fatal(err)
	}
	samples := []domain.TargetLatency{
		{Target: domain.GatewayTarget, Stats: reached},
		{Target: "Quad9", Stats: domain.LatencyStats{Sent: 2}}, // nothing answered
	}
	if err := db.SaveLatency(ctx, t0, net, samples); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveLatency(ctx, t0, 0, samples); err != nil { // unknown network
		t.Fatal(err)
	}

	var rows, nullMedians int
	if err := db.sql.QueryRowContext(ctx,
		"SELECT COUNT(*), COUNT(*) - COUNT(median_us) FROM latency_samples").Scan(&rows, &nullMedians); err != nil {
		t.Fatal(err)
	}
	if rows != 4 || nullMedians != 2 {
		t.Errorf("rows=%d nullMedians=%d, want 4 and 2", rows, nullMedians)
	}

	wifi := domain.WiFi{SignalPercent: 80, RSSIdBm: -55, Channel: 44, Band: domain.Band5GHz, Standard: "Wi-Fi 6 (802.11ax)", RxRateMbps: 1200}
	if err := db.SaveWiFi(ctx, t0, net, wifi); err != nil {
		t.Fatal(err)
	}

	speed := &domain.SpeedResult{Start: t0, Server: "Cloudflare TST", IdleLatency: 15 * time.Millisecond,
		DownloadMbps: 700.5, UploadMbps: 99.1, BytesDown: 90 * domain.MB, BytesUp: 40 * domain.MB, DownloadCapped: true}
	if err := db.SaveSpeedTest(ctx, net, TriggerScheduled, speed); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSpeedTest(ctx, net, TriggerManual, speed); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSpeedTest(ctx, net, "whatever", speed); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("invalid trigger: error = %v, want ErrInvalidInput", err)
	}

	last, ok, err := db.LastSpeedTest(ctx)
	if err != nil || !ok || !last.Equal(t0) {
		t.Errorf("LastSpeedTest = %v, %v, %v; want %v", last, ok, err, t0)
	}
}

func TestLastSpeedTestIgnoresManualTests(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.SaveSpeedTest(ctx, 0, TriggerManual, &domain.SpeedResult{Start: t0}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.LastSpeedTest(ctx); err != nil || ok {
		t.Errorf("manual tests must not shift the schedule (ok=%v, err=%v)", ok, err)
	}
}

func TestOutages(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	id, err := db.StartOutage(ctx, 0, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EndOutage(ctx, id, t0.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.EndOutage(ctx, id, t0.Add(4*time.Minute)); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ending a closed outage: error = %v, want ErrInvalidInput", err)
	}

	if _, err := db.StartOutage(ctx, 0, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.CloseOpenOutages(ctx, t0.Add(90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var open int
	if err := db.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM outages WHERE ended_ms IS NULL").Scan(&open); err != nil {
		t.Fatal(err)
	}
	if open != 0 {
		t.Errorf("%d outages still open", open)
	}
}

func TestSessionsAndCoverage(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if _, ok, err := db.LastBeat(ctx); err != nil || ok {
		t.Fatalf("empty database: LastBeat ok=%v err=%v", ok, err)
	}

	// 19:00-21:00 (crosses the window start) and 22:00-22:30.
	s1, err := db.StartSession(ctx, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Beat(ctx, s1, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.Beat(ctx, s1, t0); err != nil { // older beat must not shrink the session
		t.Fatal(err)
	}
	s2, err := db.StartSession(ctx, t0.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Beat(ctx, s2, t0.Add(150*time.Minute)); err != nil {
		t.Fatal(err)
	}

	got, err := db.Coverage(ctx, t0, t0.Add(4*time.Hour)) // window 20:00-24:00
	if err != nil {
		t.Fatal(err)
	}
	if want := 90 * time.Minute; got != want {
		t.Errorf("Coverage = %v, want %v", got, want)
	}

	last, ok, err := db.LastBeat(ctx)
	if err != nil || !ok || !last.Equal(t0.Add(150*time.Minute)) {
		t.Errorf("LastBeat = %v, %v, %v", last, ok, err)
	}
	if err := db.Beat(ctx, 999, t0); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("beat on unknown session: error = %v, want ErrInvalidInput", err)
	}
}

func TestSettings(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if _, ok, err := db.Setting(ctx, SettingProfile); err != nil || ok {
		t.Fatalf("unset setting: ok=%v err=%v", ok, err)
	}
	for _, v := range []string{"jogos", "completo"} {
		if err := db.SetSetting(ctx, SettingProfile, v, t0); err != nil {
			t.Fatal(err)
		}
	}
	if v, ok, err := db.Setting(ctx, SettingProfile); err != nil || !ok || v != "completo" {
		t.Errorf("Setting = %q, %v, %v; want completo", v, ok, err)
	}
	if err := db.SetSetting(ctx, "", "x", t0); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty key: error = %v, want ErrInvalidInput", err)
	}
}
