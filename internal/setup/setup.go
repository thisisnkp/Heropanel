// Package setup implements NexPanel's first-run infrastructure wizard. The
// panel runs itself on Go's net/http + Chi over a SQLite control-plane store, so
// it is reachable the moment the installer finishes — before any hosting stack
// exists. The wizard is what the operator sees next: it captures which webserver
// and database engine the panel should provision for hosted sites, and whether
// DNS and mail are managed here, then turns those choices into a provisioning
// plan applied on the host.
//
// The choices are persisted (a single row) and gate the panel: until the wizard
// is completed, the UI shows it instead of the dashboard. Turning choices into
// host changes (install packages, enable services) is the job of a Provisioner;
// the mapping from choices to steps (BuildPlan) is a pure function so it can be
// tested without a host.
package setup

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/internal/domain"
	"github.com/thisisnkp/nexpanel/internal/php"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Webserver identifies the webserver the panel provisions and manages for hosted
// sites.
//
// There are two, and the only reason there are two is that one of them costs
// money: OpenLiteSpeed is what every install runs, and LiteSpeed Enterprise is
// the same server with a licence, better PHP throughput and .htaccess handled
// natively. Nginx and Apache were removed — a panel that manages four web
// servers tests one and hopes about the rest.
type Webserver string

const (
	WebserverOpenLiteSpeed Webserver = "openlitespeed"
	WebserverLiteSpeed     Webserver = "litespeed_enterprise"
)

// DBEngine identifies the database engine the panel provisions and manages for
// hosted sites. It is distinct from the panel's own control-plane store, which
// is always SQLite.
//
// There is exactly one. It stays a named type with a catalog rather than
// becoming a bare constant because the wizard still reports it (the operator
// should be told what they are getting, even where they have no say), and
// because a stored selection from an older release can name an engine that no
// longer exists and has to be recognized to be repaired.
type DBEngine string

const (
	DBEngineMariaDB DBEngine = "mariadb"
)

// retiredWebservers and retiredDBEngines are values earlier releases accepted.
// They are kept only so a stored selection can be recognized and repaired —
// nothing may be provisioned or rendered for them.
var (
	retiredWebservers = map[string]bool{"nginx": true, "apache": true}
	retiredDBEngines  = map[string]bool{"mysql": true, "postgresql": true, "postgres": true}
)

// NormalizeStored repairs a selection loaded from the datastore.
//
// A panel installed before the stack was narrowed has "nginx" or "postgresql"
// sitting in its setup row. Validate would refuse it, and since the wizard's
// state gates the whole panel, that install would come up permanently stuck
// behind a form it cannot submit. So a retired value is rewritten to what the
// panel now actually runs, rather than treated as corrupt.
//
// MySQL is a rename, not a migration: it is wire-compatible with MariaDB and was
// always driven through the same code path. PostgreSQL is not — those databases
// are still on the host, and internal/database refuses to touch their rows so
// they cannot be silently re-aimed at MariaDB.
func (s *Selection) NormalizeStored() {
	if retiredWebservers[string(s.Webserver)] || s.Webserver == "" {
		s.Webserver = WebserverOpenLiteSpeed
	}
	if retiredDBEngines[string(s.DBEngine)] || s.DBEngine == "" {
		s.DBEngine = DBEngineMariaDB
	}
}

// Selection is the operator's answer to the wizard questions.
type Selection struct {
	Webserver  Webserver `json:"webserver"`
	DBEngine   DBEngine  `json:"db_engine"`
	ManageDNS  bool      `json:"manage_dns"`
	CreateMail bool      `json:"create_mail"`
	// LicenseKey is the LiteSpeed Enterprise serial. It is only meaningful when
	// Webserver is litespeed_enterprise (that engine is licensed); empty means a
	// trial, which LSWS also accepts.
	LicenseKey string `json:"license_key,omitempty"`
	// PanelDomain is this installation's own base domain — the parent a
	// throwaway site address is minted under (site-k3f9a2.<PanelDomain>), so an
	// operator can get something serving before they own a domain. Optional:
	// empty simply means the panel offers no temporary addresses.
	PanelDomain string `json:"panel_domain,omitempty"`
	// PanelIPv4 is this host's public address, and exists for exactly one
	// purpose: creating the `*.<PanelDomain>` wildcard record itself when the
	// base domain is a zone this panel hosts. The panel never infers its own
	// address — with this empty, temporary addresses still mint and the
	// operator is shown the record to add at whatever DNS they actually use.
	PanelIPv4 string `json:"panel_ipv4,omitempty"`
}

