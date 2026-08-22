// Package database manages MariaDB databases, users, and grants. State lives in
// the repo; the actual SQL runs as root via the privileged broker (which pipes
// SQL over the local socket). See docs/03 §4.
package database

import (
	"context"
	"os"
	"regexp"
	"strings"

	"github.com/thisisnkp/nexpanel/internal/broker"
	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/idgen"
)

var reName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// EngineMariaDB is the database engine the panel manages. It is the only one:
// NexPanel is a single-stack panel, and PostgreSQL support was removed rather
// than left in as a second, untested path. The engine is still recorded on every
// row and returned in the API, because existing installs have the column and
// because "which engine is this" is a question worth being able to answer if a
// second one is ever added back deliberately.
//
// MySQL is not a separate engine here: it is wire-compatible with MariaDB and
// driven through the same db.* broker capabilities.
const EngineMariaDB = "mariadb"

// normalizeEngine validates the engine, defaulting to MariaDB.
func normalizeEngine(engine string) (string, error) {
	switch engine {
	case "", EngineMariaDB, "mysql":
		return EngineMariaDB, nil
	default:
		return "", errx.Validation("invalid_engine", "MariaDB is the only database engine this panel manages.",
			errx.Field{Field: "engine", Code: "unsupported", Message: "unknown engine"})
	}
}

// brokerCap returns the broker capability name for an operation, e.g.
// "user.create" => "db.user.create".
func brokerCap(_, op string) string { return "db." + op }

// errUnmanagedEngine explains a row this panel can no longer act on.
//
// An install upgraded from a release that supported PostgreSQL still has its
// pg rows in db_instances and db_users. Those must not be quietly routed to the
// db.* capabilities: "drop the database called reports" aimed at MariaDB instead
// of PostgreSQL either fails confusingly or, if a MariaDB database happens to
// share the name, destroys the wrong one. So the row is refused by name, with
// the reason, and the operator removes it with the engine's own tools.
func errUnmanagedEngine(engine string) error {
	return errx.Validation("unmanaged_engine",
		"This record was created on "+engine+", which this panel no longer manages. "+
			"Remove it with that engine's own tools; NexPanel manages MariaDB only.")
}

// getDatabase loads a database record and refuses one this panel cannot act on.
func (s *Service) getDatabase(ctx context.Context, uid string) (*InstanceRecord, error) {
	rec, err := s.repo.GetDatabaseByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if rec.Engine != "" && rec.Engine != EngineMariaDB {
		return nil, errUnmanagedEngine(rec.Engine)
	}
	return rec, nil
}

// getUser loads a database-user record and refuses one this panel cannot act on.
func (s *Service) getUser(ctx context.Context, uid string) (*UserRecord, error) {
	rec, err := s.repo.GetUserByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if rec.Engine != "" && rec.Engine != EngineMariaDB {
		return nil, errUnmanagedEngine(rec.Engine)
	}
	return rec, nil
}

// DumpDir is where exports are produced and imports are staged. It must match
// the broker's dumpRoot: npd writes uploads here and reads dumps back, while the
// broker is the only thing that ever runs SQL against them.
const DumpDir = "/var/lib/nexpanel/dumps"

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

// Size is the API view of a database's on-disk footprint.
type Size struct {
	Bytes  int64 `json:"bytes"`
	Tables int64 `json:"tables"`
}

// Export is a finished dump waiting to be streamed to the client.
type Export struct {
	// Path is the server-side file. It is not exposed to API clients; the handler
	// streams the file and then calls DiscardExport.
	Path  string
	Name  string
	File  string
	Bytes int64
}

// Repo is the persistence contract (implemented by internal/repository).
type Repo interface {
	InsertDatabase(ctx context.Context, r *InstanceRecord) error
	ListDatabases(ctx context.Context, ownerID int64, limit, offset int) ([]InstanceRecord, error)
	GetDatabaseByUID(ctx context.Context, uid string) (*InstanceRecord, error)
	// GetDatabaseByID resolves the row a sign-on ticket points at. Tickets store
	// the numeric id because they are a foreign key with ON DELETE CASCADE —
	// dropping a database must take its pending tickets with it.
	GetDatabaseByID(ctx context.Context, id int64) (*InstanceRecord, error)
	DeleteDatabase(ctx context.Context, uid string) error
	InsertUser(ctx context.Context, r *UserRecord) error
	ListUsers(ctx context.Context, ownerID int64, limit, offset int) ([]UserRecord, error)
	GetUserByUID(ctx context.Context, uid string) (*UserRecord, error)
	DeleteUser(ctx context.Context, uid string) error
	InsertGrant(ctx context.Context, dbUserID, dbInstanceID int64, privileges string) error
	DeleteGrant(ctx context.Context, dbUserID, dbInstanceID int64) error
}

