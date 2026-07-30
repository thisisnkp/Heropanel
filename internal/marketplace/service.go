package marketplace

import (
	"context"
	"log/slog"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/proto"
)

// Module lifecycle states, as persisted by the store. Installing a module
// records it as installed; a separate enable step activates it, and disable
// parks it without uninstalling. Keeping installed and enabled distinct lets an
// operator stage a module and turn it on deliberately, and turn it off again
// without losing the install.
const (
	StateInstalled = "installed"
	StateEnabled   = "enabled"
	StateDisabled  = "disabled"
)

// InstalledModule is the store's record of a module the operator has installed.
// It is the marketplace's own bookkeeping, separate from the runtime registry
// (which advertises capabilities of modules with a live provider) — a satellite
// module can be installed and enabled here before the deferred process-supervisor
// tier exists to run it.
type InstalledModule struct {
	Slug         string `db:"slug" json:"slug"`
	Name         string `db:"name" json:"name"`
	Version      string `db:"version" json:"version"`
	Category     string `db:"category" json:"category"`
	State        string `db:"state" json:"state"`
	PublisherKey string `db:"publisher_key" json:"publisher_key"`
	InstalledAt  string `db:"installed_at" json:"installed_at"`
	UpdatedAt    string `db:"updated_at" json:"updated_at"`
}

// Store persists installed-module records. The interface lives here so the
// service depends on the capability, not on the repository package; the concrete
// implementation is repository.ModuleStore.
type Store interface {
	Upsert(ctx context.Context, m InstalledModule) error
	SetState(ctx context.Context, slug, state string) error
	Get(ctx context.Context, slug string) (*InstalledModule, error)
	List(ctx context.Context) ([]InstalledModule, error)
	Delete(ctx context.Context, slug string) error
}

// Service ties the catalog, the trust keyring, and the install store into the
// operator-facing marketplace: browse what is offered (with each entry's trust
// verdict), install a verified module, enable/disable it, uninstall it.
type Service struct {
	keyring *Keyring
	catalog *Catalog
	store   Store
	log     *slog.Logger
}

// NewService constructs the marketplace service. keyring and catalog may be
// empty (nothing to install, or nothing trusted to install it); store may be nil
// on a panel with no datastore, in which case the service reports itself
// disabled and mounts no routes.
func NewService(keyring *Keyring, catalog *Catalog, store Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{keyring: keyring, catalog: catalog, store: store, log: log}
}

// Enabled reports whether the marketplace is wired. A nil service or a service
// without a store is disabled — the surface gates on this so the reflection guard
// can hold a present-but-inert service without it imposing anything.
func (s *Service) Enabled() bool { return s != nil && s.store != nil }

// TrustAnchored reports whether at least one publisher key is pinned. When false,
// nothing can be installed — the UI surfaces this so the operator knows the fix
// is to pin a key, not that the catalog is broken.
func (s *Service) TrustAnchored() bool { return s != nil && !s.keyring.Empty() }

// CatalogEntry is one offered module plus this panel's verdict on it: whether a
// trusted publisher signed it, and whether it is already installed and in what
// state.
type CatalogEntry struct {
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Category       string   `json:"category"`
	Description    string   `json:"description"`
	Icon           string   `json:"icon"`
	Capabilities   []string `json:"capabilities"`
	RequiresBroker []string `json:"requires_broker"`
	// Verified is true only when the manifest is well-formed and signed by a
	// trusted key. Everything about install gates on this.
	Verified     bool   `json:"verified"`
	PublisherKey string `json:"publisher_key,omitempty"`
	// VerifyError explains, in the operator's terms, why an entry is not
	// verified — an unsigned module and an untrusted-key module read differently.
	VerifyError string `json:"verify_error,omitempty"`
	Installed   bool   `json:"installed"`
	State       string `json:"state,omitempty"`
}

