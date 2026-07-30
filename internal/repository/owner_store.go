package repository

import (
	"context"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// ResourceKind names an owned resource the tenant guard can resolve to an owner.
// Every value maps to a fixed SQL lookup below; the uid is always a bound
// parameter, so the kind (never user input) is the only thing that selects a
// query.
type ResourceKind string

const (
	KindSite        ResourceKind = "site"
	KindDNSZone     ResourceKind = "dns_zone"
	KindDNSRecord   ResourceKind = "dns_record"
	KindMailDomain  ResourceKind = "mail_domain"
	KindMailAccount ResourceKind = "mail_account"
	KindDBInstance  ResourceKind = "db_instance"
	KindDBUser      ResourceKind = "db_user"
)

// ResourceOwnerStore resolves any owned resource's uid to the id of the user who
// owns it. Nested resources (a DNS record, a mailbox) resolve through their
// parent, so isolation is enforced at the same grain the resource is addressed.
//
// This lives in the repository, not behind each domain service's interface, so
// the tenant guard can resolve every resource type through one dependency
// without threading an OwnerOf method through a dozen service contracts (and
// their mocks).
type ResourceOwnerStore struct {
	db *DB
}

// NewResourceOwnerStore constructs a ResourceOwnerStore.
func NewResourceOwnerStore(db *DB) *ResourceOwnerStore { return &ResourceOwnerStore{db: db} }

// ownerQueries maps each kind to the query that returns its owner_id given a uid.
var ownerQueries = map[ResourceKind]string{
	KindSite:        `SELECT owner_id FROM sites WHERE uid = ? AND deleted_at IS NULL`,
	KindDNSZone:     `SELECT owner_id FROM dns_zones WHERE uid = ?`,
	KindDNSRecord:   `SELECT z.owner_id FROM dns_records r JOIN dns_zones z ON z.id = r.zone_id WHERE r.uid = ?`,
	KindMailDomain:  `SELECT owner_id FROM mail_domains WHERE uid = ?`,
	KindMailAccount: `SELECT d.owner_id FROM mail_accounts a JOIN mail_domains d ON d.id = a.domain_id WHERE a.uid = ?`,
	KindDBInstance:  `SELECT owner_id FROM db_instances WHERE uid = ?`,
	KindDBUser:      `SELECT owner_id FROM db_users WHERE uid = ?`,
}

// OwnerOf returns the owner id of the resource of kind with the given uid, or a
// not-found error when no such resource exists.
func (s *ResourceOwnerStore) OwnerOf(ctx context.Context, kind ResourceKind, uid string) (int64, error) {
	q, ok := ownerQueries[kind]
	if !ok {
		return 0, errx.New(errx.KindInternal, "unknown_resource_kind", "No owner lookup for "+string(kind)+".")
	}
	var ownerID int64
	err := s.db.GetContext(ctx, &ownerID, q, uid)
	if isNoRows(err) {
		return 0, errx.NotFound("resource_not_found", "No such resource.")
	}
	if err != nil {
		return 0, errx.Internal(err)
	}
	return ownerID, nil
}