// State is the persisted setup state: the operator's selection plus whether the
// wizard has been completed and when.
type State struct {
	Selection
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Option describes one selectable choice for the wizard UI. Supported is false
// for choices whose provisioning backend is not yet implemented — the UI shows
// them (so the roadmap is visible) but disabled, and the service refuses to
// persist an unsupported choice rather than record a selection it cannot honor.
type Option struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Note      string `json:"note,omitempty"`
	Supported bool   `json:"supported"`
}

// Webservers is the webserver catalog, in display order. Both entries are fully
// managed: each has a config renderer in internal/webserver and an apply/reload
// path in the broker.
func Webservers() []Option {
	return []Option{
		{ID: string(WebserverOpenLiteSpeed), Label: "OpenLiteSpeed", Note: "included", Supported: true},
		{ID: string(WebserverLiteSpeed), Label: "LiteSpeed Enterprise", Note: "licence required", Supported: true},
	}
}

// DBEngines is the database-engine catalog: MariaDB, and nothing else.
//
// It stays a list of one rather than disappearing so the wizard can show the
// operator what their sites will run on. A choice of one is not a choice, but it
// is still information.
func DBEngines() []Option {
	return []Option{
		{ID: string(DBEngineMariaDB), Label: "MariaDB", Note: "included", Supported: true},
	}
}

// Baseline is the stack every install gets regardless of what the operator
// answers, for the wizard to display. It is not a set of choices — nothing here
// can be turned off — but the operator should still be told what is going on
// their machine.
//
// Two entries are listed without being installed by BuildPlan, and Supported
// separates them from the rest so the wizard can draw the difference:
//
//   - LiteSpeed Cache is Supported and has nothing to install. Page caching is
//     built into OpenLiteSpeed and LiteSpeed Enterprise, so it is part of the
//     stack the operator gets whether or not it is a package.
//   - PHP and Node are the default runtimes and their provisioning is not
//     written yet, so they are Supported: false. Listing them says what this
//     stack is; the flag stops the wizard from ticking them as done. Leaving
//     them out entirely would be the worse lie — a panel whose sites run PHP
//     describing a stack with no PHP in it.
func Baseline() []Option {
	return []Option{
		{ID: "lscache", Label: "LiteSpeed Cache", Note: "built into the web server", Supported: true},
		{ID: "php", Label: "PHP " + php.DefaultVersion, Note: "per-site FPM pools", Supported: false},
		{ID: "node", Label: "Node.js 24", Note: "app runtimes", Supported: false},
		{ID: "phpmyadmin", Label: "phpMyAdmin", Note: "database management", Supported: true},
		{ID: "clamav", Label: "ClamAV", Note: "malware scanning", Supported: true},
		{ID: "maldet", Label: "maldet", Note: "web-shell and PHP malware scanning", Supported: true},
		{ID: "fail2ban", Label: "Fail2Ban", Note: "brute-force blocking", Supported: true},
		{ID: "modsecurity", Label: "ModSecurity + OWASP CRS", Note: "web application firewall", Supported: true},
		{ID: "nftables", Label: "nftables", Note: "host firewall", Supported: true},
	}
}

func supported(opts []Option, id string) bool {
	for _, o := range opts {
		if o.ID == id {
			return o.Supported
		}
	}
	return false
}

