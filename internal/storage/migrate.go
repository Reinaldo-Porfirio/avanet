package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const migrationsDir = "migrations"

// Errors returned when the database and the binary disagree about the schema.
// Both stop the application instead of risking data loss.
var (
	// ErrMigrationModified means a migration that already ran was edited.
	// Migrations are append-only: fix mistakes with a new migration.
	ErrMigrationModified = errors.New("applied migration was modified")
	// ErrSchemaTooNew means the database was migrated by a newer AvaNet.
	ErrSchemaTooNew = errors.New("database schema is newer than this version of AvaNet")
)

// migrationName is "<4-digit version>_<snake_case_description>.sql".
var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

type migration struct {
	version  int
	name     string
	sql      string
	checksum string
}

// loadMigrations reads and validates the migration files: well-formed names,
// versions contiguous from 1 and no duplicates. A broken set fails at startup
// (and in tests), never halfway through migrating a user's database.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		match := migrationName.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil {
			return nil, fmt.Errorf("unexpected file in migrations: %q", entry.Name())
		}

		version, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("parse migration version %q: %w", entry.Name(), err)
		}
		content, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}

		// Normalize line endings so a Git checkout with CRLF does not look like
		// an edited migration.
		text := strings.ReplaceAll(string(content), "\r\n", "\n")
		sum := sha256.Sum256([]byte(text))
		migrations = append(migrations, migration{
			version:  version,
			name:     match[2],
			sql:      text,
			checksum: hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	for i, m := range migrations {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration versions must be contiguous from 1: expected %04d, found %04d_%s", i+1, m.version, m.name)
		}
	}
	return migrations, nil
}

// migrate verifies the migrations already applied and applies the pending
// ones, each in its own transaction together with its bookkeeping row.
func migrate(ctx context.Context, db *sql.DB, migrations []migration) error {
	const createTable = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version       INTEGER PRIMARY KEY,
			name          TEXT    NOT NULL,
			checksum      TEXT    NOT NULL,
			applied_at_ms INTEGER NOT NULL
		) STRICT`
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedChecksums(ctx, db)
	if err != nil {
		return err
	}
	if err := verifyApplied(applied, migrations); err != nil {
		return err
	}

	for _, m := range migrations {
		if _, done := applied[m.version]; done {
			continue
		}
		if err := apply(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

func appliedChecksums(ctx context.Context, db *sql.DB) (applied map[int]string, err error) {
	rows, err := db.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close schema_migrations rows: %w", closeErr))
		}
	}()

	applied = make(map[int]string)
	for rows.Next() {
		var (
			version  int
			checksum string
		)
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return applied, nil
}

func verifyApplied(applied map[int]string, migrations []migration) error {
	known := make(map[int]migration, len(migrations))
	for _, m := range migrations {
		known[m.version] = m
	}

	for version, checksum := range applied {
		m, ok := known[version]
		if !ok {
			return fmt.Errorf("%w: database has migration %04d, latest known is %04d", ErrSchemaTooNew, version, len(migrations))
		}
		if m.checksum != checksum {
			return fmt.Errorf("%w: %04d_%s", ErrMigrationModified, m.version, m.name)
		}
	}
	return nil
}

func apply(ctx context.Context, db *sql.DB, m migration) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %04d: %w", m.version, err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	// Another process may have applied it after we read the list; the
	// immediate transaction holds the write lock, so this check is reliable.
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", m.version).Scan(&exists); err != nil {
		return fmt.Errorf("check migration %04d: %w", m.version, err)
	}
	if exists {
		return tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("apply migration %04d_%s: %w", m.version, m.name, err)
	}
	const record = "INSERT INTO schema_migrations (version, name, checksum, applied_at_ms) VALUES (?, ?, ?, ?)"
	if _, err := tx.ExecContext(ctx, record, m.version, m.name, m.checksum, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("record migration %04d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %04d: %w", m.version, err)
	}
	return nil
}
