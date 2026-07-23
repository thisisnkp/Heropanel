// Package mail is the email module: virtual domains, accounts and
// aliases/forwarders on Postfix + Dovecot. The panel's database is the source
// of truth; every change re-renders the complete flat maps and applies them
// through the broker (render-all, apply, rollback — the webserver discipline).
// The MTAs never read the database, so mail keeps flowing when the panel is
// down. In-core, satellite-ready like its siblings.
package mail

import (
	"context"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
	"github.com/thisisnkp/heropanel/pkg/secrets"
)

// Local parts are stored lowercase; the panel normalises on the way in.
var (
	reLocalPart = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,63}$`)
	reDomain    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
)

// API views.
type Domain struct {
	UID          string `json:"uid"`
	Domain       string `json:"domain"`
	DKIMSelector string `json:"dkim_selector"`
	DKIMPublic   string `json:"dkim_public,omitempty"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
}

type Account struct {
	UID       string `json:"uid"`
	Address   string `json:"address"`
	LocalPart string `json:"local_part"`
	QuotaMB   int    `json:"quota_mb"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type Alias struct {
	UID         string `json:"uid"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	CreatedAt   string `json:"created_at"`
}

// Persistence rows.
type DomainRecord struct {
	ID           int64  `db:"id"`
	UID          string `db:"uid"`
	OwnerID      int64  `db:"owner_id"`
	Domain       string `db:"domain"`
	DKIMSelector string `db:"dkim_selector"`
	DKIMPrivate  string `db:"dkim_private"` // sealed; write-only
	DKIMPublic   string `db:"dkim_public"`
	Status       string `db:"status"`
	CreatedAt    string `db:"created_at"`
}

type AccountRecord struct {
	ID           int64  `db:"id"`
	UID          string `db:"uid"`
	DomainID     int64  `db:"domain_id"`
	LocalPart    string `db:"local_part"`
	PasswordHash string `db:"password_hash"`
	QuotaMB      int    `db:"quota_mb"`
	Status       string `db:"status"`
	CreatedAt    string `db:"created_at"`
}

type AliasRecord struct {
	ID          int64  `db:"id"`
	UID         string `db:"uid"`
	DomainID    int64  `db:"domain_id"`
	Source      string `db:"source"`
	Destination string `db:"destination"`
	CreatedAt   string `db:"created_at"`
}

// Repo is the persistence contract (implemented by internal/repository).
type Repo interface {
	InsertDomain(ctx context.Context, r *DomainRecord) error
	ListDomains(ctx context.Context, ownerID int64) ([]DomainRecord, error)
	GetDomainByUID(ctx context.Context, uid string) (*DomainRecord, error)
	UpdateDomainDKIM(ctx context.Context, id int64, selector, sealedPrivate, public string) error
	DeleteDomain(ctx context.Context, uid string) error

	InsertAccount(ctx context.Context, r *AccountRecord) error
	ListAccounts(ctx context.Context, domainID int64) ([]AccountRecord, error)
	GetAccountByUID(ctx context.Context, uid string) (*AccountRecord, error)
	UpdateAccountPassword(ctx context.Context, id int64, hash string) error
	UpdateAccountQuota(ctx context.Context, id int64, quotaMB int) error
	UpdateAccountStatus(ctx context.Context, id int64, status string) error
	DeleteAccount(ctx context.Context, uid string) error

	InsertAlias(ctx context.Context, r *AliasRecord) error
	ListAliases(ctx context.Context, domainID int64) ([]AliasRecord, error)
	GetAliasByUID(ctx context.Context, uid string) (*AliasRecord, error)
	DeleteAlias(ctx context.Context, uid string) error

	// Render feeds: the complete desired state, deterministically ordered.
	RenderAccounts(ctx context.Context) ([]RenderAccountRow, error)
	RenderAliases(ctx context.Context) ([]RenderAliasRow, error)
	AllDomainNames(ctx context.Context) ([]string, error)
}

// Service orchestrates the mail module.
type Service struct {
	repo   Repo
	broker broker.Gateway
	// hash is swappable for tests (bcrypt is deliberately slow).
	hash func(password string) (string, error)

	cipher       *secrets.Cipher // seals DKIM private keys; nil = DKIM disabled
	dns          DNSProvider     // wires records into managed zones; nil = display-only
	resolverAddr string          // pinned resolver for CheckDNS ("" = system)
}

