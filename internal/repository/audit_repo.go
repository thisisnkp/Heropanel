package repository

import (
	"context"
	"database/sql"
	"strings"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// defaultAuditLimit bounds an unfiltered listing. The audit log is the one table
// that only ever grows, so a page size is not optional.
const defaultAuditLimit = 100

// AuditRow is a persisted audit entry.
//
// CreatedAt is a string, not a time.Time, because the row's hash covers the
// timestamp exactly as stored: re-parsing it into a time and re-rendering it
// would risk a different byte sequence and a spurious Verify failure.
//
// The same round-trip requirement constrains Detail, and it is the reason this
// table is only claimed to work on MariaDB. MariaDB's JSON type is an alias for
// LONGTEXT with a json_valid() check, so it returns the bytes it was given.
// MySQL 8's native JSON type does not: it parses to a binary form and
// re-serializes on read, reordering object keys and respacing the text — which
// would change the hashed bytes and fail Verify on every row carrying a detail.
// HeroPanel targets MariaDB (docs/02), so this is sound; porting the panel's own
// store to MySQL would mean moving this column to TEXT first.
type AuditRow struct {
	ID           int64         `db:"id"`
	UID          string        `db:"uid"`
	ActorUserID  sql.NullInt64 `db:"actor_user_id"`
	ActorIP      string        `db:"actor_ip"`
	ActorKind    string        `db:"actor_kind"`
	Action       string        `db:"action"`
	ResourceType string        `db:"resource_type"`
	ResourceID   string        `db:"resource_id"`
	Outcome      string        `db:"outcome"`
	Detail       string        `db:"detail"`
	PrevHash     string        `db:"prev_hash"`
	RowHash      string        `db:"row_hash"`
	CreatedAt    string        `db:"created_at"`
}

// AuditRepository persists the audit chain.
type AuditRepository struct {
	db *DB
}

// NewAuditRepository constructs an AuditRepository.
func NewAuditRepository(db *DB) *AuditRepository { return &AuditRepository{db: db} }

// Append inserts a committed entry. It writes exactly what it is given: the
// hashes are already computed and any adjustment here would invalidate them.
func (r *AuditRepository) Append(ctx context.Context, e *audit.Entry) error {
	var actor sql.NullInt64
	if e.ActorUserID > 0 {
		actor = sql.NullInt64{Int64: e.ActorUserID, Valid: true}
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO audit_log
		   (uid, actor_user_id, actor_ip, actor_kind, action, resource_type,
		    resource_id, outcome, detail, prev_hash, row_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.UID, actor, e.ActorIP, string(e.ActorKind), e.Action, e.ResourceType,
		e.ResourceID, string(e.Outcome), e.Detail, e.PrevHash, e.RowHash, e.CreatedAt)
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		e.ID = id
	}
	return nil
}

// Head returns the most recent row_hash, or "" when the table is empty.
//
// "Most recent" is by id, not created_at: created_at has second precision, so
// several rows can share one, and the chain needs a total order that matches
// insertion exactly.
func (r *AuditRepository) Head(ctx context.Context) (string, error) {
	var hash string
	err := r.db.GetContext(ctx, &hash, `SELECT row_hash FROM audit_log ORDER BY id DESC LIMIT 1`)
	if isNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", errx.Internal(err)
	}
	return hash, nil
}

// List returns entries newest-first. A negative Limit means "every row" and
// exists for chain verification, which cannot be done from a page.
func (r *AuditRepository) List(ctx context.Context, f audit.Filter) ([]audit.Entry, error) {
	var (
		where []string
		args  []any
	)
	if f.ActorUserID > 0 {
		where = append(where, "actor_user_id = ?")
		args = append(args, f.ActorUserID)
	}
	if f.ResourceType != "" {
		where = append(where, "resource_type = ?")
		args = append(args, f.ResourceType)
	}
	if f.ResourceID != "" {
		where = append(where, "resource_id = ?")
		args = append(args, f.ResourceID)
	}
	if f.Action != "" {
		where = append(where, "action = ?")
		args = append(args, f.Action)
	}

	q := `SELECT id, uid, actor_user_id, actor_ip, actor_kind, action, resource_type,
	             resource_id, outcome, detail, prev_hash, row_hash, created_at
	        FROM audit_log`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id DESC"

	if f.Limit >= 0 {
		limit := f.Limit
		if limit == 0 {
			limit = defaultAuditLimit
		}
		q += " LIMIT ?"
		args = append(args, limit)
		if f.Offset > 0 {
			q += " OFFSET ?"
			args = append(args, f.Offset)
		}
	}

	var rows []AuditRow
	if err := r.db.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]audit.Entry, 0, len(rows))
	for _, row := range rows {
		out = append(out, audit.Entry{
			ID:           row.ID,
			UID:          row.UID,
			ActorUserID:  row.ActorUserID.Int64,
			ActorIP:      row.ActorIP,
			ActorKind:    audit.ActorKind(row.ActorKind),
			Action:       row.Action,
			ResourceType: row.ResourceType,
			ResourceID:   row.ResourceID,
			Outcome:      audit.Outcome(row.Outcome),
			Detail:       row.Detail,
			PrevHash:     row.PrevHash,
			RowHash:      row.RowHash,
			CreatedAt:    row.CreatedAt,
		})
	}
	return out, nil
}