// Service orchestrates database operations.
type Service struct {
	repo       Repo
	broker     broker.Gateway
	adminerURL string
	ssoRepo    SSORepo
}

// NewService constructs the database Service.
func NewService(repo Repo, gw broker.Gateway) *Service { return &Service{repo: repo, broker: gw} }

// resolveEngine validates the caller's engine, defaulting to MariaDB.
func (s *Service) resolveEngine(engine string) (string, error) {
	return normalizeEngine(engine)
}

func (s *Service) requireBroker() error {
	if s.broker == nil {
		return errx.New(errx.KindUnavailable, "broker_unavailable", "The broker is not available.")
	}
	return nil
}

// CreateDatabase records and creates a database on the given engine (default
// MariaDB).
func (s *Service) CreateDatabase(ctx context.Context, ownerID int64, name, engine string) (*Instance, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !reName.MatchString(name) {
		return nil, errx.Validation("invalid_name",
			"Database name must start with a letter and use only lowercase letters, digits, and underscore.")
	}
	engine, err := s.resolveEngine(engine)
	if err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	charset := "utf8mb4"
	rec := &InstanceRecord{OwnerID: ownerID, Engine: engine, Name: name, Charset: charset, Status: "active"}
	if err := s.repo.InsertDatabase(ctx, rec); err != nil {
		return nil, err
	}
	if _, err := s.broker.Invoke(ctx, brokerCap(engine, "create"), map[string]any{"name": name}); err != nil {
		_ = s.repo.DeleteDatabase(ctx, rec.UID)
		return nil, err
	}
	return instanceView(rec), nil
}