// Validate rejects a selection with an unknown webserver or database engine —
// one that is not in the catalog at all. DNS and mail are booleans over modules
// that already exist, so any value is valid. It normalizes the panel domain and
// IP in place, so callers persist the same form the rest of the panel compares
// against; both are optional and only checked when present.
func (s *Selection) Validate() error {
	// "mysql" is accepted and rewritten: it names the same server MariaDB speaks
	// for, and a client sending it is not asking for anything the panel cannot do.
	// A retired *webserver* gets no such treatment — nginx and apache are real,
	// different servers the panel can no longer configure, and quietly giving the
	// operator OpenLiteSpeed instead would be answering a question they did not
	// ask. (A selection already stored from an older release is a different case;
	// see NormalizeStored.)
	if s.DBEngine == "" || s.DBEngine == "mysql" {
		s.DBEngine = DBEngineMariaDB
	}
	if !supported(Webservers(), string(s.Webserver)) {
		return errx.Validation("unknown_webserver",
			"This panel manages OpenLiteSpeed and LiteSpeed Enterprise.")
	}
	if !supported(DBEngines(), string(s.DBEngine)) {
		return errx.Validation("unknown_db_engine", "This panel manages MariaDB.")
	}

	s.PanelDomain = domain.NormalizeFQDN(s.PanelDomain)
	if s.PanelDomain != "" {
		// A wildcard passes ValidFQDN (it is a legal vhost name) but cannot be
		// a base: "site-a1b2.*.example.com" is not a hostname.
		if strings.HasPrefix(s.PanelDomain, "*.") || !domain.ValidFQDN(s.PanelDomain) {
			return errx.Validation("invalid_panel_domain",
				"The panel domain must be a plain hostname, e.g. panel.example.com.",
				errx.Field{Field: "panel_domain", Code: "invalid", Message: "invalid domain"})
		}
	}

	s.PanelIPv4 = strings.TrimSpace(s.PanelIPv4)
	if s.PanelIPv4 != "" {
		// Must be a v4 literal specifically: this address is written into an A
		// record, and net.ParseIP alone would happily accept an IPv6 one.
		if ip := net.ParseIP(s.PanelIPv4); ip == nil || ip.To4() == nil {
			return errx.Validation("invalid_panel_ipv4",
				"The panel IP must be an IPv4 address, e.g. 203.0.113.10.",
				errx.Field{Field: "panel_ipv4", Code: "invalid", Message: "invalid IPv4 address"})
		}
	}
	return nil
}

// ── provisioning plan ────────────────────────────────────────────────────────

// StepKind is the category of a provisioning step.
type StepKind string

const (
	StepPackage StepKind = "package" // install an OS package
	StepService StepKind = "service" // enable + start a system service
	StepModule  StepKind = "module"  // turn a panel module on or off
	// StepMaldet installs Linux Malware Detect from its upstream tarball. It is
	// its own kind because it is the one baseline component with no package: the
	// broker cannot resolve it to apt or dnf, so it goes through maldet.install.
	StepMaldet StepKind = "maldet"
)

// Step is one unit of provisioning work. Target is a package name, a service
// unit, or a module slug depending on Kind; Enable distinguishes on/off for
// modules (and is always true for package/service steps).
type Step struct {
	Kind   StepKind `json:"kind"`
	Target string   `json:"target"`
	Enable bool     `json:"enable"`
	Note   string   `json:"note,omitempty"`
}

// Plan is the ordered set of steps that realizes a Selection on the host. It is
// derived purely from the Selection (BuildPlan), so the same choices always
// produce the same plan and the mapping is unit-testable without a host.
type Plan struct {
	Selection Selection `json:"selection"`
	Steps     []Step    `json:"steps"`
}

// packagesFor returns the OS packages a webserver/engine needs.
var webserverPackages = map[Webserver][]string{
	WebserverOpenLiteSpeed: {"openlitespeed"},
	WebserverLiteSpeed:     {"lsws"},
}

var webserverService = map[Webserver]string{
	WebserverOpenLiteSpeed: "lsws",
	WebserverLiteSpeed:     "lsws",
}

var dbPackages = map[DBEngine][]string{
	DBEngineMariaDB: {"mariadb-server"},
}

var dbService = map[DBEngine]string{
	DBEngineMariaDB: "mariadb",
}

