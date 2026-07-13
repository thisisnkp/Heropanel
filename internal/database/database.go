// Package database manages MariaDB databases, users, and grants. State lives in
// the repo; the actual SQL runs as root via the privileged broker (which pipes
// SQL over the local socket). See docs/03 §4.
package database

import (
	"context"
	"regexp"
	"strings"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

var reName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Instance is the API view of a database.
type Instance struct {
	UID       string `json:"uid"`
	Engine    string `json:"engine"`
	Name      string `json:"name"`
	Charset   string `json:"charset"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// User is the API view of a database user.
type User struct {
	UID       string `json:"uid"`
	Engine    string `json:"engine"`
	Username  string `json:"username"`
	Host      string `json:"host"`
	CreatedAt string `json:"created_at"`
}

// InstanceRecord / UserRecord are the persistence rows.
type InstanceRecord struct {
	ID        int64  `db:"id"`
	UID       string `db:"uid"`
	OwnerID   int64  `db:"owner_id"`
	Engine    string `db:"engine"`
	Name      string `db:"name"`
	Charset   string `db:"charset"`
	Status    string `db:"status"`
	CreatedAt string `db:"created_at"`
}

type UserRecord struct {
	ID        int64  `db:"id"`
	UID       string `db:"uid"`
	OwnerID   int64  `db:"owner_id"`
	Engine    string `db:"engine"`
	Username  string `db:"username"`
	Host      string `db:"host"`
	CreatedAt string `db:"created_at"`
}

// Repo is the persistence contract (implemented by internal/repository).
type Repo interface {
	InsertDatabase(ctx context.Context, r *InstanceRecord) error
	ListDatabases(ctx context.Context, ownerID int64, limit, offset int) ([]InstanceRecord, error)
	GetDatabaseByUID(ctx context.Context, uid string) (*InstanceRecord, error)
	DeleteDatabase(ctx context.Context, uid string) error
	InsertUser(ctx context.Context, r *UserRecord) error
	ListUsers(ctx context.Context, ownerID int64, limit, offset int) ([]UserRecord, error)
	GetUserByUID(ctx context.Context, uid string) (*UserRecord, error)
	InsertGrant(ctx context.Context, dbUserID, dbInstanceID int64, privileges string) error
}

// Service orchestrates database operations.
type Service struct {
	repo   Repo
	broker broker.Gateway
}

// NewService constructs the database Service.
func NewService(repo Repo, gw broker.Gateway) *Service { return &Service{repo: repo, broker: gw} }

func (s *Service) requireBroker() error {
	if s.broker == nil {
		return errx.New(errx.KindUnavailable, "broker_unavailable", "The broker is not available.")
	}
	return nil
}

// CreateDatabase records and creates a database.
func (s *Service) CreateDatabase(ctx context.Context, ownerID int64, name string) (*Instance, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !reName.MatchString(name) {
		return nil, errx.Validation("invalid_name",
			"Database name must start with a letter and use only lowercase letters, digits, and underscore.")
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	rec := &InstanceRecord{OwnerID: ownerID, Engine: "mariadb", Name: name, Charset: "utf8mb4", Status: "active"}
	if err := s.repo.InsertDatabase(ctx, rec); err != nil {
		return nil, err
	}
	if _, err := s.broker.Invoke(ctx, "db.create", map[string]any{"name": name}); err != nil {
		_ = s.repo.DeleteDatabase(ctx, rec.UID)
		return nil, err
	}
	return instanceView(rec), nil
}

// DeleteDatabase drops a database and removes its record.
func (s *Service) DeleteDatabase(ctx context.Context, uid string) error {
	rec, err := s.repo.GetDatabaseByUID(ctx, uid)
	if err != nil {
		return err
	}
	if err := s.requireBroker(); err != nil {
		return err
	}
	if _, err := s.broker.Invoke(ctx, "db.drop", map[string]any{"name": rec.Name}); err != nil {
		return err
	}
	return s.repo.DeleteDatabase(ctx, uid)
}

// ListDatabases lists databases (ownerID 0 = all).
func (s *Service) ListDatabases(ctx context.Context, ownerID int64, limit, offset int) ([]Instance, error) {
	recs, err := s.repo.ListDatabases(ctx, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]Instance, len(recs))
	for i := range recs {
		out[i] = *instanceView(&recs[i])
	}
	return out, nil
}

// CreateUser records and creates a database user with the given password.
func (s *Service) CreateUser(ctx context.Context, ownerID int64, username, host, password string) (*User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !reName.MatchString(username) {
		return nil, errx.Validation("invalid_username",
			"Username must start with a letter and use only lowercase letters, digits, and underscore.")
	}
	if host == "" {
		host = "localhost"
	}
	if len(password) < 8 {
		return nil, errx.Validation("weak_password", "Database password must be at least 8 characters.")
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	rec := &UserRecord{OwnerID: ownerID, Engine: "mariadb", Username: username, Host: host}
	if err := s.repo.InsertUser(ctx, rec); err != nil {
		return nil, err
	}
	if _, err := s.broker.Invoke(ctx, "db.user.create", map[string]any{
		"username": username, "host": host, "password": password,
	}); err != nil {
		return nil, err
	}
	return userView(rec), nil
}

// ListUsers lists database users (ownerID 0 = all).
func (s *Service) ListUsers(ctx context.Context, ownerID int64, limit, offset int) ([]User, error) {
	recs, err := s.repo.ListUsers(ctx, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]User, len(recs))
	for i := range recs {
		out[i] = *userView(&recs[i])
	}
	return out, nil
}

// Grant grants privileges on a database to a user.
func (s *Service) Grant(ctx context.Context, dbUID, userUID string, privileges []string) error {
	dbRec, err := s.repo.GetDatabaseByUID(ctx, dbUID)
	if err != nil {
		return err
	}
	userRec, err := s.repo.GetUserByUID(ctx, userUID)
	if err != nil {
		return err
	}
	if len(privileges) == 0 {
		privileges = []string{"ALL"}
	}
	if err := s.requireBroker(); err != nil {
		return err
	}
	if _, err := s.broker.Invoke(ctx, "db.grant", map[string]any{
		"database":   dbRec.Name,
		"username":   userRec.Username,
		"host":       userRec.Host,
		"privileges": privileges,
	}); err != nil {
		return err
	}
	return s.repo.InsertGrant(ctx, userRec.ID, dbRec.ID, strings.Join(privileges, ","))
}

func instanceView(r *InstanceRecord) *Instance {
	return &Instance{UID: r.UID, Engine: r.Engine, Name: r.Name, Charset: r.Charset, Status: r.Status, CreatedAt: r.CreatedAt}
}

func userView(r *UserRecord) *User {
	return &User{UID: r.UID, Engine: r.Engine, Username: r.Username, Host: r.Host, CreatedAt: r.CreatedAt}
}
