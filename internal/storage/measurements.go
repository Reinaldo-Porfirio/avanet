package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// NetworkID identifies a row of the networks table; zero means "unknown".
type NetworkID int64

// SpeedTrigger records why a speed test ran.
type SpeedTrigger string

// Speed test triggers.
const (
	TriggerScheduled SpeedTrigger = "scheduled"
	TriggerAdaptive  SpeedTrigger = "adaptive"
	TriggerManual    SpeedTrigger = "manual"
)

func (t SpeedTrigger) valid() bool {
	return t == TriggerScheduled || t == TriggerAdaptive || t == TriggerManual
}

// EnsureNetwork returns the id of a network, creating it on first sight.
func (db *DB) EnsureNetwork(ctx context.Context, n domain.Network, ssid string, at time.Time) (NetworkID, error) {
	kind := n.Kind.String()
	const upsert = `
		INSERT INTO networks (kind, interface_name, ssid, first_seen_ms) VALUES (?, ?, ?, ?)
		ON CONFLICT (kind, interface_name, ssid) DO UPDATE SET kind = excluded.kind
		RETURNING id`
	var id int64
	if err := db.sql.QueryRowContext(ctx, upsert, kind, n.InterfaceName, ssid, at.UnixMilli()).Scan(&id); err != nil {
		return 0, fmt.Errorf("ensure network: %w", err)
	}
	return NetworkID(id), nil
}

// SaveLatency stores one latency round (all targets) atomically.
func (db *DB) SaveLatency(ctx context.Context, at time.Time, network NetworkID, samples []domain.TargetLatency) (err error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save latency: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	const insert = `
		INSERT INTO latency_samples (at_ms, network_id, target, sent, received, median_us, jitter_us, min_us, max_us)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, s := range samples {
		st := s.Stats
		_, err := tx.ExecContext(ctx, insert, at.UnixMilli(), nullableID(network), s.Target, st.Sent, st.Received,
			micros(st, st.Median), micros(st, st.Jitter), micros(st, st.Min), micros(st, st.Max))
		if err != nil {
			return fmt.Errorf("save latency for %s: %w", s.Target, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save latency: %w", err)
	}
	return nil
}

// SaveWiFi stores the state of the Wi-Fi link.
func (db *DB) SaveWiFi(ctx context.Context, at time.Time, network NetworkID, w domain.WiFi) error {
	const insert = `
		INSERT INTO wifi_snapshots (at_ms, network_id, signal_percent, rssi_dbm, channel, band, standard, rx_rate_mbps)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := db.sql.ExecContext(ctx, insert, at.UnixMilli(), nullableID(network), w.SignalPercent, w.RSSIdBm,
		w.Channel, w.Band.String(), w.Standard, w.RxRateMbps); err != nil {
		return fmt.Errorf("save wifi: %w", err)
	}
	return nil
}

