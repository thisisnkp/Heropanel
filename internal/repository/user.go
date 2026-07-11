package repository

import (
	"context"
	"database/sql"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// User is the persisted identity row (core columns; timestamps are managed by
// the database and read separately where needed).
type User struct {
	ID           int64          `db:"id"`
	UID          string         `db:"uid"`
	Email        string         `db:"email"`
	Username     string         `db:"username"`
	DisplayName  string         `db:"display_name"`
	PasswordHash sql.NullString `db:"password_hash"`
	Status       string         `db:"status"`
}

// UserRepository provides persistence for users.
type UserRepository struct {
	db *DB
}

// NewUserRepository constructs a UserRepository.
func NewUserRepository(db *DB) *UserRepository { return &UserRepository{db: db} }

const userColumns = `id, uid, email, username, display_name, password_hash, status`

// Create inserts u, assigning a fresh UID (26-char ULID) and populating u.ID.
func (r *UserRepository) Create(ctx context.Context, u *User) error {
	if u.UID == "" {
		u.UID = idgen.NewULID()
	}
	if u.Status == "" {
		u.Status = "active"
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO users (uid, email, username, display_name, password_hash, status)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		u.UID, u.Email, u.Username, u.DisplayName, u.PasswordHash, u.Status,
	)
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "user_create_failed", "Could not create the user.")
	}
	if id, err := res.LastInsertId(); err == nil {
		u.ID = id
	}
	return nil
}

// GetByEmail returns the active (non-deleted) user with the given email.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.db.GetContext(ctx, &u,
		`SELECT `+userColumns+` FROM users WHERE email = ? AND deleted_at IS NULL`, email)
	if isNoRows(err) {
		return nil, errx.NotFound("user_not_found", "No user with that email.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &u, nil
}

// GetByUID returns the user with the given external UID.
func (r *UserRepository) GetByUID(ctx context.Context, uid string) (*User, error) {
	var u User
	err := r.db.GetContext(ctx, &u,
		`SELECT `+userColumns+` FROM users WHERE uid = ? AND deleted_at IS NULL`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("user_not_found", "No such user.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &u, nil
}

// Count returns the number of non-deleted users (used by the first-run/bootstrap
// flow to decide whether an admin must be created).
func (r *UserRepository) Count(ctx context.Context) (int, error) {
	var n int
	if err := r.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`); err != nil {
		return 0, errx.Internal(err)
	}
	return n, nil
}
