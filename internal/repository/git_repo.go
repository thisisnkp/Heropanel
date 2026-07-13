package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/internal/git"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// GitStore implements git.Repo over the datastore.
type GitStore struct {
	db *DB
}

// NewGitStore constructs a GitStore.
func NewGitStore(db *DB) *GitStore { return &GitStore{db: db} }

var _ git.Repo = (*GitStore)(nil)

const gitSourceCols = `id, uid, site_id, repo_url, branch, build_command, web_root, webhook_secret, created_at, updated_at`

// UpsertSource writes the site's single Git source. It updates in place when one
// exists (a site has at most one source) and inserts otherwise, then reloads the
// row so uid/timestamps are populated. Dialect-agnostic (no ON CONFLICT).
func (s *GitStore) UpsertSource(ctx context.Context, r *git.SourceRecord) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE git_sources SET repo_url = ?, branch = ?, build_command = ?, web_root = ?, webhook_secret = ?, updated_at = ?
		 WHERE site_id = ?`,
		r.RepoURL, r.Branch, r.BuildCommand, r.WebRoot, r.WebhookSecret, fmtTS(time.Now()), r.SiteID)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		r.UID = idgen.NewULID()
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO git_sources (uid, site_id, repo_url, branch, build_command, web_root, webhook_secret)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.UID, r.SiteID, r.RepoURL, r.Branch, r.BuildCommand, r.WebRoot, r.WebhookSecret); err != nil {
			return errx.Wrap(err, errx.KindConflict, "git_source_exists", "A Git source already exists for this site.")
		}
	}
	got, err := s.GetSourceBySiteID(ctx, r.SiteID)
	if err != nil {
		return err
	}
	*r = *got
	return nil
}

// GetSourceBySiteID returns a site's Git source, or a not-found error.
func (s *GitStore) GetSourceBySiteID(ctx context.Context, siteID int64) (*git.SourceRecord, error) {
	var rec git.SourceRecord
	err := s.db.GetContext(ctx, &rec, `SELECT `+gitSourceCols+` FROM git_sources WHERE site_id = ?`, siteID)
	if isNoRows(err) {
		return nil, errx.NotFound("git_source_not_found", "No Git source is configured for this site.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

const gitDeploymentCols = `id, uid, site_id, source_id, commit_sha, status, trigger_kind, release_dir, log, created_at, finished_at`

// InsertDeployment appends a deployment row, assigning UID/ID.
func (s *GitStore) InsertDeployment(ctx context.Context, r *git.DeploymentRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO git_deployments (uid, site_id, source_id, commit_sha, status, trigger_kind, release_dir, log)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.SiteID, r.SourceID, r.CommitSHA, r.Status, r.TriggerKind, r.ReleaseDir, r.Log)
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

// UpdateDeployment persists a deployment's terminal state (status, commit, log,
// finish time). Targets the row by id.
func (s *GitStore) UpdateDeployment(ctx context.Context, r *git.DeploymentRecord) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE git_deployments SET status = ?, commit_sha = ?, release_dir = ?, log = ?, finished_at = ?
		 WHERE id = ?`,
		r.Status, r.CommitSHA, r.ReleaseDir, r.Log, r.FinishedAt, r.ID); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// ListDeployments returns a site's deployments, newest first.
func (s *GitStore) ListDeployments(ctx context.Context, siteID int64, limit int) ([]git.DeploymentRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var recs []git.DeploymentRecord
	if err := s.db.SelectContext(ctx, &recs,
		`SELECT `+gitDeploymentCols+` FROM git_deployments WHERE site_id = ? ORDER BY id DESC LIMIT ?`,
		siteID, limit); err != nil {
		return nil, errx.Internal(err)
	}
	return recs, nil
}

// GetDeploymentByUID returns a deployment by UID, or a not-found error.
func (s *GitStore) GetDeploymentByUID(ctx context.Context, uid string) (*git.DeploymentRecord, error) {
	var rec git.DeploymentRecord
	err := s.db.GetContext(ctx, &rec, `SELECT `+gitDeploymentCols+` FROM git_deployments WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("deployment_not_found", "No such deployment.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}
