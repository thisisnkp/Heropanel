// Package repository is HeroPanel's data-access layer: it opens the
// control-plane datastore, runs migrations, and provides repositories (per
// bounded context) implemented with sqlx and explicit SQL (ADR-0006).
//
// Two dialects are supported: MariaDB/MySQL (production) and SQLite via the
// pure-Go modernc driver (minimal installs and cgo-free local tests, ADR-0004).
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	_ "github.com/go-sql-driver/mysql" // registers "mysql"
	_ "modernc.org/sqlite"             // registers "sqlite" (pure Go, no cgo)

	"github.com/thisisnkp/heropanel/internal/config"
)

// Dialect identifies the SQL flavor in use, selecting the matching migrations
// and DDL.
type Dialect string

const (
	DialectMySQL  Dialect = "mysql"
	DialectSQLite Dialect = "sqlite"
)

// DB wraps a sqlx handle plus the active dialect.
type DB struct {
	*sqlx.DB
	Dialect Dialect
}

// Configured reports whether a datastore should be opened for this config. We
// open only when a DSN is provided, which keeps hpd bootable with no database
// during early bring-up.
func Configured(cfg config.Database) bool {
	return strings.TrimSpace(cfg.DSN) != ""
}

// Open connects to the datastore described by cfg, configures pooling, and
// verifies connectivity with a ping.
func Open(cfg config.Database) (*DB, error) {
	driver, dialect, err := resolveDriver(cfg.Driver)
	if err != nil {
		return nil, err
	}

	sdb, err := sqlx.Open(driver, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("repository: open %s: %w", dialect, err)
	}

	applyPool(sdb, cfg, dialect)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sdb.PingContext(ctx); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("repository: ping %s: %w", dialect, err)
	}

	return &DB{DB: sdb, Dialect: dialect}, nil
}

func resolveDriver(driver string) (sqlDriver string, dialect Dialect, err error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "mariadb", "mysql":
		return "mysql", DialectMySQL, nil
	case "sqlite", "sqlite3":
		return "sqlite", DialectSQLite, nil
	default:
		return "", "", fmt.Errorf("repository: unsupported driver %q", driver)
	}
}

func applyPool(db *sqlx.DB, cfg config.Database, dialect Dialect) {
	// SQLite serializes writes; a single connection avoids "database is locked"
	// and keeps an in-memory DB consistent across the pool.
	if dialect == DialectSQLite {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		return
	}
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if d := cfg.ConnMaxLifetime.D(); d > 0 {
		db.SetConnMaxLifetime(d)
	}
}

// Health pings the database; used by the readiness probe.
func (db *DB) Health(ctx context.Context) error {
	return db.PingContext(ctx)
}

// WithTx runs fn inside a transaction, committing on success and rolling back on
// error or panic.
func (db *DB) WithTx(ctx context.Context, fn func(tx *sqlx.Tx) error) (err error) {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repository: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
			return
		}
		err = tx.Commit()
	}()
	return fn(tx)
}

// isNoRows reports whether err is a "no rows" error.
func isNoRows(err error) bool { return err == sql.ErrNoRows }
