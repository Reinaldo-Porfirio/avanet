package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

func TestOpenAppliesConnectionSettings(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tests := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"synchronous", "1"}, // NORMAL
		{"busy_timeout", "5000"},
	}
	for _, tt := range tests {
		var got string
		if err := db.sql.QueryRowContext(ctx, "PRAGMA "+tt.pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", tt.pragma, err)
		}
		if got != tt.want {
			t.Errorf("PRAGMA %s = %q, want %q", tt.pragma, got, tt.want)
		}
	}
}

func TestOpenAppliesAllMigrations(t *testing.T) {
	db := openTestDB(t)

	migrations, err := loadMigrations(migrationFiles, migrationsDir)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	var count int
	if err := db.sql.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(migrations) {
		t.Errorf("applied %d migrations, want %d", count, len(migrations))
	}
}

func TestReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	for i := range 3 {
		db, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i+1, err)
		}
	}
}

func TestMigrateRejectsModifiedMigration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if _, err := db.sql.ExecContext(ctx, "UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1"); err != nil {
		t.Fatal(err)
	}

	migrations, err := loadMigrations(migrationFiles, migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db.sql, migrations); !errors.Is(err, ErrMigrationModified) {
		t.Errorf("migrate error = %v, want ErrMigrationModified", err)
	}
}

func TestMigrateRejectsNewerSchema(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	const futureVersion = 9999
	if _, err := db.sql.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name, checksum, applied_at_ms) VALUES (?, 'future', 'x', 0)", futureVersion); err != nil {
		t.Fatal(err)
	}

	migrations, err := loadMigrations(migrationFiles, migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db.sql, migrations); !errors.Is(err, ErrSchemaTooNew) {
		t.Errorf("migrate error = %v, want ErrSchemaTooNew", err)
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	migrations, err := loadMigrations(migrationFiles, migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	broken := migration{
		version:  len(migrations) + 1,
		name:     "broken",
		sql:      "CREATE TABLE half_done (id INTEGER) STRICT; THIS IS NOT SQL;",
		checksum: "x",
	}

	if err := migrate(ctx, db.sql, append(migrations, broken)); err == nil {
		t.Fatal("migrate with broken migration succeeded, want error")
	}

	var tables int
	if err := db.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name = 'half_done'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Error("broken migration left a partial table behind")
	}
}

func TestLoadMigrationsValidation(t *testing.T) {
	file := func(sql string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(sql)} }

	tests := []struct {
		name    string
		files   fstest.MapFS
		wantErr string
	}{
		{
			name:  "valid set",
			files: fstest.MapFS{"m/0001_a.sql": file("SELECT 1;"), "m/0002_b.sql": file("SELECT 2;")},
		},
		{
			name:    "gap in versions",
			files:   fstest.MapFS{"m/0001_a.sql": file("SELECT 1;"), "m/0003_c.sql": file("SELECT 3;")},
			wantErr: "contiguous",
		},
		{
			name:    "duplicate version",
			files:   fstest.MapFS{"m/0001_a.sql": file("SELECT 1;"), "m/0001_b.sql": file("SELECT 1;")},
			wantErr: "contiguous",
		},
		{
			name:    "bad file name",
			files:   fstest.MapFS{"m/1_init.sql": file("SELECT 1;")},
			wantErr: "unexpected file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadMigrations(tt.files, "m")
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadMigrationsIgnoresLineEndings(t *testing.T) {
	lf := fstest.MapFS{"m/0001_a.sql": {Data: []byte("SELECT 1;\nSELECT 2;\n")}}
	crlf := fstest.MapFS{"m/0001_a.sql": {Data: []byte("SELECT 1;\r\nSELECT 2;\r\n")}}

	a, err := loadMigrations(lf, "m")
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadMigrations(crlf, "m")
	if err != nil {
		t.Fatal(err)
	}
	if a[0].checksum != b[0].checksum {
		t.Error("CRLF and LF versions of the same migration have different checksums")
	}
}

func TestBuildDSNRejectsInvalidPaths(t *testing.T) {
	for _, path := range []string{"", `C:\data\x.db?mode=ro`} {
		if _, err := buildDSN(path); err == nil {
			t.Errorf("buildDSN(%q) succeeded, want error", path)
		}
	}
}
