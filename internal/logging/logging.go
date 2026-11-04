// Package logging configures structured JSON logs written to a size-rotated file.
package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	fileName = "avanet.log"

	// DefaultMaxBytes keeps a single log file small enough to open in an editor.
	DefaultMaxBytes = 5 << 20 // 5 MiB
	// DefaultMaxBackups bounds disk usage to roughly (1 + backups) * MaxBytes.
	DefaultMaxBackups = 3

	dirPerm = 0o700
)

// Options configures New. Zero values select the defaults.
type Options struct {
	Dir        string
	Level      slog.Leveler
	MaxBytes   int64
	MaxBackups int
}

// New returns a logger writing JSON lines to Dir/avanet.log. The returned
// closer must be closed on shutdown to flush and release the file.
//
// Never log secrets, tokens or the public IP address.
func New(opts Options) (*slog.Logger, io.Closer, error) {
	if opts.Dir == "" {
		return nil, nil, errors.New("log directory is required")
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.MaxBackups == 0 {
		opts.MaxBackups = DefaultMaxBackups
	}
	if opts.MaxBytes < 0 || opts.MaxBackups < 0 {
		return nil, nil, fmt.Errorf("invalid rotation limits: max bytes %d, max backups %d", opts.MaxBytes, opts.MaxBackups)
	}
	if opts.Level == nil {
		opts.Level = slog.LevelInfo
	}

	if err := os.MkdirAll(opts.Dir, dirPerm); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}

	file, err := openRotatingFile(filepath.Join(opts.Dir, fileName), opts.MaxBytes, opts.MaxBackups)
	if err != nil {
		return nil, nil, err
	}

	handler := slog.NewJSONHandler(file, &slog.HandlerOptions{Level: opts.Level})
	return slog.New(handler), file, nil
}
