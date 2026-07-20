// Package terminal is the in-core web terminal: it gives an operator an
// interactive shell on a site, running as that site's Linux user.
//
// hpd cannot start such a shell itself — it is unprivileged and cannot become
// another user — so the session is opened by the root broker over an upgraded
// (streaming) broker connection, and this package is the bridge between that
// stream and the browser's WebSocket. It holds no privilege of its own; what it
// contributes is the *policy* half: which site, which user, and whether the
// caller may have a terminal at all.
package terminal

import (
	"context"

	brokerclient "github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Capability is the broker capability that opens an interactive session.
const Capability = "terminal.open"

// Default window size used when the client does not send one.
const (
	DefaultCols = 80
	DefaultRows = 24
)

// SiteRef is the identity + paths a terminal session needs, resolved by UID.
type SiteRef struct {
	ID         int64
	UID        string
	LinuxUser  string
	HomeDir    string
	DeployMode string
}

// Sites resolves a site UID to its identity and paths.
type Sites interface {
	Resolve(ctx context.Context, siteUID string) (*SiteRef, error)
}

// Service opens terminal sessions for sites.
type Service struct {
	sites  Sites
	broker brokerclient.StreamGateway
}

// NewService constructs the terminal service.
func NewService(sites Sites, broker brokerclient.StreamGateway) *Service {
	return &Service{sites: sites, broker: broker}
}

// Available reports whether terminals can be opened at all (the broker is the
// only way to run a shell as another user).
func (s *Service) Available() bool { return s.broker != nil }

// Site resolves a site UID, so callers that need the site's identity without
// opening a session (listing its recordings, say) do not have to reach past this
// package for a second resolver.
func (s *Service) Site(ctx context.Context, siteUID string) (*SiteRef, error) {
	return s.sites.Resolve(ctx, siteUID)
}

// Open starts a session on the given site. cwd is relative to the site home and
// clamped under it by the broker. The caller owns the returned stream and must
// Close it when the browser disconnects — that is what kills the shell.
func (s *Service) Open(ctx context.Context, siteUID, cwd string, cols, rows uint16) (brokerclient.Stream, *SiteRef, error) {
	if s.broker == nil {
		return nil, nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The terminal requires the privileged broker, which is not configured.")
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, nil, err
	}
	// A terminal is only meaningful where the site actually has a Linux account
	// and a home to land in. Docker-hosted sites have neither on the host, so
	// there is nothing here to attach a shell to.
	if ref.LinuxUser == "" || ref.HomeDir == "" {
		return nil, nil, errx.New(errx.KindUnavailable, "site_not_provisioned",
			"This site has no Linux user yet, so a terminal cannot be opened.")
	}
	if cols == 0 {
		cols = DefaultCols
	}
	if rows == 0 {
		rows = DefaultRows
	}

	stream, err := s.broker.OpenStream(ctx, Capability, map[string]any{
		"username": ref.LinuxUser,
		"root":     ref.HomeDir,
		"cwd":      cwd,
		"cols":     cols,
		"rows":     rows,
	})
	if err != nil {
		return nil, nil, err
	}
	return stream, ref, nil
}