// NewService constructs the mail service.
func NewService(repo Repo, gw broker.Gateway) *Service {
	return &Service{repo: repo, broker: gw, hash: bcryptHash}
}

// WithSecrets enables DKIM: private keys are sealed with this cipher before
// they touch the database. No cipher, no DKIM — never a plaintext key at rest.
func (s *Service) WithSecrets(c *secrets.Cipher) *Service { s.cipher = c; return s }

// WithDNS wires MX/SPF/DKIM/DMARC records into panel-managed zones.
func (s *Service) WithDNS(p DNSProvider) *Service { s.dns = p; return s }

// WithResolver pins the resolver CheckDNS queries (host:port). Empty keeps the
// system resolver.
func (s *Service) WithResolver(addr string) *Service { s.resolverAddr = addr; return s }

// bcryptHash produces a Dovecot BLF-CRYPT credential. bcrypt because it is
// already in the tree (x/crypto) and verified by libxcrypt on every current
// distro; argon2id support in dovecot depends on how the distro built it.
func bcryptHash(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return "{BLF-CRYPT}" + string(h), nil
}

// Available reports whether the module can operate.
func (s *Service) Available() bool { return s != nil && s.broker != nil && s.repo != nil }

func (s *Service) requireAvailable() error {
	if s.Available() {
		return nil
	}
	return errx.New(errx.KindUnavailable, "mail_unavailable",
		"Mail management needs the broker and a datastore.")
}

// apply renders the complete desired state and hands it to the broker.
func (s *Service) apply(ctx context.Context) error {
	domains, err := s.repo.AllDomainNames(ctx)
	if err != nil {
		return err
	}
	accounts, err := s.repo.RenderAccounts(ctx)
	if err != nil {
		return err
	}
	aliases, err := s.repo.RenderAliases(ctx)
	if err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, "mail.apply", map[string]any{
		"domains":   RenderDomains(domains),
		"mailboxes": RenderMailboxes(accounts),
		"aliases":   RenderAliases(aliases),
		"users":     RenderUsers(accounts),
	})
	return err
}

// ── domains ──────────────────────────────────────────────────────────────────

// CreateDomain provisions the host (idempotent) and adds a mail domain.
func (s *Service) CreateDomain(ctx context.Context, ownerID int64, domain string) (*Domain, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !reDomain.MatchString(domain) || len(domain) > 253 {
		return nil, errx.Validation("invalid_domain", "Not a valid mail domain.")
	}
	// Provision before the row exists: a host that cannot be prepared must not
	// accumulate domains that silently do not work.
	if _, err := s.broker.Invoke(ctx, "mail.provision", map[string]any{}); err != nil {
		return nil, err
	}
	rec := &DomainRecord{UID: idgen.NewULID(), OwnerID: ownerID, Domain: domain, DKIMSelector: dkimSelector, Status: "active"}
	if err := s.repo.InsertDomain(ctx, rec); err != nil {
		return nil, err
	}
	if err := s.apply(ctx); err != nil {
		// Roll the row back rather than leaving a domain the MTA never learned.
		_ = s.repo.DeleteDomain(ctx, rec.UID)
		return nil, err
	}
	// DKIM: generated + sealed when a data key exists; a failure here fails the
	// create loudly (outbound mail without DKIM lands in spam — that is broken,
	// not degraded).
	if err := s.enableDKIM(ctx, rec); err != nil {
		_ = s.repo.DeleteDomain(ctx, rec.UID)
		_ = s.apply(ctx)
		return nil, err
	}
	// DNS wiring is best-effort by design: a domain on external DNS is normal,
	// and a wiring failure must not strand a domain that otherwise fully works
	// — the live DNS check is the honest surface for a record that is missing,
	// whatever the reason.
	_, _ = s.wireDNS(ctx, rec)
	return domainView(rec), nil
}

