package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/analysis"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

func TestParseDayArg(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)
	for arg, want := range map[string]domain.Day{
		"":           "2026-11-04",
		"ontem":      "2026-11-04",
		"HOJE":       "2026-11-05",
		"2026-10-31": "2026-10-31",
	} {
		got, err := parseDayArg(arg, now)
		if err != nil || got != want {
			t.Errorf("parseDayArg(%q) = %q, %v; want %q", arg, got, err, want)
		}
	}
	if _, err := parseDayArg("amanhã?", now); err == nil {
		t.Error("invalid day accepted")
	}
}

func sampleReport() *analysis.Daily {
	at := time.Date(2026, 11, 3, 21, 5, 0, 0, time.Local)
	d := &analysis.Daily{
		Day: "2026-11-03", ProfileName: "Home office", Network: "Wi-Fi “Casa”",
		Grade: domain.QualityFair, Headline: "Dia firme, noite instável.", Lede: "Das 20h às 22h a velocidade caiu.",
		Coverage: analysis.Coverage{MonitoredMs: float64(15 * time.Hour / time.Millisecond), WindowMs: float64(24 * time.Hour / time.Millisecond), Rounds: 90, SpeedTests: 15},
		Download: analysis.Stat{Count: 15, Median: 254, P10: 112, P90: 301, Min: 41, MinAt: at, Max: 312, MaxAt: at},
		Outages:  []analysis.OutageSpan{{Start: at, End: at.Add(3 * time.Minute)}}, DowntimeMs: float64(3 * time.Minute / time.Millisecond),
		Diagnosis:     analysis.Diagnosis{Title: "A lentidão veio da operadora", Detail: "Sua rede de casa esteve bem."},
		DataUsedBytes: 1100 * domain.MB, IgnoredSpeedTests: 1,
	}
	d.Hours[7].Grade, d.Hours[13].Grade, d.Hours[20].Grade = domain.QualityGood, domain.QualityFair, domain.QualityPoor
	d.Periods[0] = analysis.Period{Name: "Madrugada", Condition: "Sem leitura"}
	d.Periods[3] = analysis.Period{Name: "Noite", Condition: "Tempestade", Grade: domain.QualityPoor, LatencyMs: 74, DownloadMbps: 118, UploadMbps: 22, Outages: 1}
	return d
}

func TestRenderReport(t *testing.T) {
	var out bytes.Buffer
	renderReport(&out, sampleReport())
	text := out.String()

	for _, want := range []string{
		"Boletim de terça-feira, 3 de novembro · perfil Home office · Wi-Fi “Casa”",
		"Dia firme, noite instável.",
		"Nota do dia: regular",
		"Monitorado 15h00 de 24h00 · 90 medições · 15 testes de velocidade",
		"1 teste(s) ignorado(s)",
		"·······█·····▒······!···",
		"Tempestade",
		"ping 74 ms · 118 ↓ / 22 ↑ Mbps · 1 queda(s)",
		"254,0 Mbps",
		"80% entre 112,0 e 301,0",
		"mín. 41,0 às 21:05",
		"Upload", "sem medições",
		"1 (0h03 no total) às 21:05",
		"A lentidão veio da operadora",
		"1100,0 MB",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
}

func TestRenderReportForToday(t *testing.T) {
	d := sampleReport()
	d.InProgress = true
	var out bytes.Buffer
	renderReport(&out, d)
	if !strings.HasPrefix(out.String(), "Hoje até agora · perfil") {
		t.Errorf("title = %q", strings.SplitN(out.String(), "\n", 2)[0])
	}
}

func TestReportCommandWithoutData(t *testing.T) {
	r := runMain(context.Background(), []string{"report", "2026-01-15"}, dataDirEnv(t))
	if r.code != ExitFailure || !strings.Contains(r.stderr, "Não há medições em 2026-01-15") {
		t.Errorf("code %d, stderr %q", r.code, r.stderr)
	}
}

func TestReportCommandRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{{"report", "a", "b"}, {"report", "quando"}, {"report", "-nope"}} {
		if r := runMain(context.Background(), args, dataDirEnv(t)); r.code != ExitUsage {
			t.Errorf("%v: code %d, want %d", args, r.code, ExitUsage)
		}
	}
}
