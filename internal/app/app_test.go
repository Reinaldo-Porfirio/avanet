package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Reinaldo-Porfirio/avanet/internal/config"
)

type result struct {
	code   int
	stdout string
	stderr string
}

func runMain(ctx context.Context, args []string, vars map[string]string) result {
	var stdout, stderr bytes.Buffer
	code := Main(ctx, args, Env{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(key string) string { return vars[key] },
	})
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestVersion(t *testing.T) {
	r := runMain(context.Background(), []string{"version"}, nil)
	if r.code != ExitOK || !strings.HasPrefix(r.stdout, "avanet ") {
		t.Errorf("version: code %d, stdout %q", r.code, r.stdout)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"bogus"},
		{"version", "extra"},
		{"run", "extra"},
		{"run", "-no-such-flag"},
	} {
		r := runMain(context.Background(), args, nil)
		if r.code != ExitUsage {
			t.Errorf("%v: code %d, want %d", args, r.code, ExitUsage)
		}
		if !strings.Contains(r.stderr, "Uso:") {
			t.Errorf("%v: usage not printed to stderr", args)
		}
	}
}

func TestHelp(t *testing.T) {
	r := runMain(context.Background(), []string{"help"}, nil)
	if r.code != ExitOK || !strings.Contains(r.stdout, "Comandos:") {
		t.Errorf("help: code %d, stdout %q", r.code, r.stdout)
	}
}

// cancelOnWrite cancels a context the first time something is written to it,
// which happens when the agent announces it is running.
type cancelOnWrite struct {
	buf    bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelOnWrite) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	w.cancel()
	return n, err
}

// TestRunStartsAndStopsCleanly simulates Ctrl+C right after startup: the agent
// must have created its files and must exit with success.
func TestRunStartsAndStopsCleanly(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "AvaNet")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout := &cancelOnWrite{cancel: cancel}
	var stderr bytes.Buffer

	code := Main(ctx, nil, Env{
		Stdout: stdout,
		Stderr: &stderr,
		Getenv: func(key string) string { return map[string]string{config.EnvDataDir: dataDir}[key] },
	})
	if code != ExitOK {
		t.Fatalf("run: code %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.buf.String(), "em execução") {
		t.Errorf("startup message missing, stdout %q", stdout.buf.String())
	}

	for _, name := range []string{"avanet.db", filepath.Join("logs", "avanet.log")} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); err != nil {
			t.Errorf("%s not created: %v", name, err)
		}
	}
}

func TestRunInterruptedDuringStartupIsNotAFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := runMain(ctx, nil, map[string]string{config.EnvDataDir: filepath.Join(t.TempDir(), "AvaNet")})
	if r.code != ExitOK {
		t.Errorf("code %d, want %d (stderr %q)", r.code, ExitOK, r.stderr)
	}
}

func TestRunReportsInvalidConfig(t *testing.T) {
	r := runMain(context.Background(), []string{"run"}, map[string]string{config.EnvDataDir: "relative"})
	if r.code != ExitFailure {
		t.Errorf("code %d, want %d (stderr %q)", r.code, ExitFailure, r.stderr)
	}
}
