package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// StoredReport is a daily report as persisted. JSON is opaque here.
type StoredReport struct {
	Day         domain.Day
	Profile     domain.ProfileID
	Version     int
	JSON        string
	GeneratedAt time.Time
	NotifiedAt  time.Time // zero if the user was not notified yet
}

// SaveDailyReport stores a report, replacing any previous version of that day
// while keeping whether the user was already notified about it.
func (db *DB) SaveDailyReport(ctx context.Context, r *StoredReport) error {
	if !r.Day.Valid() {
		return fmt.Errorf("%w: day %q", ErrInvalidInput, r.Day)
	}
	const upsert = `
		INSERT INTO daily_reports (day, profile, version, summary_json, generated_at_ms) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (day) DO UPDATE SET
			profile = excluded.profile, version = excluded.version,
			summary_json = excluded.summary_json, generated_at_ms = excluded.generated_at_ms`
	if _, err := db.sql.ExecContext(ctx, upsert, string(r.Day), string(r.Profile), r.Version, r.JSON, r.GeneratedAt.UnixMilli()); err != nil {
		return fmt.Errorf("save daily report: %w", err)
	}
	return nil
}

// DailyReport returns the stored report of a day, if any.
func (db *DB) DailyReport(ctx context.Context, day domain.Day) (StoredReport, bool, error) {
	const query = `
		SELECT profile, version, summary_json, generated_at_ms, notified_at_ms
		FROM daily_reports WHERE day = ?`
	var (
		r         = StoredReport{Day: day}
		profile   string
		generated int64
		notified  sql.NullInt64
	)
	err := db.sql.QueryRowContext(ctx, query, string(day)).Scan(&profile, &r.Version, &r.JSON, &generated, &notified)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return StoredReport{}, false, nil
	case err != nil:
		return StoredReport{}, false, fmt.Errorf("read daily report: %w", err)
	}
	r.Profile = domain.ProfileID(profile)
	r.GeneratedAt = time.UnixMilli(generated)
	if notified.Valid {
		r.NotifiedAt = time.UnixMilli(notified.Int64)
	}
	return r, true, nil
}

// MarkReportNotified records that the user was told about a day's report.
func (db *DB) MarkReportNotified(ctx context.Context, day domain.Day, at time.Time) error {
	res, err := db.sql.ExecContext(ctx, "UPDATE daily_reports SET notified_at_ms = ? WHERE day = ?", at.UnixMilli(), string(day))
	if err != nil {
		return fmt.Errorf("mark report notified: %w", err)
	}
	return expectOneRow(res, "mark report notified")
}

// HasMeasurements reports whether any latency round was recorded in [from, to).
func (db *DB) HasMeasurements(ctx context.Context, from, to time.Time) (bool, error) {
	var exists bool
	err := db.sql.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM latency_samples WHERE at_ms >= ? AND at_ms < ?)",
		from.UnixMilli(), to.UnixMilli()).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check measurements: %w", err)
	}
	return exists, nil
}

// PruneRawData deletes high-volume samples older than before. Reports,
// speed tests and outages are small and stay.
func (db *DB) PruneRawData(ctx context.Context, before time.Time) (err error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("prune raw data: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	for _, stmt := range []string{
		"DELETE FROM latency_samples WHERE at_ms < ?",
		"DELETE FROM wifi_snapshots WHERE at_ms < ?",
	} {
		if _, err := tx.ExecContext(ctx, stmt, before.UnixMilli()); err != nil {
			return fmt.Errorf("prune raw data: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("prune raw data: %w", err)
	}
	return nil
}
