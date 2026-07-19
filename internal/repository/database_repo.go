package repository

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// DatabaseStore implements database.Repo over the datastore.
type DatabaseStore struct {
	db *DB
}

// NewDatabaseStore constructs a DatabaseStore.
func NewDatabaseStore(db *DB) *DatabaseStore { return &DatabaseStore{db: db} }

var _ database.Repo = (*DatabaseStore)(nil)

func (s *DatabaseStore) InsertDatabase(ctx context.Context, r *database.InstanceRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO db_instances (uid, owner_id, engine, name, charset, status) VALUES (?, ?, ?, ?, ?, ?)`,
		r.UID, r.OwnerID, r.Engine, r.Name, r.Charset, r.Status)
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "database_exists", "A database with that name already exists.")
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

func (s *DatabaseStore) ListDatabases(ctx context.Context, ownerID int64, limit, offset int) ([]database.InstanceRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var recs []database.InstanceRecord
	const cols = `id, uid, owner_id, engine, name, charset, status, created_at`
	var err error
	if ownerID > 0 {
		err = s.db.SelectContext(ctx, &recs,
			`SELECT `+cols+` FROM db_instances WHERE owner_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`, ownerID, limit, offset)
	} else {
		err = s.db.SelectContext(ctx, &recs,
			`SELECT `+cols+` FROM db_instances ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return recs, nil
}

func (s *DatabaseStore) GetDatabaseByUID(ctx context.Context, uid string) (*database.InstanceRecord, error) {
	var rec database.InstanceRecord
	err := s.db.GetContext(ctx, &rec,
		`SELECT id, uid, owner_id, engine, name, charset, status, created_at FROM db_instances WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("database_not_found", "No such database.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

func (s *DatabaseStore) DeleteDatabase(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM db_instances WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

func (s *DatabaseStore) InsertUser(ctx context.Context, r *database.UserRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO db_users (uid, owner_id, engine, username, host) VALUES (?, ?, ?, ?, ?)`,
		r.UID, r.OwnerID, r.Engine, r.Username, r.Host)
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "db_user_exists", "A database user with that name/host already exists.")
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

func (s *DatabaseStore) ListUsers(ctx context.Context, ownerID int64, limit, offset int) ([]database.UserRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var recs []database.UserRecord
	const cols = `id, uid, owner_id, engine, username, host, created_at`
	var err error
	if ownerID > 0 {
		err = s.db.SelectContext(ctx, &recs,
			`SELECT `+cols+` FROM db_users WHERE owner_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`, ownerID, limit, offset)
	} else {
		err = s.db.SelectContext(ctx, &recs,
			`SELECT `+cols+` FROM db_users ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return recs, nil
}

func (s *DatabaseStore) GetUserByUID(ctx context.Context, uid string) (*database.UserRecord, error) {
	var rec database.UserRecord
	err := s.db.GetContext(ctx, &rec,
		`SELECT id, uid, owner_id, engine, username, host, created_at FROM db_users WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("db_user_not_found", "No such database user.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

func (s *DatabaseStore) InsertGrant(ctx context.Context, dbUserID, dbInstanceID int64, privileges string) error {
	// Idempotent: replace an existing grant for the pair.
	res, err := s.db.ExecContext(ctx,
		`UPDATE db_grants SET privileges = ? WHERE db_user_id = ? AND db_instance_id = ?`,
		privileges, dbUserID, dbInstanceID)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO db_grants (db_user_id, db_instance_id, privileges) VALUES (?, ?, ?)`,
		dbUserID, dbInstanceID, privileges); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// DeleteUser removes a database user's record. Its grants go with it via the
// db_grants foreign key.
func (s *DatabaseStore) DeleteUser(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM db_users WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// DeleteGrant removes the recorded grant for a user/database pair.
func (s *DatabaseStore) DeleteGrant(ctx context.Context, dbUserID, dbInstanceID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM db_grants WHERE db_user_id = ? AND db_instance_id = ?`,
		dbUserID, dbInstanceID); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// ── Adminer/phpMyAdmin hand-off sessions ─────────────────────────────────────

const ssoSessionCols = `id, uid, db_instance_id, username, created_at, expires_at`

// InsertSSOSession records a hand-off account so the sweeper can drop it later.
func (s *DatabaseStore) InsertSSOSession(ctx context.Context, r *database.SSOSessionRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO db_sso_sessions (uid, db_instance_id, username, expires_at) VALUES (?, ?, ?, ?)`,
		r.UID, r.DBInstanceID, r.Username, r.ExpiresAt)
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

// ListExpiredSSOSessions returns the sessions whose accounts are due to be
// dropped. `now` is formatted by the caller so the comparison is a plain string
// compare that behaves identically on SQLite TEXT and MariaDB DATETIME.
func (s *DatabaseStore) ListExpiredSSOSessions(ctx context.Context, now string) ([]database.SSOSessionRecord, error) {
	var recs []database.SSOSessionRecord
	if err := s.db.SelectContext(ctx, &recs,
		`SELECT `+ssoSessionCols+` FROM db_sso_sessions WHERE expires_at <= ? ORDER BY id LIMIT 500`,
		now); err != nil {
		return nil, errx.Internal(err)
	}
	return recs, nil
}

// DeleteSSOSession removes a hand-off session record.
func (s *DatabaseStore) DeleteSSOSession(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM db_sso_sessions WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}
