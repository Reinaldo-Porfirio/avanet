package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/agent"
	"github.com/Reinaldo-Porfirio/avanet/internal/config"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

func dataDirEnv(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{config.EnvDataDir: filepath.Join(t.TempDir(), "AvaNet")}
}

func TestProfileListAndChoose(t *testing.T) {
	env := dataDirEnv(t)

	r := runMain(context.Background(), []string{"profile"}, env)
	if r.code != ExitOK || !strings.Contains(r.stdout, "▸ Uso básico (basico)") {
		t.Fatalf("default profile not marked: code %d\n%s", r.code, r.stdout)
	}

	r = runMain(context.Background(), []string{"profile", "JOGOS"}, env)
	if r.code != ExitOK || !strings.Contains(r.stdout, "Perfil alterado para Jogos online") {
		t.Fatalf("choose profile: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}

	r = runMain(context.Background(), []string{"profile"}, env)
	if !strings.Contains(r.stdout, "▸ Jogos online (jogos)") {
		t.Errorf("chosen profile not marked:\n%s", r.stdout)
	}

	if r := runMain(context.Background(), []string{"profile", "turbo"}, env); r.code != ExitUsage {
		t.Errorf("unknown profile: code %d, want %d", r.code, ExitUsage)
	}
}

func TestStatusOnFreshInstall(t *testing.T) {
	r := runMain(context.Background(), []string{"status"}, dataDirEnv(t))
	if r.code != ExitOK {
		t.Fatalf("code %d, stderr %q", r.code, r.stderr)
	}
	for _, want := range []string{
		"parado · perfil Uso básico",
		"Monitorado hoje",
		"0,0 MB de 2000,0 MB (rede fixa)",
		"nenhuma ainda",
		"nenhum ainda",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("status lacks %q:\n%s", want, r.stdout)
		}
	}
}

func TestManagementCommandsRejectBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{"status", "extra"},
		{"profile", "a", "b"},
		{"autostart", "maybe"},
		{"autostart", "on", "off"},
		{"run", "-no-such-flag"},
	} {
		if r := runMain(context.Background(), args, dataDirEnv(t)); r.code != ExitUsage {
			t.Errorf("%v: code %d, want %d", args, r.code, ExitUsage)
		}
	}
}

func TestInstanceNameDependsOnDataDir(t *testing.T) {
	a := instanceName(config.Config{DataDir: `C:\Users\x\AppData\Local\AvaNet`})
	b := instanceName(config.Config{DataDir: `c:\users\X\appdata\local\avanet`}) // Windows paths ignore case
	c := instanceName(config.Config{DataDir: `C:\Temp\avanet-dev`})
	if a != b {
		t.Error("the same directory in different case must share the instance")
	}
	if a == c {
		t.Error("different data directories must not block each other")
	}
}

func TestConsolePrinter(t *testing.T) {
	stats, err := domain.SummarizeLatency([]time.Duration{11 * time.Millisecond, 13 * time.Millisecond}, 2)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 5, 21, 3, 4, 0, time.Local)
	speed := &domain.SpeedResult{DownloadMbps: 700, UploadMbps: 99, BytesDown: 90 * domain.MB, BytesUp: 40 * domain.MB}

	var out bytes.Buffer
	printStatus := consolePrinter(&out)
	printStatus(agent.Status{At: at, Internet: stats, BestTarget: "Quad9", LastSpeed: speed})
	printStatus(agent.Status{At: at.Add(time.Minute), Internet: stats, BestTarget: "Quad9", LastSpeed: speed}) // same test
	printStatus(agent.Status{At: at.Add(2 * time.Minute), Outage: true, LastSpeed: speed})

	text := out.String()
	for _, want := range []string{"21:03:04  internet 12,0 ms (Quad9)", "velocidade 700,0 Mbps ↓", "sem conexão com a internet"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "velocidade"); n != 1 {
		t.Errorf("speed result printed %d times, want once", n)
	}
}

func TestHumanInterval(t *testing.T) {
	tests := map[time.Duration]string{
		time.Minute:      "1 minuto",
		5 * time.Minute:  "5 minutos",
		time.Hour:        "1 hora",
		3 * time.Hour:    "3 horas",
		90 * time.Minute: "1h30",
	}
	for d, want := range tests {
		if got := humanInterval(d); got != want {
			t.Errorf("humanInterval(%v) = %q, want %q", d, got, want)
		}
	}
}
