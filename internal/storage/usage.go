package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// ErrInvalidInput is returned when a repository call receives bad arguments.
var ErrInvalidInput = errors.New("invalid storage input")

// AddDataUsage adds bytes to the usage of one day and data class.
func (db *DB) AddDataUsage(ctx context.Context, day domain.Day, class domain.DataClass, bytes int64) error {
	if err := validateUsageKey(day, class); err != nil {
		return err
	}
	if bytes < 0 {
		return fmt.Errorf("%w: negative byte count %d", ErrInvalidInput, bytes)
	}

	const upsert = `
		INSERT INTO data_usage (day, data_class, bytes) VALUES (?, ?, ?)
		ON CONFLICT (day, data_class) DO UPDATE SET bytes = bytes + excluded.bytes`
	if _, err := db.sql.ExecContext(ctx, upsert, string(day), class.String(), bytes); err != nil {
		return fmt.Errorf("add data usage: %w", err)
	}
	return nil
}

// DataUsage returns the bytes used on one day for a data class (0 if none).
func (db *DB) DataUsage(ctx context.Context, day domain.Day, class domain.DataClass) (int64, error) {
	if err := validateUsageKey(day, class); err != nil {
		return 0, err
	}

	var bytes int64
	err := db.sql.QueryRowContext(ctx,
		"SELECT bytes FROM data_usage WHERE day = ? AND data_class = ?", string(day), class.String()).Scan(&bytes)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("read data usage: %w", err)
	}
	return bytes, nil
}

func validateUsageKey(day domain.Day, class domain.DataClass) error {
	if !day.Valid() {
		return fmt.Errorf("%w: day %q", ErrInvalidInput, day)
	}
	if class != domain.DataFixed && class != domain.DataMobile {
		return fmt.Errorf("%w: data class %d", ErrInvalidInput, class)
	}
	return nil
}