// ListDomains lists mail domains (ownerID 0 = all).
func (s *Service) ListDomains(ctx context.Context, ownerID int64) ([]Domain, error) {
	recs, err := s.repo.ListDomains(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	out := make([]Domain, len(recs))
	for i := range recs {
		out[i] = *domainView(&recs[i])
	}
	return out, nil
}

// GetDomain returns one domain with its accounts and aliases.
func (s *Service) GetDomain(ctx context.Context, uid string) (*Domain, []Account, []Alias, error) {
	rec, err := s.repo.GetDomainByUID(ctx, uid)
	if err != nil {
		return nil, nil, nil, err
	}
	accts, err := s.repo.ListAccounts(ctx, rec.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	aliases, err := s.repo.ListAliases(ctx, rec.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	av := make([]Account, len(accts))
	for i := range accts {
		av[i] = *accountView(&accts[i], rec.Domain)
	}
	lv := make([]Alias, len(aliases))
	for i := range aliases {
		lv[i] = *aliasView(&aliases[i], rec.Domain)
	}
	return domainView(rec), av, lv, nil
}

// DeleteDomain removes a domain (accounts and aliases cascade), re-applies,
// and optionally purges the stored mail. Purge is explicit — deleting a
// domain's configuration and destroying its mailboxes are different acts.
func (s *Service) DeleteDomain(ctx context.Context, uid string, purge bool) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	rec, err := s.repo.GetDomainByUID(ctx, uid)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteDomain(ctx, uid); err != nil {
		return err
	}
	if err := s.apply(ctx); err != nil {
		return err
	}
	// The signer must forget the domain's key too.
	if err := s.applyDKIM(ctx); err != nil {
		return err
	}
	if purge {
		if _, err := s.broker.Invoke(ctx, "mail.purge", map[string]any{"domain": rec.Domain}); err != nil {
			return err
		}
	}
	return nil
}

// ── accounts ─────────────────────────────────────────────────────────────────

// CreateAccount adds a mailbox to a domain.
func (s *Service) CreateAccount(ctx context.Context, domainUID, localPart, password string, quotaMB int) (*Account, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	dom, err := s.repo.GetDomainByUID(ctx, domainUID)
	if err != nil {
		return nil, err
	}
	localPart = strings.ToLower(strings.TrimSpace(localPart))
	if !reLocalPart.MatchString(localPart) {
		return nil, errx.Validation("invalid_local_part",
			"Mailbox names use lowercase letters, digits and . _ + - (starting alphanumeric).")
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	if quotaMB == 0 {
		quotaMB = 1024
	}
	if quotaMB < 1 || quotaMB > 1024*100 {
		return nil, errx.Validation("invalid_quota", "Quota must be between 1 MB and 100 GB.")
	}
	hash, err := s.hash(password)
	if err != nil {
		return nil, errx.Internal(err)
	}
	rec := &AccountRecord{
		UID: idgen.NewULID(), DomainID: dom.ID, LocalPart: localPart,
		PasswordHash: hash, QuotaMB: quotaMB, Status: "active",
	}
	if err := s.repo.InsertAccount(ctx, rec); err != nil {
		return nil, err
	}
	if err := s.apply(ctx); err != nil {
		_ = s.repo.DeleteAccount(ctx, rec.UID)
		return nil, err
	}
	return accountView(rec, dom.Domain), nil
}

// SetAccountPassword replaces a mailbox password. The old hash is never shown;
// the new one is stored hashed — write-only both ways.
func (s *Service) SetAccountPassword(ctx context.Context, uid, password string) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	rec, err := s.repo.GetAccountByUID(ctx, uid)
	if err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := s.hash(password)
	if err != nil {
		return errx.Internal(err)
	}
	if err := s.repo.UpdateAccountPassword(ctx, rec.ID, hash); err != nil {
		return err
	}
	return s.apply(ctx)
}

// SetAccountQuota changes a mailbox quota.
func (s *Service) SetAccountQuota(ctx context.Context, uid string, quotaMB int) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	rec, err := s.repo.GetAccountByUID(ctx, uid)
	if err != nil {
		return err
	}
	if quotaMB < 1 || quotaMB > 1024*100 {
		return errx.Validation("invalid_quota", "Quota must be between 1 MB and 100 GB.")
	}
	if err := s.repo.UpdateAccountQuota(ctx, rec.ID, quotaMB); err != nil {
		return err
	}
	return s.apply(ctx)
}

// SetAccountStatus suspends or reactivates a mailbox. Suspension blocks logins
// (the account leaves the passwd-file) but the mailbox KEEPS receiving —
// suspending must not bounce mail.
func (s *Service) SetAccountStatus(ctx context.Context, uid, status string) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	if status != "active" && status != "suspended" {
		return errx.Validation("invalid_status", "Status must be active or suspended.")
	}
	rec, err := s.repo.GetAccountByUID(ctx, uid)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateAccountStatus(ctx, rec.ID, status); err != nil {
		return err
	}
	return s.apply(ctx)
}

