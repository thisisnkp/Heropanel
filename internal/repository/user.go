package repository

import (
	"context"
	"database/sql"
	"time"

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

// GetByID returns the user with the given internal id.
func (r *UserRepository) GetByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := r.db.GetContext(ctx, &u,
		`SELECT `+userColumns+` FROM users WHERE id = ? AND deleted_at IS NULL`, id)
	if isNoRows(err) {
		return nil, errx.NotFound("user_not_found", "No such user.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &u, nil
}

// List returns up to limit non-deleted users ordered by id, skipping offset.
func (r *UserRepository) List(ctx context.Context, limit, offset int) ([]User, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var users []User
	err := r.db.SelectContext(ctx, &users,
		`SELECT `+userColumns+` FROM users WHERE deleted_at IS NULL ORDER BY id LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil, errx.Internal(err)
	}
	return users, nil
}

// AuthUser carries the fields the login flow needs, including a computed Locked
// flag (1 when the account is currently locked out).
type AuthUser struct {
	ID           int64          `db:"id"`
	UID          string         `db:"uid"`
	Email        string         `db:"email"`
	Username     string         `db:"username"`
	DisplayName  string         `db:"display_name"`
	PasswordHash sql.NullString `db:"password_hash"`
	Status       string         `db:"status"`
	FailedLogins int            `db:"failed_logins"`
	TOTPEnabled  int            `db:"totp_enabled"`
	Locked       int            `db:"locked"`
}

// GetAuthByEmail loads the login-relevant fields for email, computing whether
// the account is locked as of now (avoids parsing timestamps in Go).
func (r *UserRepository) GetAuthByEmail(ctx context.Context, email string, now time.Time) (*AuthUser, error) {
	var u AuthUser
	err := r.db.GetContext(ctx, &u,
		`SELECT id, uid, email, username, display_name, password_hash, status, failed_logins, totp_enabled,
		        CASE WHEN locked_until IS NOT NULL AND locked_until > ? THEN 1 ELSE 0 END AS locked
		 FROM users WHERE email = ? AND deleted_at IS NULL`,
		fmtTS(now), email)
	if isNoRows(err) {
		return nil, errx.NotFound("user_not_found", "No such user.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &u, nil
}

// SetTOTP stores (or clears) a user's TOTP secret and enabled flag. NOTE: the
// secret is stored as-is for now; envelope encryption of *_enc columns is a
// planned follow-up (docs/05 §6).
func (r *UserRepository) SetTOTP(ctx context.Context, id int64, secret string, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	var secretVal any
	if secret == "" {
		secretVal = nil
	} else {
		secretVal = []byte(secret)
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET totp_secret_enc = ?, totp_enabled = ? WHERE id = ?`, secretVal, e, id)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// SetTOTPEnabled toggles a user's TOTP enabled flag.
func (r *UserRepository) SetTOTPEnabled(ctx context.Context, id int64, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET totp_enabled = ? WHERE id = ?`, e, id); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// GetTOTP returns a user's TOTP secret and enabled flag.
func (r *UserRepository) GetTOTP(ctx context.Context, id int64) (secret string, enabled bool, err error) {
	var row struct {
		Secret  []byte `db:"totp_secret_enc"`
		Enabled int    `db:"totp_enabled"`
	}
	e := r.db.GetContext(ctx, &row, `SELECT totp_secret_enc, totp_enabled FROM users WHERE id = ?`, id)
	if isNoRows(e) {
		return "", false, errx.NotFound("user_not_found", "No such user.")
	}
	if e != nil {
		return "", false, errx.Internal(e)
	}
	return string(row.Secret), row.Enabled == 1, nil
}

// RegisterFailedLogin increments the failed-login counter and locks the account
// until lockUntil once the count reaches threshold.
func (r *UserRepository) RegisterFailedLogin(ctx context.Context, id int64, threshold int, lockUntil time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users
		    SET failed_logins = failed_logins + 1,
		        locked_until = CASE WHEN failed_logins + 1 >= ? THEN ? ELSE locked_until END
		  WHERE id = ?`,
		threshold, fmtTS(lockUntil), id)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// RegisterSuccessfulLogin clears the failed-login state and records the login.
func (r *UserRepository) RegisterSuccessfulLogin(ctx context.Context, id int64, ip string, now time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users
		    SET failed_logins = 0, locked_until = NULL, last_login_at = ?, last_login_ip = ?
		  WHERE id = ?`,
		fmtTS(now), ip, id)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}
