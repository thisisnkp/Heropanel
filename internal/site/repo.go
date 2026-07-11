package site

import (
	"context"
	"database/sql"
)

// Record is the persistence-facing representation of a site joined with its
// system user. System-user fields are nullable because a site exists briefly
// before it is provisioned.
type Record struct {
	ID            int64          `db:"id"`
	UID           string         `db:"uid"`
	OwnerID       int64          `db:"owner_id"`
	Name          string         `db:"name"`
	PrimaryDomain string         `db:"primary_domain"`
	Type          string         `db:"type"`
	DeployMode    string         `db:"deploy_mode"`
	Status        string         `db:"status"`
	Webserver     string         `db:"webserver"`
	DocumentRoot  string         `db:"document_root"`
	LinuxUser     sql.NullString `db:"linux_user"`
	LinuxUID      sql.NullInt64  `db:"linux_uid"`
	HomeDir       sql.NullString `db:"home_dir"`
	CreatedAt     string         `db:"created_at"`
}

// ProvisionData carries the derived identity/paths written when a site is
// provisioned.
type ProvisionData struct {
	SiteID        int64
	DocumentRoot  string
	LinuxUser     string
	LinuxUID      int
	HomeDir       string
	Shell         string
	PrimaryDomain string
}

// Repo is the persistence contract the site service depends on. It is
// implemented by internal/repository (infrastructure implements the domain
// interface — clean architecture).
type Repo interface {
	// Insert creates the sites row in status "provisioning", assigning ID/UID
	// and CreatedAt on the passed record.
	Insert(ctx context.Context, r *Record) error
	// Provision, in a transaction, sets the document root and inserts the
	// site_system_users and primary domain rows.
	Provision(ctx context.Context, p ProvisionData) error
	// UpdateStatus sets a site's status by internal id.
	UpdateStatus(ctx context.Context, id int64, status string) error
	// GetByUID returns a site (joined with its system user).
	GetByUID(ctx context.Context, uid string) (*Record, error)
	// List returns sites ordered by id. ownerID 0 lists all.
	List(ctx context.Context, ownerID int64, limit, offset int) ([]Record, error)
	// SoftDelete marks a site deleted.
	SoftDelete(ctx context.Context, uid string) error
}