// DeleteAccount removes a mailbox, optionally purging its stored mail.
func (s *Service) DeleteAccount(ctx context.Context, domainUID, uid string, purge bool) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	dom, err := s.repo.GetDomainByUID(ctx, domainUID)
	if err != nil {
		return err
	}
	rec, err := s.repo.GetAccountByUID(ctx, uid)
	if err != nil {
		return err
	}
	if rec.DomainID != dom.ID {
		return errx.NotFound("account_not_found", "No such mailbox in this domain.")
	}
	if err := s.repo.DeleteAccount(ctx, uid); err != nil {
		return err
	}
	if err := s.apply(ctx); err != nil {
		return err
	}
	if purge {
		if _, err := s.broker.Invoke(ctx, "mail.purge", map[string]any{
			"domain": dom.Domain, "local_part": rec.LocalPart,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ── aliases ──────────────────────────────────────────────────────────────────

// CreateAlias adds an alias (internal destination) or forwarder (external).
func (s *Service) CreateAlias(ctx context.Context, domainUID, source, destination string) (*Alias, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	dom, err := s.repo.GetDomainByUID(ctx, domainUID)
	if err != nil {
		return nil, err
	}
	source = strings.ToLower(strings.TrimSpace(source))
	destination = strings.ToLower(strings.TrimSpace(destination))
	if !reLocalPart.MatchString(source) {
		return nil, errx.Validation("invalid_source", "Alias names look like mailbox names (the part before @).")
	}
	if !validAddress(destination) {
		return nil, errx.Validation("invalid_destination", "The destination must be a full email address.")
	}
	if source+"@"+dom.Domain == destination {
		return nil, errx.Validation("alias_loop", "An alias cannot point at itself.")
	}
	rec := &AliasRecord{UID: idgen.NewULID(), DomainID: dom.ID, Source: source, Destination: destination}
	if err := s.repo.InsertAlias(ctx, rec); err != nil {
		return nil, err
	}
	if err := s.apply(ctx); err != nil {
		_ = s.repo.DeleteAlias(ctx, rec.UID)
		return nil, err
	}
	return aliasView(rec, dom.Domain), nil
}

// DeleteAlias removes one alias pair.
func (s *Service) DeleteAlias(ctx context.Context, domainUID, uid string) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	dom, err := s.repo.GetDomainByUID(ctx, domainUID)
	if err != nil {
		return err
	}
	rec, err := s.repo.GetAliasByUID(ctx, uid)
	if err != nil {
		return err
	}
	if rec.DomainID != dom.ID {
		return errx.NotFound("alias_not_found", "No such alias in this domain.")
	}
	if err := s.repo.DeleteAlias(ctx, uid); err != nil {
		return err
	}
	return s.apply(ctx)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func validatePassword(p string) error {
	if len(p) < 8 || len(p) > 128 {
		return errx.Validation("invalid_password", "Mailbox passwords are 8–128 characters.")
	}
	return nil
}

// validAddress accepts local@fqdn with the module's own charsets.
func validAddress(a string) bool {
	at := strings.LastIndex(a, "@")
	if at <= 0 || at == len(a)-1 {
		return false
	}
	return reLocalPart.MatchString(a[:at]) && reDomain.MatchString(a[at+1:]) && len(a) <= 320
}

func domainView(r *DomainRecord) *Domain {
	return &Domain{
		UID: r.UID, Domain: r.Domain, DKIMSelector: r.DKIMSelector,
		DKIMPublic: r.DKIMPublic, Status: r.Status, CreatedAt: r.CreatedAt,
	}
}

func accountView(r *AccountRecord, domain string) *Account {
	return &Account{
		UID: r.UID, Address: r.LocalPart + "@" + domain, LocalPart: r.LocalPart,
		QuotaMB: r.QuotaMB, Status: r.Status, CreatedAt: r.CreatedAt,
	}
}

func aliasView(r *AliasRecord, domain string) *Alias {
	return &Alias{
		UID: r.UID, Source: r.Source + "@" + domain, Destination: r.Destination, CreatedAt: r.CreatedAt,
	}
}
