package storage

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

func TestPreferencesDefaultsAndRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	got, invalid, err := db.Preferences(ctx)
	if err != nil || len(invalid) != 0 || got != domain.DefaultPreferences() {
		t.Fatalf("empty database: %+v invalid=%v err=%v", got, invalid, err)
	}

	want := domain.Preferences{
		Profile: domain.ProfileHomeOffice, FixedDailyMB: 750, MobileEnabled: true, MobileDailyMB: 120,
		PlanDownloadMbps: 300.5, PlanUploadMbps: 40,
	}
	if err := db.SavePreferences(ctx, want, t0); err != nil {
		t.Fatal(err)
	}
	if got, invalid, err = db.Preferences(ctx); err != nil || len(invalid) != 0 || got != want {
		t.Errorf("got %+v invalid=%v err=%v", got, invalid, err)
	}
}

func TestSavePreferencesRejectsInvalid(t *testing.T) {
	db := openTestDB(t)
	bad := domain.DefaultPreferences()
	bad.FixedDailyMB = 1
	if err := db.SavePreferences(context.Background(), bad, t0); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v", err)
	}
}

// A damaged value must fall back to its default without taking the good ones
// with it or stopping the monitoring.
func TestDamagedPreferencesFallBack(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	for key, value := range map[string]string{
		SettingProfile:       "does-not-exist",
		settingFixedDailyMB:  "muito",
		settingMobileEnabled: "talvez",
		settingMobileDailyMB: "9999",
		settingPlanDownload:  "NaN",
		settingPlanUpload:    "75",
		"some_future_key":    "ignored",
	} {
		if err := db.SetSetting(ctx, key, value, t0); err != nil {
			t.Fatal(err)
		}
	}

	got, invalid, err := db.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.DefaultPreferences()
	want.PlanUploadMbps = 75
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	slices.Sort(invalid)
	wantInvalid := []string{settingFixedDailyMB, settingMobileDailyMB, settingMobileEnabled, settingPlanDownload, SettingProfile}
	slices.Sort(wantInvalid)
	if !slices.Equal(invalid, wantInvalid) {
		t.Errorf("invalid = %v, want %v", invalid, wantInvalid)
	}
}

func countRows(t *testing.T, db *DB, table string) int {
	t.Helper()
	var n int
	if err := db.sql.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDeleteHistoryKeepsWhatTheRunningAgentNeeds(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	today := domain.DayOf(t0)
	yesterday := domain.DayOf(t0.AddDate(0, 0, -1))

	network, err := db.EnsureNetwork(ctx, domain.Network{Kind: domain.ConnectionWiFi, InterfaceName: "Wi-Fi"}, "Rede Antiga", t0)
	if err != nil {
		t.Fatal(err)
	}
	sample := []domain.TargetLatency{{Target: "Cloudflare", Stats: domain.LatencyStats{Sent: 1, Received: 1, Median: time.Millisecond}}}
	if err := db.SaveLatency(ctx, t0, network, sample); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveWiFi(ctx, t0, network, domain.WiFi{SSID: "Rede Antiga", SignalPercent: 70, Band: domain.Band5GHz}); err != nil {
		t.Fatal(err)
	}
	result := domain.SpeedResult{Start: t0, Server: "Cloudflare", DownloadMbps: 100, UploadMbps: 10}
	if err := db.SaveSpeedTest(ctx, network, TriggerManual, &result); err != nil {
		t.Fatal(err)
	}
	closed, err := db.StartOutage(ctx, network, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EndOutage(ctx, closed, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartSession(ctx, t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	current, err := db.StartSession(ctx, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Beat(ctx, current, t0.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.AddDataUsage(ctx, today, domain.DataFixed, 500); err != nil {
		t.Fatal(err)
	}
	if err := db.AddDataUsage(ctx, yesterday, domain.DataFixed, 900); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveDailyReport(ctx, &StoredReport{Day: yesterday, Profile: domain.ProfileBasic, Version: 1, JSON: `{}`, GeneratedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(ctx, SettingProfile, string(domain.ProfileGaming), t0); err != nil {
		t.Fatal(err)
	}

	if err := db.DeleteHistory(ctx, today); err != nil {
		t.Fatal(err)
	}
	if err := db.Compact(ctx); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"latency_samples", "wifi_snapshots", "speed_tests", "outages", "daily_reports", "networks"} {
		if n := countRows(t, db, table); n != 0 {
			t.Errorf("%s still has %d rows", table, n)
		}
	}
	if n := countRows(t, db, "agent_sessions"); n != 1 {
		t.Errorf("%d sessions, want only the current one", n)
	}
	if err := db.Beat(ctx, current, t0.Add(11*time.Minute)); err != nil {
		t.Errorf("the running agent must keep working: %v", err)
	}
	if coverage, err := db.Coverage(ctx, t0.Add(-2*time.Hour), t0.Add(time.Hour)); err != nil || coverage > 2*time.Minute {
		t.Errorf("coverage = %v, err = %v: erased time still counted", coverage, err)
	}
	if used, err := db.DataUsage(ctx, today, domain.DataFixed); err != nil || used != 500 {
		t.Errorf("today's usage = %d, err = %v: the daily limit must survive", used, err)
	}
	if used, err := db.DataUsage(ctx, yesterday, domain.DataFixed); err != nil || used != 0 {
		t.Errorf("yesterday's usage = %d, err = %v", used, err)
	}
	if _, found, err := db.Setting(ctx, SettingProfile); err != nil || !found {
		t.Errorf("preferences must stay: found=%v err=%v", found, err)
	}
}

func TestDeleteHistoryKeepsAnOutageInProgress(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	network, err := db.EnsureNetwork(ctx, domain.Network{Kind: domain.ConnectionEthernet, InterfaceName: "Ethernet"}, "", t0)
	if err != nil {
		t.Fatal(err)
	}
	open, err := db.StartOutage(ctx, network, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteHistory(ctx, domain.DayOf(t0)); err != nil {
		t.Fatal(err)
	}
	if err := db.EndOutage(ctx, open, t0.Add(time.Minute)); err != nil {
		t.Errorf("the outage in progress must still be closable: %v", err)
	}
}

func TestReportSummaries(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	for _, r := range []StoredReport{
		{Day: "2026-11-05", JSON: `{"grade":"good","headline":"Dia firme."}`},
		{Day: "2026-11-07", JSON: `{"grade":"poor","headline":"Noite ruim."}`},
		{Day: "2026-11-06", JSON: `{}`},
	} {
		r.Profile, r.Version, r.GeneratedAt = domain.ProfileBasic, 1, t0
		if err := db.SaveDailyReport(ctx, &r); err != nil {
			t.Fatal(err)
		}
	}

	got, err := db.ReportSummaries(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []ReportSummary{{Day: "2026-11-07", Grade: "poor", Headline: "Noite ruim."}, {Day: "2026-11-06"}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
