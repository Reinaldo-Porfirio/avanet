package config

import (
	"errors"
	"log/slog"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeEnv returns a getenv function backed by a map.
func fakeEnv(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// absPath builds an absolute path valid on the current OS.
func absPath(t *testing.T, elem ...string) string {
	t.Helper()
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	return filepath.Join(append([]string{root}, elem...)...)
}

func TestLoadUsesExplicitDataDir(t *testing.T) {
	dir := absPath(t, "data", "avanet")
	cfg, err := Load(fakeEnv(map[string]string{EnvDataDir: dir + string(filepath.Separator)}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != dir {
		t.Errorf("DataDir = %q, want cleaned %q", cfg.DataDir, dir)
	}
	if cfg.DatabasePath() != filepath.Join(dir, "avanet.db") {
		t.Errorf("DatabasePath = %q", cfg.DatabasePath())
	}
	if cfg.LogDir() != filepath.Join(dir, "logs") {
		t.Errorf("LogDir = %q", cfg.LogDir())
	}
}

func TestLoadDefaultDataDir(t *testing.T) {
	base := absPath(t, "home", "user")
	env := map[string]string{"LOCALAPPDATA": base, "XDG_DATA_HOME": base}

	cfg, err := Load(fakeEnv(env))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, appDirName); cfg.DataDir != want {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	valid := absPath(t, "data")
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"relative data dir", map[string]string{EnvDataDir: `data\avanet`}},
		{"no base dir available", map[string]string{}},
		{"unknown log level", map[string]string{EnvDataDir: valid, EnvLogLevel: "loud"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(fakeEnv(tt.env))
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestLoadLogLevel(t *testing.T) {
	valid := absPath(t, "data")
	tests := []struct {
		value string
		want  slog.Level
	}{
		{"", slog.LevelInfo},
		{"debug", slog.LevelDebug},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
	}
	for _, tt := range tests {
		cfg, err := Load(fakeEnv(map[string]string{EnvDataDir: valid, EnvLogLevel: tt.value}))
		if err != nil {
			t.Fatalf("level %q: %v", tt.value, err)
		}
		if cfg.LogLevel != tt.want {
			t.Errorf("level %q = %v, want %v", tt.value, cfg.LogLevel, tt.want)
		}
	}
}
