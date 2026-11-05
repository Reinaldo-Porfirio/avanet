package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Setting keys. Values are validated by their owners before being written.
const (
	SettingProfile = "profile"
)

// Setting returns a stored value and whether it exists.
func (db *DB) Setting(ctx context.Context, key string) (value string, found bool, err error) {
	err = db.sql.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("read setting %q: %w", key, err)
	}
	return value, true, nil
}

// SetSetting stores a value, replacing any previous one.
func (db *DB) SetSetting(ctx context.Context, key, value string, at time.Time) error {
	if key == "" {
		return fmt.Errorf("%w: empty setting key", ErrInvalidInput)
	}
	const upsert = `
		INSERT INTO settings (key, value, updated_at_ms) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at_ms = excluded.updated_at_ms`
	if _, err := db.sql.ExecContext(ctx, upsert, key, value, at.UnixMilli()); err != nil {
		return fmt.Errorf("write setting %q: %w", key, err)
	}
	return nil
}
