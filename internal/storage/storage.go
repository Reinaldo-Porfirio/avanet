// Package storage owns the SQLite database: opening it with safe settings,
// keeping its schema up to date and (in later phases) the repositories.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver (no C compiler needed)
)

// ErrCorrupt is returned by Open when SQLite's integrity check fails.
var ErrCorrupt = errors.New("database integrity check failed")

// maxOpenConns allows a few concurrent readers (WAL mode) while writes are
// serialized by SQLite itself; the workload is a handful of queries per minute.
const maxOpenConns = 4

// DB is the application database.
type DB struct {
	sql *sql.DB
}

// Open opens (creating if needed) the database at path, verifies its
// integrity and applies pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn, err := buildDSN(path)
	if err != nil {
		return nil, err
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)

	db := &DB{sql: sqlDB}
	if err := db.init(ctx); err != nil {
		return nil, errors.Join(err, sqlDB.Close())
	}
	return db, nil
}

// Close releases the database. It is safe to call once at shutdown.
func (db *DB) Close() error {
	if err := db.sql.Close(); err != nil {
		return fmt.Errorf("close sqlite: %w", err)
	}
	return nil
}

func (db *DB) init(ctx context.Context) error {
	if err := db.sql.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to sqlite: %w", err)
	}
	if err := db.checkIntegrity(ctx); err != nil {
		return err
	}

	migrations, err := loadMigrations(migrationFiles, migrationsDir)
	if err != nil {
		return err
	}
	return migrate(ctx, db.sql, migrations)
}

// checkIntegrity runs PRAGMA quick_check, which is fast enough at startup for
// a database of this size and catches corruption (e.g. after a power loss)
// before new data is written on top of it.
func (db *DB) checkIntegrity(ctx context.Context) error {
	var result string
	if err := db.sql.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return fmt.Errorf("run integrity check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("%w: %s", ErrCorrupt, result)
	}
	return nil
}

// buildDSN applies, to every pooled connection:
//   - WAL journal: readers never block the writer and the file survives crashes;
//   - synchronous=NORMAL: safe with WAL and much cheaper than FULL;
//   - foreign_keys: SQLite leaves them off by default;
//   - busy_timeout: wait for a lock instead of failing immediately;
//   - _txlock=immediate: transactions take the write lock up front, avoiding
//     deadlocks when two transactions try to upgrade from read to write.
func buildDSN(path string) (string, error) {
	if path == "" {
		return "", errors.New("database path is required")
	}
	// A '?' would be parsed as the start of the parameters below. Windows paths
	// cannot contain it, but refuse it explicitly rather than misbehave.
	if strings.ContainsRune(path, '?') {
		return "", fmt.Errorf("database path must not contain '?': %q", path)
	}

	params := url.Values{}
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "synchronous(NORMAL)")
	params.Add("_pragma", "foreign_keys(ON)")
	params.Add("_pragma", "busy_timeout(5000)")
	params.Set("_txlock", "immediate")
	return path + "?" + params.Encode(), nil
}