// baselineComponents is the stack every install gets, whatever the operator
// answered: the database management client, the malware scanner, the intrusion
// blocker, the WAF rule engine, and the firewall.
//
// They are not questions because treating them as questions produces panels
// where half the fleet has no WAF. The wizard shows them; it does not offer to
// omit them. Each one is already driven by a module that exists — phpMyAdmin by
// the database hand-off, ClamAV by internal/security/malware.go, Fail2Ban by
// fail2ban.go, ModSecurity by the waf.provision capability, nftables by
// firewall.go — so this is wiring up what the panel already assumes is there,
// rather than a promise about future work.
//
// maldet is in the baseline but not in this list: it is not in any distribution
// repository, so it installs from an upstream tarball through a capability of
// its own rather than a package name. BrokerProvisioner runs that after these.
var baselineComponents = []string{"phpmyadmin", "clamav", "fail2ban", "modsecurity", "nftables"}

// baselineServices are the baseline units that must be running, as opposed to
// the ones that are only ever invoked as a command (clamscan, fail2ban-client).
var baselineServices = []string{"clamav-freshclam", "fail2ban", "nftables"}

// BuildPlan turns a selection into an ordered provisioning plan: install and
// enable the webserver and MariaDB, add the always-on baseline (phpMyAdmin,
// ClamAV, Fail2Ban, ModSecurity, nftables), then turn the DNS and mail modules
// on or off. DNS uses BIND (named); mail uses Postfix + Dovecot — the packages
// the existing dns and mail modules drive.
func BuildPlan(sel Selection) Plan {
	steps := make([]Step, 0, 16)

	for _, p := range webserverPackages[sel.Webserver] {
		steps = append(steps, Step{Kind: StepPackage, Target: p, Enable: true})
	}
	if svc := webserverService[sel.Webserver]; svc != "" {
		steps = append(steps, Step{Kind: StepService, Target: svc, Enable: true})
	}

	for _, p := range dbPackages[sel.DBEngine] {
		steps = append(steps, Step{Kind: StepPackage, Target: p, Enable: true})
	}
	if svc := dbService[sel.DBEngine]; svc != "" {
		steps = append(steps, Step{Kind: StepService, Target: svc, Enable: true})
	}

	// The baseline: not conditional on anything the operator chose.
	for _, c := range baselineComponents {
		steps = append(steps, Step{Kind: StepPackage, Target: c, Enable: true, Note: "always installed"})
	}
	// maldet has no package, so it is its own step rather than a component the
	// broker resolves to apt/dnf.
	steps = append(steps, Step{Kind: StepMaldet, Target: "maldet", Enable: true, Note: "always installed"})
	for _, svc := range baselineServices {
		steps = append(steps, Step{Kind: StepService, Target: svc, Enable: true, Note: "always enabled"})
	}

	// DNS (BIND). Only add the package/service work when it is being turned on;
	// always record the module intent so a re-run can turn it off.
	if sel.ManageDNS {
		steps = append(steps,
			Step{Kind: StepPackage, Target: "bind9", Enable: true},
			Step{Kind: StepService, Target: "named", Enable: true})
	}
	steps = append(steps, Step{Kind: StepModule, Target: "dns", Enable: sel.ManageDNS})

	// Mail (Postfix + Dovecot).
	if sel.CreateMail {
		steps = append(steps,
			Step{Kind: StepPackage, Target: "postfix", Enable: true},
			Step{Kind: StepPackage, Target: "dovecot-core", Enable: true},
			Step{Kind: StepService, Target: "postfix", Enable: true},
			Step{Kind: StepService, Target: "dovecot", Enable: true})
	}
	steps = append(steps, Step{Kind: StepModule, Target: "mail", Enable: sel.CreateMail})

	return Plan{Selection: sel, Steps: steps}
}

// ── service ──────────────────────────────────────────────────────────────────

// Store persists the setup state. A single row is expected.
type Store interface {
	Get(ctx context.Context) (*State, error)
	Save(ctx context.Context, sel Selection, completedAt *time.Time) error
}

