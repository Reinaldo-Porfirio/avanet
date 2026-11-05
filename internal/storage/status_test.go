package storage

import (
	"context"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

func TestLatestRoundAndSpeedTest(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if _, _, found, err := db.LatestRound(ctx); err != nil || found {
		t.Fatalf("empty database: found=%v err=%v", found, err)
	}
	if _, _, found, err := db.LatestSpeedTest(ctx); err != nil || found {
		t.Fatalf("empty database: found=%v err=%v", found, err)
	}

	reached, err := domain.SummarizeLatency([]time.Duration{10 * time.Millisecond, 14 * time.Millisecond}, 3)
	if err != nil {
		t.Fatal(err)
	}
	old := []domain.TargetLatency{{Target: "Quad9", Stats: reached}}
	latest := []domain.TargetLatency{
		{Target: domain.GatewayTarget, Stats: reached},
		{Target: "Google", Stats: domain.LatencyStats{Sent: 3}},
	}
	if err := db.SaveLatency(ctx, t0, 0, old); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveLatency(ctx, t0.Add(time.Minute), 0, latest); err != nil {
		t.Fatal(err)
	}

	at, samples, found, err := db.LatestRound(ctx)
	if err != nil || !found {
		t.Fatalf("LatestRound: found=%v err=%v", found, err)
	}
	if !at.Equal(t0.Add(time.Minute)) || len(samples) != 2 {
		t.Fatalf("LatestRound = %v with %d samples, want the newer round with 2", at, len(samples))
	}
	if samples[0].Target != domain.GatewayTarget || samples[0].Stats != reached {
		t.Errorf("gateway sample = %+v, want %+v (round trip through the database)", samples[0], reached)
	}
	if samples[1].Stats.Reachable() || samples[1].Stats.LossPercent() != 100 {
		t.Errorf("unanswered target came back as %+v", samples[1].Stats)
	}

	want := domain.SpeedResult{Start: t0, Server: "Cloudflare TST", IdleLatency: 15 * time.Millisecond,
		DownloadMbps: 700.5, UploadMbps: 99.25, BytesDown: 90 * domain.MB, BytesUp: 40 * domain.MB, UploadCapped: true}
	if err := db.SaveSpeedTest(ctx, 0, TriggerAdaptive, &want); err != nil {
		t.Fatal(err)
	}
	got, trigger, found, err := db.LatestSpeedTest(ctx)
	if err != nil || !found || trigger != TriggerAdaptive {
		t.Fatalf("LatestSpeedTest: found=%v trigger=%q err=%v", found, trigger, err)
	}
	if !got.Start.Equal(want.Start) {
		t.Errorf("Start = %v, want %v", got.Start, want.Start)
	}
	got.Start = want.Start // compared above; time zones differ
	if got != want {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, want)
	}
}
