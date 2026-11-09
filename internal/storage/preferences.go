package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// Setting keys of the preferences chosen in the panel.
const (
	settingFixedDailyMB  = "data_fixed_daily_mb"
	settingMobileEnabled = "data_mobile_enabled"
	settingMobileDailyMB = "data_mobile_daily_mb"
	settingPlanDownload  = "plan_download_mbps"
	settingPlanUpload    = "plan_upload_mbps"
)

// preferenceParsers read each stored value into its field.
var preferenceParsers = map[string]func(p *domain.Preferences, value string) error{
	SettingProfile: func(p *domain.Preferences, value string) error {
		p.Profile = domain.ProfileID(value)
		return nil
	},
	settingFixedDailyMB: func(p *domain.Preferences, v string) (err error) {
		p.FixedDailyMB, err = strconv.ParseInt(v, 10, 64)
		return err
	},
	settingMobileDailyMB: func(p *domain.Preferences, v string) (err error) {
		p.MobileDailyMB, err = strconv.ParseInt(v, 10, 64)
		return err
	},
	settingMobileEnabled: func(p *domain.Preferences, v string) (err error) {
		p.MobileEnabled, err = strconv.ParseBool(v)
		return err
	},
	settingPlanDownload: func(p *domain.Preferences, v string) (err error) {
		p.PlanDownloadMbps, err = strconv.ParseFloat(v, 64)
		return err
	},
	settingPlanUpload: func(p *domain.Preferences, v string) (err error) {
		p.PlanUploadMbps, err = strconv.ParseFloat(v, 64)
		return err
	},
}

// Preferences returns the stored preferences, with the default for anything
// never chosen. A value that cannot be used also falls back to its default,
// so a damaged setting cannot stop the monitoring; its key is returned in
// invalid for the caller to log.
func (db *DB) Preferences(ctx context.Context) (prefs domain.Preferences, invalid []string, err error) {
	rows, err := db.sql.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return domain.Preferences{}, nil, fmt.Errorf("read preferences: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("read preferences: %w", closeErr))
		}
	}()

	prefs = domain.DefaultPreferences()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return domain.Preferences{}, nil, fmt.Errorf("read preferences: %w", err)
		}
		parse, known := preferenceParsers[key]
		if !known {
			continue
		}
		candidate := prefs
		if parse(&candidate, value) != nil || candidate.Validate() != nil {
			invalid = append(invalid, key)
			continue
		}
		prefs = candidate
	}
	if err := rows.Err(); err != nil {
		return domain.Preferences{}, nil, fmt.Errorf("read preferences: %w", err)
	}
	return prefs, invalid, nil
}

// SavePreferences validates and stores every preference at once.
func (db *DB) SavePreferences(ctx context.Context, p domain.Preferences, at time.Time) (err error) {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	values := [...][2]string{
		{SettingProfile, string(p.Profile)},
		{settingFixedDailyMB, strconv.FormatInt(p.FixedDailyMB, 10)},
		{settingMobileEnabled, strconv.FormatBool(p.MobileEnabled)},
		{settingMobileDailyMB, strconv.FormatInt(p.MobileDailyMB, 10)},
		{settingPlanDownload, strconv.FormatFloat(p.PlanDownloadMbps, 'f', -1, 64)},
		{settingPlanUpload, strconv.FormatFloat(p.PlanUploadMbps, 'f', -1, 64)},
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save preferences: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	for _, kv := range values {
		if _, err := tx.ExecContext(ctx, upsertSetting, kv[0], kv[1], at.UnixMilli()); err != nil {
			return fmt.Errorf("save preferences: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save preferences: %w", err)
	}
	return nil
}

// DeleteHistory erases what was measured, keeping the preferences. Three
// things survive because the running agent depends on them: its current
// session (restarted from its last heartbeat), an outage still in progress
// with its network, and today's data usage, without which erasing the
// history would also reset the daily data limit.
func (db *DB) DeleteHistory(ctx context.Context, today domain.Day) (err error) {
	if !today.Valid() {
		return fmt.Errorf("%w: day %q", ErrInvalidInput, today)
	}
	statements := [...]struct {
		sql  string
		args []any
	}{
		{sql: "DELETE FROM latency_samples"},
		{sql: "DELETE FROM wifi_snapshots"},
		{sql: "DELETE FROM speed_tests"},
		{sql: "DELETE FROM daily_reports"},
		{sql: "DELETE FROM outages WHERE ended_ms IS NOT NULL"},
		{sql: "DELETE FROM agent_sessions WHERE id <> (SELECT MAX(id) FROM agent_sessions)"},
		{sql: "UPDATE agent_sessions SET started_ms = last_ms"},
		{sql: "DELETE FROM networks WHERE id NOT IN (SELECT network_id FROM outages WHERE network_id IS NOT NULL)"},
		{sql: "DELETE FROM data_usage WHERE day <> ?", args: []any{string(today)}},
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete history: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	for _, s := range statements {
		if _, err := tx.ExecContext(ctx, s.sql, s.args...); err != nil {
			return fmt.Errorf("delete history: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete history: %w", err)
	}
	return nil
}

// Compact removes deleted data from the database file itself: SQLite only
// marks the space as free, so names of Wi-Fi networks would remain readable
// in the file until something overwrote them.
func (db *DB) Compact(ctx context.Context) error {
	for _, statement := range []string{"VACUUM", "PRAGMA wal_checkpoint(TRUNCATE)"} {
		if _, err := db.sql.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("compact database: %w", err)
		}
	}
	return nil
}
