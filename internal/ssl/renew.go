package ssl

import (
	"context"
	"log/slog"
	"time"
)

// Renewal defaults.
const (
	// DefaultRenewWindow is how close to expiry a certificate is renewed. Let's
	// Encrypt certs live 90 days, so 30 days leaves ample retry room.
	DefaultRenewWindow = 30 * 24 * time.Hour
	// DefaultRenewInterval is how often the renewer sweeps for due certificates.
	DefaultRenewInterval = 12 * time.Hour
)

// Renewer periodically re-issues certificates that are close to expiry. It
// repeats whatever flow originally produced the certificate: HTTP-01 when a
// webroot was recorded, DNS-01 otherwise (including wildcards), and a fresh
// self-signed for self-signed certs. Uploaded (custom) certs are never touched —
// HeroPanel has no way to obtain a new one.
type Renewer struct {
	svc      *Service
	log      *slog.Logger
	interval time.Duration
	window   time.Duration
	now      func() time.Time // injectable for tests
}

// NewRenewer constructs a Renewer with the default schedule.
func NewRenewer(svc *Service, log *slog.Logger) *Renewer {
	if log == nil {
		log = slog.Default()
	}
	return &Renewer{
		svc: svc, log: log,
		interval: DefaultRenewInterval,
		window:   DefaultRenewWindow,
		now:      time.Now,
	}
}

// WithSchedule overrides the sweep interval and renewal window.
func (r *Renewer) WithSchedule(interval, window time.Duration) *Renewer {
	r.interval, r.window = interval, window
	return r
}

// Run sweeps for due certificates until ctx is cancelled. It runs one sweep
// immediately so a restart picks up anything already due.
func (r *Renewer) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		if n, err := r.RenewDue(ctx); err != nil {
			r.log.Warn("certificate renewal sweep failed", "err", err)
		} else if n > 0 {
			r.log.Info("certificates renewed", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RenewDue renews every certificate expiring within the window and returns how
// many were successfully renewed. A failure on one certificate is logged and
// does not stop the others.
func (r *Renewer) RenewDue(ctx context.Context) (int, error) {
	cutoff := fmtTime(r.now().Add(r.window))
	due, err := r.svc.repo.ListDueForRenewal(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	renewed := 0
	for i := range due {
		rec := due[i]
		if err := r.renewOne(ctx, &rec); err != nil {
			r.log.Warn("certificate renewal failed",
				"common_name", rec.CommonName, "provider", rec.Provider, "err", err)
			continue
		}
		renewed++
	}
	return renewed, nil
}

// renewOne re-issues a single certificate using the flow it was created with.
// Insert upserts by common_name, so the existing row is replaced in place.
func (r *Renewer) renewOne(ctx context.Context, rec *Record) error {
	switch rec.Provider {
	case ProviderSelfSigned:
		_, err := r.svc.IssueSelfSigned(ctx, rec.OwnerID, rec.CommonName)
		return err
	case ProviderLetsEncrypt:
		if rec.Webroot != "" {
			_, err := r.svc.Issue(ctx, rec.OwnerID, rec.CommonName, rec.Webroot)
			return err
		}
		_, err := r.svc.IssueDNS(ctx, rec.OwnerID, rec.CommonName)
		return err
	default:
		return nil // custom uploads cannot be renewed automatically
	}
}
