package app

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/netinfo"
	"github.com/Reinaldo-Porfirio/avanet/internal/probe"
	"github.com/Reinaldo-Porfirio/avanet/internal/wifi"
)

var testGateway = netip.MustParseAddr("192.168.0.1")

func stats(t *testing.T, sent int, rtts ...time.Duration) domain.LatencyStats {
	t.Helper()
	s, err := domain.SummarizeLatency(rtts, sent)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func wifiSources(t *testing.T) probeSources {
	t.Helper()
	targets := probe.InternetTargets()
	return probeSources{
		network: func() (domain.Network, error) {
			return domain.Network{Kind: domain.ConnectionWiFi, Description: "Test adapter", Gateway: testGateway,
				LinkSpeedMbps: 1200, Cost: domain.CostUnmetered}, nil
		},
		wifi: func() (domain.WiFi, error) {
			return domain.WiFi{SSID: "Casa", SignalPercent: 80, RSSIdBm: -55, Channel: 6, Band: domain.Band2GHz,
				Standard: "Wi-Fi 6 (802.11ax)", RxRateMbps: 574}, nil
		},
		latency: func(_ context.Context, gateway netip.Addr, _ []probe.Target) (probe.Round, error) {
			if gateway != testGateway {
				t.Errorf("latency called with gateway %v, want %v", gateway, testGateway)
			}
			gw := probe.TargetResult{Target: probe.Target{Name: "gateway", Addr: gateway},
				Stats: stats(t, 2, 1500*time.Microsecond, 2500*time.Microsecond)}
			return probe.Round{
				Gateway: &gw,
				Internet: []probe.TargetResult{
					{Target: targets[0], Stats: stats(t, 2, 12*time.Millisecond, 14*time.Millisecond)},
					{Target: targets[1], Stats: stats(t, 3, 50*time.Millisecond, 50*time.Millisecond)}, // one lost
					{Target: targets[2], Stats: stats(t, 2)},
				},
			}, nil
		},
	}
}

func TestProbeReportOverWiFi(t *testing.T) {
	report, err := collectProbe(context.Background(), wifiSources(t))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderProbe(&out, &report, 2)
	text := out.String()

	for _, want := range []string{
		"Wi-Fi (Test adapter)",
		"192.168.0.1",
		"1200 Mbps (velocidade até o roteador",
		"ilimitada",
		"Casa",
		"80% (-55 dBm)",
		"2,4 GHz · canal 6",
		"Wi-Fi 6 (802.11ax)",
		"Latência (2 pacotes por alvo)",
		"2,0 ms", // gateway median, decimal comma
		"33%",    // Google lost one of three
		"100%",   // Quad9 lost everything
		"Internet: 13,0 ms (melhor alvo: Cloudflare)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
}

func TestProbeReportExplainsProblems(t *testing.T) {
	src := wifiSources(t)
	src.wifi = func() (domain.WiFi, error) { return domain.WiFi{}, wifi.ErrAccessDenied }

	report, err := collectProbe(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderProbe(&out, &report, 2)
	if !strings.Contains(out.String(), "Privacidade e segurança > Localização") {
		t.Errorf("access-denied guidance missing:\n%s", out.String())
	}
}

func TestProbeReportWithoutConnection(t *testing.T) {
	src := wifiSources(t)
	src.network = func() (domain.Network, error) { return domain.Network{}, netinfo.ErrNoConnection }
	src.wifi = func() (domain.WiFi, error) {
		t.Error("wifi must not be read when there is no connection")
		return domain.WiFi{}, nil
	}
	src.latency = func(_ context.Context, gateway netip.Addr, _ []probe.Target) (probe.Round, error) {
		if gateway.IsValid() {
			t.Errorf("gateway %v pinged without a known network", gateway)
		}
		return probe.Round{Internet: []probe.TargetResult{{Target: probe.InternetTargets()[0], Stats: stats(t, 2)}}}, nil
	}

	report, err := collectProbe(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderProbe(&out, &report, 2)
	for _, want := range []string{"Nenhuma interface de rede leva à internet", "nenhum alvo respondeu"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestCollectProbePropagatesLatencyFailure(t *testing.T) {
	src := wifiSources(t)
	boom := errors.New("interrupted")
	src.latency = func(context.Context, netip.Addr, []probe.Target) (probe.Round, error) { return probe.Round{}, boom }
	if _, err := collectProbe(context.Background(), src); !errors.Is(err, boom) {
		t.Errorf("error = %v, want %v", err, boom)
	}
}

func TestInterruptedCommandExitsCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := runMain(ctx, []string{"probe", "-n", "1"}, nil)
	if r.code != ExitOK {
		t.Errorf("code %d, want %d (stderr %q)", r.code, ExitOK, r.stderr)
	}
}

func TestProbeRejectsInvalidCount(t *testing.T) {
	for _, n := range []string{"0", "101", "abc"} {
		r := runMain(context.Background(), []string{"probe", "-n", n}, nil)
		if r.code != ExitUsage {
			t.Errorf("-n %s: code %d, want %d", n, r.code, ExitUsage)
		}
	}
}
