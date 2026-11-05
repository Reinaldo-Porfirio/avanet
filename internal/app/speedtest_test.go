package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/agent"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// The budget, charging and local-traffic rules are tested with
// agent.SpeedRunner; here only the command's own output is covered.

func TestRenderSpeed(t *testing.T) {
	out := agent.SpeedOutcome{
		Class: domain.DataFixed, UsedToday: 194 * domain.MB, DailyLimit: 2000 * domain.MB,
		Result: domain.SpeedResult{Server: "Cloudflare TST", DownloadMbps: 512.34, UploadMbps: 38.9,
			IdleLatency: 13 * time.Millisecond, BytesDown: 86 * domain.MB, BytesUp: 11 * domain.MB,
			DownloadCapped: true, LocalTraffic: true},
	}
	var buf bytes.Buffer
	renderSpeed(&buf, domain.Network{Kind: domain.ConnectionWiFi}, &out)
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
