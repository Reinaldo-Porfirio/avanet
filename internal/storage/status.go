package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
)

// LatestRound returns the most recent latency round, if any.
func (db *DB) LatestRound(ctx context.Context) (at time.Time, samples []domain.TargetLatency, found bool, err error) {
	const query = `
		SELECT at_ms, target, sent, received,
		       COALESCE(median_us, 0), COALESCE(jitter_us, 0), COALESCE(min_us, 0), COALESCE(max_us, 0)
		FROM latency_samples
		WHERE at_ms = (SELECT MAX(at_ms) FROM latency_samples)
		ORDER BY id`
	rows, err := db.sql.QueryContext(ctx, query)
	if err != nil {
		return time.Time{}, nil, false, fmt.Errorf("latest round: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("latest round: %w", closeErr))
		}
	}()

	var atMS int64
	for rows.Next() {
		var (
			s                            domain.TargetLatency
			median, jitter, minUS, maxUS int64
		)
		if err := rows.Scan(&atMS, &s.Target, &s.Stats.Sent, &s.Stats.Received, &median, &jitter, &minUS, &maxUS); err != nil {
			return time.Time{}, nil, false, fmt.Errorf("latest round: %w", err)
		}
		s.Stats.Median = time.Duration(median) * time.Microsecond
		s.Stats.Jitter = time.Duration(jitter) * time.Microsecond
		s.Stats.Min = time.Duration(minUS) * time.Microsecond
		s.Stats.Max = time.Duration(maxUS) * time.Microsecond
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, nil, false, fmt.Errorf("latest round: %w", err)
	}
	if len(samples) == 0 {
		return time.Time{}, nil, false, nil
	}
	return time.UnixMilli(atMS), samples, true, nil
}

// LatestSpeedTest returns the most recent speed test of any trigger.
func (db *DB) LatestSpeedTest(ctx context.Context) (domain.SpeedResult, SpeedTrigger, bool, error) {
	const query = `
		SELECT at_ms, trigger, server, idle_latency_us, download_mbps, upload_mbps, bytes_down, bytes_up,
		       download_capped, upload_capped, local_traffic
		FROM speed_tests ORDER BY at_ms DESC, id DESC LIMIT 1`
	var (
		r            domain.SpeedResult
		atMS, idleUS int64
		trigger      string
	)
	err := db.sql.QueryRowContext(ctx, query).Scan(&atMS, &trigger, &r.Server, &idleUS, &r.DownloadMbps, &r.UploadMbps,
		&r.BytesDown, &r.BytesUp, &r.DownloadCapped, &r.UploadCapped, &r.LocalTraffic)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return domain.SpeedResult{}, "", false, nil
	case err != nil:
		return domain.SpeedResult{}, "", false, fmt.Errorf("latest speed test: %w", err)
	}
	r.Start = time.UnixMilli(atMS)
	r.IdleLatency = time.Duration(idleUS) * time.Microsecond
	return r, SpeedTrigger(trigger), true, nil
}
