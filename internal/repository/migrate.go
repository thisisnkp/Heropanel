package repository

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

// migrationsFS embeds the dialect-specific SQL migrations. Each dialect has its
// own directory because DDL is not portable across MariaDB and SQLite.
//
//go:embed migrations/mysql/*.sql migrations/sqlite/*.sql
var migrationsFS embed.FS

type migration struct {
	version int
	name    string
	up      string
}

// Migrate applies all pending "up" migrations for db's dialect and returns how
// many were applied. It is idempotent: already-applied versions are skipped.
//
// Each migration runs inside a transaction. Note: on MariaDB/MySQL, DDL
// statements auto-commit, so a mid-migration failure there can leave partial
// state — keep each migration's DDL cohesive. On SQLite, DDL is transactional.
func Migrate(ctx context.Context, db *DB) (int, error) {
	if err := ensureSchemaMigrations(ctx, db); err != nil {
		return 0, err
	}
	current, err := currentVersion(ctx, db)
	if err != nil {
		return 0, err
	}
	migs, err := loadMigrations(db.Dialect)
	if err != nil {
		return 0, err
	}

	applied := 0
	for _, m := range migs {
		if m.version <= current {
			continue
		}
		if err := applyOne(ctx, db, m); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func applyOne(ctx context.Context, db *DB, m migration) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repository: begin tx for migration %d: %w", m.version, err)
	}
	for _, stmt := range splitStatements(m.up) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("repository: migration %04d_%s failed: %w", m.version, m.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, time.Now().UTC().Format("2006-01-02 15:04:05"),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("repository: record migration %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repository: commit migration %d: %w", m.version, err)
	}
	return nil
}

func ensureSchemaMigrations(ctx context.Context, db *DB) error {
	var ddl string
	switch db.Dialect {
	case DialectMySQL:
		ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT NOT NULL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			applied_at DATETIME(6) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
	default:
		ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER NOT NULL PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`
	}
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("repository: ensure schema_migrations: %w", err)
	}
	return nil
}

func currentVersion(ctx context.Context, db *DB) (int, error) {
	var v int
	if err := db.GetContext(ctx, &v, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`); err != nil {
		return 0, fmt.Errorf("repository: read current version: %w", err)
	}
	return v, nil
}

func loadMigrations(dialect Dialect) ([]migration, error) {
	dir := "migrations/" + string(dialect)
	entries, err := fs.ReadDir(migrationsFS, dir)
	if err != nil {
		return nil, fmt.Errorf("repository: read migrations dir: %w", err)
	}

	var migs []migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		version, short, err := parseMigrationName(name)
		if err != nil {
			return nil, err
		}
		content, err := migrationsFS.ReadFile(dir + "/" + name)
		if err != nil {
			return nil, fmt.Errorf("repository: read %s: %w", name, err)
		}
		migs = append(migs, migration{version: version, name: short, up: string(content)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs, nil
}

// parseMigrationName parses "0001_init.up.sql" into (1, "init").
func parseMigrationName(filename string) (int, string, error) {
	base := strings.TrimSuffix(filename, ".up.sql")
	idx := strings.IndexByte(base, '_')
	if idx <= 0 {
		return 0, "", fmt.Errorf("repository: bad migration filename %q", filename)
	}
	version, err := strconv.Atoi(base[:idx])
	if err != nil {
		return 0, "", fmt.Errorf("repository: bad migration version in %q: %w", filename, err)
	}
	return version, base[idx+1:], nil
}

// splitStatements splits a SQL script into individual statements, stripping
// line comments and blank statements. Migration files must not contain ';'
// inside string literals or compound statements (no triggers/procedures).
func splitStatements(script string) []string {
	var b strings.Builder
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") || trimmed == "" {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	var out []string
	for _, part := range strings.Split(b.String(), ";") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}
