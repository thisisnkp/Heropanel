// Package keyring exposes the rotating data-key envelope (pkg/secrets) as a
// service: report the active generation and mint a new one. Master rotation
// (re-wrapping under a new master) is an out-of-band `npd` operation, not an API,
// because it requires a new master key the running panel must not choose for
// itself.
package keyring

import (
	"context"

	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/secrets"
)

// Store persists wrapped data keys (implemented by internal/repository).
type Store interface {
	List(ctx context.Context) ([]secrets.WrappedKey, error)
	Insert(ctx context.Context, wk secrets.WrappedKey) error
}

// Service rotates and reports the data-key envelope.
type Service struct {
	cipher *secrets.Cipher
	store  Store
}

// NewService constructs the keyring service.
func NewService(c *secrets.Cipher, s Store) *Service { return &Service{cipher: c, store: s} }

// Status is the API view of the envelope.
type Status struct {
	Available        bool `json:"available"`
	ActiveGeneration int  `json:"active_generation"`
	KeyCount         int  `json:"key_count"`
	LegacyKeyInUse   bool `json:"legacy_key_in_use"`
}

// Available reports whether rotation is possible (a master key + a datastore).
func (s *Service) Available() bool {
	return s != nil && s.cipher.Configured() && s.store != nil
}

// Status reports the active generation and how many data keys exist.
func (s *Service) Status(ctx context.Context) (*Status, error) {
	if s == nil || !s.cipher.Configured() {
		return &Status{Available: false}, nil
	}
	if s.store == nil {
		return &Status{Available: false, ActiveGeneration: s.cipher.ActiveGeneration()}, nil
	}
	keys, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	active := s.cipher.ActiveGeneration()
	return &Status{
		Available:        true,
		ActiveGeneration: active,
		KeyCount:         len(keys),
		LegacyKeyInUse:   active == 0,
	}, nil
}

// Rotate mints a new active data key and persists it. New values seal under it;
// existing values keep opening under their own generation.
func (s *Service) Rotate(ctx context.Context) (*Status, error) {
	if !s.Available() {
		return nil, errx.New(errx.KindUnavailable, "keyring_unavailable",
			"Data-key rotation needs a master key (NP_SECRET_KEY) and a datastore.")
	}
	wk, err := s.cipher.Rotate()
	if err != nil {
		return nil, errx.Wrap(err, errx.KindInternal, "rotate_failed", "Could not mint a new data key.")
	}
	if err := s.store.Insert(ctx, wk); err != nil {
		return nil, err
	}
	return s.Status(ctx)
}