// SaveSpeedTest stores a completed speed test.
func (db *DB) SaveSpeedTest(ctx context.Context, network NetworkID, trigger SpeedTrigger, r *domain.SpeedResult) error {
	if !trigger.valid() {
		return fmt.Errorf("%w: speed trigger %q", ErrInvalidInput, trigger)
	}
	const insert = `
		INSERT INTO speed_tests (at_ms, network_id, trigger, server, idle_latency_us, download_mbps, upload_mbps,
			bytes_down, bytes_up, download_capped, upload_capped, local_traffic)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := db.sql.ExecContext(ctx, insert, r.Start.UnixMilli(), nullableID(network), string(trigger), r.Server,
		r.IdleLatency.Microseconds(), r.DownloadMbps, r.UploadMbps, r.BytesDown, r.BytesUp,
		r.DownloadCapped, r.UploadCapped, r.LocalTraffic); err != nil {
		return fmt.Errorf("save speed test: %w", err)
	}
	return nil
}

// LastSpeedTest returns when the latest automatic (scheduled or adaptive)
// speed test ran, so the schedule survives restarts.
func (db *DB) LastSpeedTest(ctx context.Context) (time.Time, bool, error) {
	var ms sql.NullInt64
	err := db.sql.QueryRowContext(ctx,
		"SELECT MAX(at_ms) FROM speed_tests WHERE trigger IN ('scheduled', 'adaptive')").Scan(&ms)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("last speed test: %w", err)
	}
	if !ms.Valid {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(ms.Int64), true, nil
}

// StartOutage records the beginning of an outage and returns its id.
func (db *DB) StartOutage(ctx context.Context, network NetworkID, start time.Time) (int64, error) {
	var id int64
	err := db.sql.QueryRowContext(ctx,
		"INSERT INTO outages (network_id, started_ms) VALUES (?, ?) RETURNING id", nullableID(network), start.UnixMilli()).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("start outage: %w", err)
	}
	return id, nil
}

// EndOutage closes an outage.
func (db *DB) EndOutage(ctx context.Context, id int64, end time.Time) error {
	res, err := db.sql.ExecContext(ctx, "UPDATE outages SET ended_ms = ? WHERE id = ? AND ended_ms IS NULL", end.UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("end outage: %w", err)
	}
	return expectOneRow(res, "end outage")
}

// CloseOpenOutages ends outages left open by a previous run (the agent was
// stopped mid-outage) at the given time, the last moment it was observed.
func (db *DB) CloseOpenOutages(ctx context.Context, at time.Time) error {
	_, err := db.sql.ExecContext(ctx,
		"UPDATE outages SET ended_ms = MAX(started_ms, ?) WHERE ended_ms IS NULL", at.UnixMilli())
	if err != nil {
		return fmt.Errorf("close open outages: %w", err)
	}
	return nil
}

// StartSession opens a new agent session (a continuous running period).
func (db *DB) StartSession(ctx context.Context, at time.Time) (int64, error) {
	var id int64
	ms := at.UnixMilli()
	if err := db.sql.QueryRowContext(ctx,
		"INSERT INTO agent_sessions (started_ms, last_ms) VALUES (?, ?) RETURNING id", ms, ms).Scan(&id); err != nil {
		return 0, fmt.Errorf("start session: %w", err)
	}
	return id, nil
}

// Beat extends a session up to at.
func (db *DB) Beat(ctx context.Context, session int64, at time.Time) error {
	res, err := db.sql.ExecContext(ctx,
		"UPDATE agent_sessions SET last_ms = MAX(last_ms, ?) WHERE id = ?", at.UnixMilli(), session)
	if err != nil {
		return fmt.Errorf("session heartbeat: %w", err)
	}
	return expectOneRow(res, "session heartbeat")
}

// LastBeat returns the end of the most recent session, if any.
func (db *DB) LastBeat(ctx context.Context) (time.Time, bool, error) {
	var ms sql.NullInt64
	if err := db.sql.QueryRowContext(ctx, "SELECT MAX(last_ms) FROM agent_sessions").Scan(&ms); err != nil {
		return time.Time{}, false, fmt.Errorf("last heartbeat: %w", err)
	}
	if !ms.Valid {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(ms.Int64), true, nil
}

// Coverage returns how long the agent ran within [from, to).
func (db *DB) Coverage(ctx context.Context, from, to time.Time) (time.Duration, error) {
	const query = `
		SELECT COALESCE(SUM(MIN(last_ms, :to) - MAX(started_ms, :from)), 0)
		FROM agent_sessions
		WHERE last_ms > :from AND started_ms < :to`
	var ms int64
	if err := db.sql.QueryRowContext(ctx, query,
		sql.Named("from", from.UnixMilli()), sql.Named("to", to.UnixMilli())).Scan(&ms); err != nil {
		return 0, fmt.Errorf("coverage: %w", err)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func nullableID(id NetworkID) any {
	if id == 0 {
		return nil
	}
	return int64(id)
}

// micros stores a duration, or NULL when the target never answered.
func micros(s domain.LatencyStats, d time.Duration) any {
	if !s.Reachable() {
		return nil
	}
	return d.Microseconds()
}

func expectOneRow(res sql.Result, op string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if n != 1 {
		return fmt.Errorf("%w: %s affected %d rows", ErrInvalidInput, op, n)
	}
	return nil
}