// Browse returns the catalog with each entry's trust verdict and install state.
// It never fails an entry for another's sake: an untrusted or malformed module is
// listed with Verified=false and its reason, so the operator sees the whole feed
// and can tell the safe entries from the rest.
func (s *Service) Browse(ctx context.Context) ([]CatalogEntry, error) {
	installed := map[string]InstalledModule{}
	if s.store != nil {
		list, err := s.store.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, m := range list {
			installed[m.Slug] = m
		}
	}
	mods := []proto.Manifest{}
	if s.catalog != nil {
		mods = s.catalog.Modules
	}
	out := make([]CatalogEntry, 0, len(mods))
	for _, m := range mods {
		e := s.describe(m)
		if inst, ok := installed[m.Metadata.Slug]; ok {
			e.Installed = true
			e.State = inst.State
		}
		out = append(out, e)
	}
	return out, nil
}

// describe computes the verdict for one manifest: valid, then trusted.
func (s *Service) describe(m proto.Manifest) CatalogEntry {
	e := CatalogEntry{
		Slug:           m.Metadata.Slug,
		Name:           m.Metadata.Name,
		Version:        m.Metadata.Version,
		Category:       m.Metadata.Category,
		Description:    m.Metadata.Description,
		Icon:           m.Metadata.Icon,
		Capabilities:   m.Spec.Capabilities,
		RequiresBroker: m.Spec.RequiresBroker,
	}
	if e.Capabilities == nil {
		e.Capabilities = []string{}
	}
	if e.RequiresBroker == nil {
		e.RequiresBroker = []string{}
	}
	if err := m.Validate(); err != nil {
		e.VerifyError = err.Error()
		return e
	}
	keyID, err := s.keyring.VerifyManifest(m)
	if err != nil {
		e.VerifyError = err.Error()
		return e
	}
	e.Verified = true
	e.PublisherKey = keyID
	return e
}

// Install verifies a catalog module and records it as installed. Verification is
// the gate, not a warning: a manifest that is malformed or not signed by a
// trusted key is refused before any record is written, because installing is the
// step after which the module's code is meant to run.
func (s *Service) Install(ctx context.Context, slug string) (*InstalledModule, error) {
	m, ok := s.catalog.find(slug)
	if !ok {
		return nil, errx.NotFound("module_not_found", "No such module in the catalog.")
	}
	if err := m.Validate(); err != nil {
		return nil, errx.New(errx.KindValidation, "module_invalid", err.Error())
	}
	keyID, err := s.keyring.VerifyManifest(m)
	if err != nil {
		// A failed trust check is a refusal to proceed, surfaced as forbidden: the
		// module is well-formed but nobody the operator trusts vouched for it.
		return nil, errx.Forbidden("module_unverified", err.Error())
	}
	rec := InstalledModule{
		Slug:         m.Metadata.Slug,
		Name:         m.Metadata.Name,
		Version:      m.Metadata.Version,
		Category:     m.Metadata.Category,
		State:        StateInstalled,
		PublisherKey: keyID,
	}
	if err := s.store.Upsert(ctx, rec); err != nil {
		return nil, err
	}
	s.log.Info("module installed", "slug", rec.Slug, "version", rec.Version, "publisher", keyID)
	return s.store.Get(ctx, slug)
}

// SetEnabled toggles an installed module between enabled and disabled. It refuses
// a slug that is not installed (Get returns not-found), so enabling is always a
// transition of a real record rather than a way to conjure one.
func (s *Service) SetEnabled(ctx context.Context, slug string, enabled bool) (*InstalledModule, error) {
	if _, err := s.store.Get(ctx, slug); err != nil {
		return nil, err
	}
	state := StateDisabled
	if enabled {
		state = StateEnabled
	}
	if err := s.store.SetState(ctx, slug, state); err != nil {
		return nil, err
	}
	s.log.Info("module state changed", "slug", slug, "state", state)
	return s.store.Get(ctx, slug)
}

// Uninstall removes an installed module's record. It refuses an unknown slug so
// the caller gets a clear not-found rather than a silent no-op.
func (s *Service) Uninstall(ctx context.Context, slug string) error {
	if _, err := s.store.Get(ctx, slug); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, slug); err != nil {
		return err
	}
	s.log.Info("module uninstalled", "slug", slug)
	return nil
}

// Installed lists the installed-module records — the operator's inventory,
// independent of what the catalog currently offers.
func (s *Service) Installed(ctx context.Context) ([]InstalledModule, error) {
	list, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []InstalledModule{}
	}
	return list, nil
}
