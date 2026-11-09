package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// LoadDay reads everything recorded in [from, to), one calendar day. Outages
// and sessions overlapping the range are returned whole; clamping them is up
// to the caller.
func (db *DB) LoadDay(ctx context.Context, day domain.Day, from, to time.Time) (domain.DayData, error) {
	var d domain.DayData
	loaders := []func() error{
		func() (err error) { d.Latency, err = db.loadLatency(ctx, from, to); return err },
		func() (err error) { d.Speed, err = db.loadSpeedTests(ctx, from, to); return err },
		func() (err error) { d.WiFi, err = db.loadWiFi(ctx, from, to); return err },
		func() (err error) { d.Outages, err = db.loadOutages(ctx, from, to); return err },
		func() (err error) { d.Sessions, err = db.loadSessions(ctx, from, to); return err },
		func() (err error) { d.Network, d.HasNetwork, err = db.primaryNetwork(ctx, from, to); return err },
		func() (err error) { d.DataUsed, err = db.totalDataUsage(ctx, day); return err },
	}
	for _, load := range loaders {
		if err := load(); err != nil {
			return domain.DayData{}, fmt.Errorf("load day %s: %w", day, err)
		}
	}
	return d, nil
}

// scanAll runs a query and appends one scanned value per row.
func scanAll[T any](ctx context.Context, db *sql.DB, scan func(*sql.Rows) (T, error), query string, args ...any) (out []T, err error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (db *DB) loadLatency(ctx context.Context, from, to time.Time) ([]domain.LatencyRecord, error) {
	const query = `
		SELECT at_ms, target, sent, received,
		       COALESCE(median_us, 0), COALESCE(jitter_us, 0), COALESCE(min_us, 0), COALESCE(max_us, 0)
		FROM latency_samples WHERE at_ms >= ? AND at_ms < ? ORDER BY at_ms, id`
	return scanAll(ctx, db.sql, func(rows *sql.Rows) (domain.LatencyRecord, error) {
		var (
			r                                domain.LatencyRecord
			at, median, jitter, minUS, maxUS int64
		)
		err := rows.Scan(&at, &r.Target, &r.Stats.Sent, &r.Stats.Received, &median, &jitter, &minUS, &maxUS)
		r.At = time.UnixMilli(at)
		r.Stats.Median, r.Stats.Jitter = microseconds(median), microseconds(jitter)
		r.Stats.Min, r.Stats.Max = microseconds(minUS), microseconds(maxUS)
		return r, err
	}, query, from.UnixMilli(), to.UnixMilli())
}

func (db *DB) loadSpeedTests(ctx context.Context, from, to time.Time) ([]domain.SpeedResult, error) {
	const query = `
		SELECT at_ms, server, idle_latency_us, download_mbps, upload_mbps, bytes_down, bytes_up,
		       download_capped, upload_capped, local_traffic
		FROM speed_tests WHERE at_ms >= ? AND at_ms < ? ORDER BY at_ms, id`
	return scanAll(ctx, db.sql, func(rows *sql.Rows) (domain.SpeedResult, error) {
		var (
			r        domain.SpeedResult
			at, idle int64
		)
		err := rows.Scan(&at, &r.Server, &idle, &r.DownloadMbps, &r.UploadMbps, &r.BytesDown, &r.BytesUp,
			&r.DownloadCapped, &r.UploadCapped, &r.LocalTraffic)
		r.Start, r.IdleLatency = time.UnixMilli(at), microseconds(idle)
		return r, err
	}, query, from.UnixMilli(), to.UnixMilli())
}

func (db *DB) loadWiFi(ctx context.Context, from, to time.Time) ([]domain.WiFiRecord, error) {
	const query = `SELECT at_ms, signal_percent FROM wifi_snapshots WHERE at_ms >= ? AND at_ms < ? ORDER BY at_ms, id`
	return scanAll(ctx, db.sql, func(rows *sql.Rows) (domain.WiFiRecord, error) {
		var (
			r  domain.WiFiRecord
			at int64
		)
		err := rows.Scan(&at, &r.SignalPercent)
		r.At = time.UnixMilli(at)
		return r, err
	}, query, from.UnixMilli(), to.UnixMilli())
}

func (db *DB) loadOutages(ctx context.Context, from, to time.Time) ([]domain.Outage, error) {
	const query = `
		SELECT started_ms, ended_ms FROM outages
		WHERE started_ms < ? AND (ended_ms IS NULL OR ended_ms > ?) ORDER BY started_ms, id`
	return scanAll(ctx, db.sql, func(rows *sql.Rows) (domain.Outage, error) {
		var (
			o     domain.Outage
			start int64
			end   sql.NullInt64
		)
		err := rows.Scan(&start, &end)
		o.Start = time.UnixMilli(start)
		if end.Valid {
			o.End = time.UnixMilli(end.Int64)
		}
		return o, err
	}, query, to.UnixMilli(), from.UnixMilli())
}

func (db *DB) loadSessions(ctx context.Context, from, to time.Time) ([]domain.TimeSpan, error) {
	const query = `
		SELECT started_ms, last_ms FROM agent_sessions
		WHERE started_ms < ? AND last_ms > ? ORDER BY started_ms, id`
	return scanAll(ctx, db.sql, func(rows *sql.Rows) (domain.TimeSpan, error) {
		var start, end int64
		err := rows.Scan(&start, &end)
		return domain.TimeSpan{Start: time.UnixMilli(start), End: time.UnixMilli(end)}, err
	}, query, to.UnixMilli(), from.UnixMilli())
}

func (db *DB) primaryNetwork(ctx context.Context, from, to time.Time) (domain.NetworkRef, bool, error) {
	const query = `
		SELECT n.kind, n.ssid FROM latency_samples l JOIN networks n ON n.id = l.network_id
		WHERE l.at_ms >= ? AND l.at_ms < ?
		GROUP BY n.id ORDER BY COUNT(*) DESC, n.id LIMIT 1`
	var kind, ssid string
	err := db.sql.QueryRowContext(ctx, query, from.UnixMilli(), to.UnixMilli()).Scan(&kind, &ssid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return domain.NetworkRef{}, false, nil
	case err != nil:
		return domain.NetworkRef{}, false, err
	}
	return domain.NetworkRef{Kind: connectionKindFrom(kind), SSID: ssid}, true, nil
}

func connectionKindFrom(kind string) domain.ConnectionKind {
	for _, k := range []domain.ConnectionKind{domain.ConnectionEthernet, domain.ConnectionWiFi, domain.ConnectionMobile} {
		if k.String() == kind {
			return k
		}
	}
	return domain.ConnectionOther
}

func (db *DB) totalDataUsage(ctx context.Context, day domain.Day) (int64, error) {
	var total int64
	err := db.sql.QueryRowContext(ctx, "SELECT COALESCE(SUM(bytes), 0) FROM data_usage WHERE day = ?", string(day)).Scan(&total)
	return total, err
}

func microseconds(v int64) time.Duration { return time.Duration(v) * time.Microsecond }