// DeleteDatabase drops a database and removes its record.
func (s *Service) DeleteDatabase(ctx context.Context, uid string) error {
	rec, err := s.getDatabase(ctx, uid)
	if err != nil {
		return err
	}
	if err := s.requireBroker(); err != nil {
		return err
	}
	if _, err := s.broker.Invoke(ctx, brokerCap(rec.Engine, "drop"), map[string]any{"name": rec.Name}); err != nil {
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
func (s *Service) CreateUser(ctx context.Context, ownerID int64, username, host, password, engine string) (*User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !reName.MatchString(username) {
		return nil, errx.Validation("invalid_username",
			"Username must start with a letter and use only lowercase letters, digits, and underscore.")
	}
	engine, err := s.resolveEngine(engine)
	if err != nil {
		return nil, err
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
	rec := &UserRecord{OwnerID: ownerID, Engine: engine, Username: username, Host: host}
	if err := s.repo.InsertUser(ctx, rec); err != nil {
		return nil, err
	}
	// PostgreSQL roles have no host; the broker capability ignores the field.
	if _, err := s.broker.Invoke(ctx, brokerCap(engine, "user.create"), map[string]any{
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
	dbRec, err := s.getDatabase(ctx, dbUID)
	if err != nil {
		return err
	}
	userRec, err := s.getUser(ctx, userUID)
	if err != nil {
		return err
	}
	if dbRec.Engine != userRec.Engine {
		return errx.Validation("engine_mismatch", "The database and user must be on the same engine.")
	}
	if len(privileges) == 0 {
		privileges = []string{"ALL"}
	}
	if err := s.requireBroker(); err != nil {
		return err
	}
	if _, err := s.broker.Invoke(ctx, brokerCap(dbRec.Engine, "grant"), map[string]any{
		"database":   dbRec.Name,
		"username":   userRec.Username,
		"host":       userRec.Host,
		"privileges": privileges,
	}); err != nil {
		return err
	}
	return s.repo.InsertGrant(ctx, userRec.ID, dbRec.ID, strings.Join(privileges, ","))
}

// DeleteUser drops a database user and removes its record.
func (s *Service) DeleteUser(ctx context.Context, uid string) error {
	rec, err := s.getUser(ctx, uid)
	if err != nil {
		return err
	}
	if err := s.requireBroker(); err != nil {
		return err
	}
	if _, err := s.broker.Invoke(ctx, brokerCap(rec.Engine, "user.drop"), map[string]any{
		"username": rec.Username, "host": rec.Host,
	}); err != nil {
		return err
	}
	return s.repo.DeleteUser(ctx, uid)
}

// Revoke removes a user's privileges on a database. The record is dropped only
// after MariaDB confirms, so the panel never claims access was removed when it
// was not.
func (s *Service) Revoke(ctx context.Context, dbUID, userUID string, privileges []string) error {
	dbRec, err := s.getDatabase(ctx, dbUID)
	if err != nil {
		return err
	}
	userRec, err := s.getUser(ctx, userUID)
	if err != nil {
		return err
	}
	if len(privileges) == 0 {
		privileges = []string{"ALL"}
	}
	if err := s.requireBroker(); err != nil {
		return err
	}
	if _, err := s.broker.Invoke(ctx, brokerCap(dbRec.Engine, "revoke"), map[string]any{
		"database":   dbRec.Name,
		"username":   userRec.Username,
		"host":       userRec.Host,
		"privileges": privileges,
	}); err != nil {
		return err
	}
	return s.repo.DeleteGrant(ctx, userRec.ID, dbRec.ID)
}

// Size reports a database's on-disk size.
func (s *Service) Size(ctx context.Context, uid string) (*Size, error) {
	rec, err := s.getDatabase(ctx, uid)
	if err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	res, err := s.broker.Invoke(ctx, brokerCap(rec.Engine, "size"), map[string]any{"name": rec.Name})
	if err != nil {
		return nil, err
	}
	return &Size{Bytes: asInt64(res["bytes"]), Tables: asInt64(res["tables"])}, nil
}

// Export dumps a database and returns the server-side file for the caller to
// stream. The caller must call DiscardExport when done, however it goes: the
// dump is a full copy of the customer's data sitting on disk.
func (s *Service) Export(ctx context.Context, uid string) (*Export, error) {
	rec, err := s.getDatabase(ctx, uid)
	if err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	// A fresh filename per export: two concurrent exports of the same database
	// must not write over each other's dump mid-stream.
	file := rec.Name + "-" + idgen.NewULID() + ".sql"
	res, err := s.broker.Invoke(ctx, brokerCap(rec.Engine, "export"), map[string]any{
		"name": rec.Name, "file": file,
	})
	if err != nil {
		return nil, err
	}
	path, _ := res["path"].(string)
	if path == "" {
		return nil, errx.New(errx.KindUpstream, "export_failed", "The export did not produce a file.")
	}
	return &Export{Path: path, Name: rec.Name, File: file + ".gz", Bytes: asInt64(res["bytes"])}, nil
}

// DiscardExport removes a finished dump from the server.
func (s *Service) DiscardExport(path string) error {
	if path == "" {
		return nil
	}
	return os.Remove(path)
}

// ImportStagePath returns where the caller must write an upload before calling
// Import, along with the bare filename Import expects.
//
// npd stages the upload itself rather than shipping the bytes through the
// broker: the broker's transport is length-prefixed JSON, and a database dump is
// arbitrarily large.
func (s *Service) ImportStagePath(gzipped bool) (path, file string) {
	file = "import-" + idgen.NewULID() + ".sql"
	if gzipped {
		file += ".gz"
	}
	return DumpDir + "/" + file, file
}

// Import loads a SQL file previously staged at ImportStagePath into a database.
func (s *Service) Import(ctx context.Context, uid, file string) error {
	rec, err := s.getDatabase(ctx, uid)
	if err != nil {
		return err
	}
	if err := s.requireBroker(); err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, brokerCap(rec.Engine, "import"), map[string]any{"name": rec.Name, "file": file})
	return err
}

// asInt64 normalizes a JSON number (float64 over the wire) to an int64.
func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func instanceView(r *InstanceRecord) *Instance {
	return &Instance{UID: r.UID, Engine: r.Engine, Name: r.Name, Charset: r.Charset, Status: r.Status, CreatedAt: r.CreatedAt}
}

func userView(r *UserRecord) *User {
	return &User{UID: r.UID, Engine: r.Engine, Username: r.Username, Host: r.Host, CreatedAt: r.CreatedAt}
}
