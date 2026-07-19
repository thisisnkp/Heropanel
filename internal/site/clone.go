package site

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// CloneInput describes the copy to make of an existing site.
type CloneInput struct {
	// SourceUID is the site being copied.
	SourceUID string `json:"source_uid"`
	// Name and PrimaryDomain belong to the new site. A clone is a separate site
	// in every respect — its own Linux user, its own tree, its own vhost — so it
	// needs its own domain. Sharing one would give two vhosts the same map entry
	// and the web server would answer with whichever it saw first.
	Name          string `json:"name"`
	PrimaryDomain string `json:"primary_domain"`
	OwnerID       int64  `json:"owner_id"`
}

// Clone copies a site synchronously (used directly and by tests).
func (s *Service) Clone(ctx context.Context, in CloneInput) (*Site, error) {
	return s.RunClone(ctx, in, job.Noop)
}

// RunClone provisions a new site and copies the source's content into it.
//
// What is copied is the **document root, and only that**. Not the database: a
// cloned site pointing at the original's database is not a staging copy, it is a
// second set of hands on live customer data, and the first write from the
// "staging" site is a production incident. Not the git source either — a clone
// is a snapshot of files as they are, and inheriting the origin's webhook would
// have the next push to the original silently overwrite the copy. Not the app
// runtime, whose port would collide. Each of those is the operator's to set up
// on the clone deliberately, which is the point.
//
// This is the body executed by the async "site.clone" job handler. It has to be
// able to run there: copying a real document root takes minutes, and no HTTP
// request survives that.
func (s *Service) RunClone(ctx context.Context, in CloneInput, p job.Progress) (*Site, error) {
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; sites cannot be cloned.")
	}

	p.Report(5, "loading source site")
	src, err := s.repo.GetByUID(ctx, in.SourceUID)
	if err != nil {
		return nil, err
	}
	if !src.HomeDir.Valid || src.HomeDir.String == "" {
		return nil, errx.New(errx.KindConflict, "source_not_provisioned",
			"The source site is not provisioned; there is nothing to clone.")
	}

	// The clone inherits what makes the copy behave like the original — its type
	// and how content reaches it — and nothing that identifies it.
	create := CreateInput{
		Name:          in.Name,
		PrimaryDomain: in.PrimaryDomain,
		Type:          Type(src.Type),
		DeployMode:    DeployMode(src.DeployMode),
		OwnerID:       in.OwnerID,
	}
	if err := validateCreate(&create); err != nil {
		return nil, err
	}

	p.Report(10, "provisioning the clone")
	// Report the source's provisioning progress into the first half of the bar:
	// creating the site is genuinely most of the work for a small site, and a bar
	// that sits at 10% through all of it looks hung.
	created, err := s.RunCreate(ctx, create, job.ScaleProgress(p, 10, 70))
	if err != nil {
		return nil, err
	}

	dst, err := s.repo.GetByUID(ctx, created.UID)
	if err != nil {
		return nil, err
	}

	p.Report(75, "copying content")
	_, err = s.broker.Invoke(ctx, "site.copy_tree", map[string]any{
		"src_root": src.HomeDir.String,
		"dst_root": dst.HomeDir.String,
		"username": dst.LinuxUser.String,
	})
	if err != nil {
		// The clone exists but is empty, which is a worse answer than no clone:
		// the operator would find a site in the list, assume it holds a copy, and
		// only discover otherwise later. Take it back down.
		p.Report(80, "copy failed; removing the incomplete clone")
		_ = s.RunDelete(ctx, created.UID, job.Noop)
		return nil, err
	}

	p.Report(90, "matching PHP version")
	s.clonePHPVersion(ctx, src, created.UID)

	p.Report(100, "cloned")
	return s.Get(ctx, created.UID)
}

// clonePHPVersion gives the clone the same PHP version as its source.
//
// A failure is not fatal. The clone exists and has the content, which is what
// was asked for; it simply runs the default PHP version, which is visible in the
// UI and one click to change. Destroying a good copy over this would be worse.
func (s *Service) clonePHPVersion(ctx context.Context, src *Record, dstUID string) {
	if s.php == nil || src.Type != string(TypePHP) {
		return
	}
	pool, err := s.php.GetBySiteID(ctx, src.ID)
	if err != nil || pool.PHPVersion == "" {
		return
	}
	_, _ = s.SetPHPVersion(ctx, dstUID, pool.PHPVersion)
}
