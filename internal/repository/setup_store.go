package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/thisisnkp/heropanel/internal/setup"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// SetupStore persists the panel's first-run setup state as a single row
// (id = 1). It is the concrete implementation of setup.Store.
type SetupStore struct {
	db *DB
}

// NewSetupStore constructs a SetupStore.
func NewSetupStore(db *DB) *SetupStore { return &SetupStore{db: db} }

// Get returns the setup state. A fresh install has no row yet, which is not an
// error: it returns a zero State (Completed=false), and that absence is exactly
// what gates the wizard on first run.
func (s *SetupStore) Get(ctx context.Context) (*setup.State, error) {
	var (
		webserver, dbEngine   string
		manageDNS, createMail int
		licenseKey            string
		completedAt           sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT webserver, db_engine, manage_dns, create_mail, license_key, completed_at
		   FROM panel_setup WHERE id = 1`).
		Scan(&webserver, &dbEngine, &manageDNS, &createMail, &licenseKey, &completedAt)
	if isNoRows(err) {
		return &setup.State{}, nil
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	st := &setup.State{
		Selection: setup.Selection{
			Webserver:  setup.Webserver(webserver),
			DBEngine:   setup.DBEngine(dbEngine),
			ManageDNS:  manageDNS != 0,
			CreateMail: createMail != 0,
			LicenseKey: licenseKey,
		},
		Completed: completedAt.Valid && completedAt.String != "",
	}
	if st.Completed {
		if t, perr := time.Parse(tsLayout, completedAt.String); perr == nil {
			ut := t.UTC()
			st.CompletedAt = &ut
		}
	}
	return st, nil
}

// Save upserts the single setup row with the operator's selection. A non-nil
// completedAt marks the wizard finished (and stamps when); nil clears it.
func (s *SetupStore) Save(ctx context.Context, sel setup.Selection, completedAt *time.Time) error {
	now := fmtTS(time.Now())
	var completed any
	if completedAt != nil {
		completed = fmtTS(*completedAt)
	}
	manageDNS := boolToInt(sel.ManageDNS)
	createMail := boolToInt(sel.CreateMail)

	var q string
	if s.db.Dialect == DialectMySQL {
		q = `INSERT INTO panel_setup (id, webserver, db_engine, manage_dns, create_mail, license_key, completed_at, created_at, updated_at)
		     VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
		     ON DUPLICATE KEY UPDATE webserver = VALUES(webserver), db_engine = VALUES(db_engine),
		       manage_dns = VALUES(manage_dns), create_mail = VALUES(create_mail),
		       license_key = VALUES(license_key),
		       completed_at = VALUES(completed_at), updated_at = VALUES(updated_at)`
	} else {
		q = `INSERT INTO panel_setup (id, webserver, db_engine, manage_dns, create_mail, license_key, completed_at, created_at, updated_at)
		     VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
		     ON CONFLICT(id) DO UPDATE SET webserver = excluded.webserver, db_engine = excluded.db_engine,
		       manage_dns = excluded.manage_dns, create_mail = excluded.create_mail,
		       license_key = excluded.license_key,
		       completed_at = excluded.completed_at, updated_at = excluded.updated_at`
	}
	if _, err := s.db.ExecContext(ctx, q,
		string(sel.Webserver), string(sel.DBEngine), manageDNS, createMail, sel.LicenseKey, completed, now, now); err != nil {
		return errx.Internal(err)
	}
	return nil
}