// Provisioner applies a plan to the host. It is host-level and privileged
// (package installs, systemctl) so the real implementation drives the broker;
// it may be nil, in which case the wizard records the operator's choices and
// leaves execution to a later provisioning pass (record-only mode).
type Provisioner interface {
	Provision(ctx context.Context, plan Plan) error
}

// Service is the setup module. It is safe to construct with a nil store (the
// wizard then reports "not available", which is what happens with no datastore)
// and a nil provisioner (record-only).
type Service struct {
	store      Store
	prov       Provisioner
	log        *slog.Logger
	onComplete func(ctx context.Context, sel Selection)
}

// NewService constructs the setup service. log may be nil (falls back to the
// default logger); prov may be nil (record-only).
func NewService(store Store, prov Provisioner, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: store, prov: prov, log: log}
}

// WithOnComplete registers a hook run after a successful Complete, so the rest of
// the panel can react to the operator's choice at runtime — switch the active
// web-server engine and the default database engine, and re-render vhosts —
// without a restart. Chainable.
func (s *Service) WithOnComplete(fn func(ctx context.Context, sel Selection)) *Service {
	s.onComplete = fn
	return s
}

// Available reports whether the setup module can operate (a datastore is wired).
func (s *Service) Available() bool { return s != nil && s.store != nil }

// Status returns the persisted state. On a fresh install (no row yet) it returns
// a zero State with Completed=false, which is what gates the wizard.
//
// The stored selection is repaired on the way out (NormalizeStored), so an
// install upgraded from a release with more engines reports the stack it is
// actually running rather than the one it was originally asked for.
func (s *Service) Status(ctx context.Context) (*State, error) {
	if !s.Available() {
		return nil, errx.New(errx.KindUnavailable, "setup_unavailable",
			"Setup is unavailable because the panel has no datastore.")
	}
	st, err := s.store.Get(ctx)
	if err != nil {
		return nil, err
	}
	if st != nil {
		st.Selection.NormalizeStored()
	}
	return st, nil
}

// Complete validates the selection, applies the provisioning plan (when a
// provisioner is wired), and records the wizard as finished. It is idempotent:
// completing an already-completed setup re-applies the plan and updates the
// stored selection, so an operator can change the stack later.
func (s *Service) Complete(ctx context.Context, sel Selection) (*State, error) {
	if !s.Available() {
		return nil, errx.New(errx.KindUnavailable, "setup_unavailable",
			"Setup is unavailable because the panel has no datastore.")
	}
	if err := sel.Validate(); err != nil {
		return nil, err
	}
	plan := BuildPlan(sel)
	if s.prov != nil {
		if err := s.prov.Provision(ctx, plan); err != nil {
			return nil, err
		}
	} else {
		s.log.Warn("setup: no provisioner wired; recording selection without applying host changes",
			"webserver", sel.Webserver, "db_engine", sel.DBEngine,
			"manage_dns", sel.ManageDNS, "create_mail", sel.CreateMail, "steps", len(plan.Steps))
	}
	now := time.Now().UTC()
	if err := s.store.Save(ctx, sel, &now); err != nil {
		return nil, err
	}
	if s.onComplete != nil {
		s.onComplete(ctx, sel)
	}
	return &State{Selection: sel, Completed: true, CompletedAt: &now}, nil
}

// Plan returns the provisioning plan a selection would produce, without applying
// or persisting anything — for a wizard that previews what "finish" will do.
func (s *Service) Plan(sel Selection) Plan { return BuildPlan(sel) }

// Components returns the logical infrastructure components this selection needs
// provisioned on the host, in install order: the webserver, MariaDB, the
// always-on baseline, then BIND (when DNS is managed here) and Postfix + Dovecot
// (when a mail server is wanted). These are the contract with the broker's
// system.provision capability; they are distro-agnostic on purpose, because the
// broker is the only place that knows apt-vs-dnf and the matching package and
// service names.
func (s Selection) Components() []string {
	out := []string{string(s.Webserver), string(s.DBEngine)}
	out = append(out, baselineComponents...)
	if s.ManageDNS {
		out = append(out, "bind")
	}
	if s.CreateMail {
		out = append(out, "postfix", "dovecot")
	}
	return out
}
