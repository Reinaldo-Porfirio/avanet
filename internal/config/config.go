// Package config loads and validates the process configuration at startup.
//
// Only settings needed before the database is open live here (where the data
// is and how much to log). User preferences such as the usage profile are
// stored in the database.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
)

// Environment variables read by Load.
const (
	EnvDataDir  = "AVANET_DATA_DIR"
	EnvLogLevel = "AVANET_LOG_LEVEL"
)

const (
	databaseFileName = "avanet.db"
	logDirName       = "logs"
)

// ErrInvalid is wrapped by every validation error returned by Load.
var ErrInvalid = errors.New("invalid configuration")

// Config is the validated process configuration.
type Config struct {
	// DataDir is an absolute, cleaned path holding the database and logs.
	DataDir  string
	LogLevel slog.Level
}

// DatabasePath is the location of the SQLite database file.
func (c Config) DatabasePath() string { return filepath.Join(c.DataDir, databaseFileName) }

// LogDir is the directory holding the log files.
func (c Config) LogDir() string { return filepath.Join(c.DataDir, logDirName) }

// Load builds the configuration from environment variables read through
// getenv, falling back to platform defaults.
func Load(getenv func(key string) string) (Config, error) {
	dataDir, err := resolveDataDir(getenv)
	if err != nil {
		return Config{}, err
	}

	level, err := parseLogLevel(getenv(EnvLogLevel))
	if err != nil {
		return Config{}, err
	}

	return Config{DataDir: dataDir, LogLevel: level}, nil
}

func resolveDataDir(getenv func(string) string) (string, error) {
	if dir := getenv(EnvDataDir); dir != "" {
		return absoluteDir(EnvDataDir, dir)
	}

	base, err := defaultBaseDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDirName), nil
}

// absoluteDir rejects relative paths, which would silently depend on the
// working directory the process happened to be started from.
func absoluteDir(source, dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%w: %s must be an absolute path, got %q", ErrInvalid, source, dir)
	}
	return filepath.Clean(dir), nil
}

func parseLogLevel(value string) (slog.Level, error) {
	if value == "" {
		return slog.LevelInfo, nil
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(value)); err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrInvalid, EnvLogLevel, err)
	}
	return level, nil
}
