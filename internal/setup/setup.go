// Package setup implements HeroPanel's first-run infrastructure wizard. The
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
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Webserver identifies the webserver the panel provisions and manages for hosted
// sites.
type Webserver string

const (
	WebserverOpenLiteSpeed Webserver = "openlitespeed"
	WebserverNginx         Webserver = "nginx"
	WebserverApache        Webserver = "apache"
	WebserverLiteSpeed     Webserver = "litespeed_enterprise"
)

// DBEngine identifies the database engine the panel provisions and manages for
// hosted sites. It is distinct from the panel's own control-plane store, which
// is always SQLite.
type DBEngine string

const (
	DBEngineMySQL      DBEngine = "mysql"
	DBEngineMariaDB    DBEngine = "mariadb"
	DBEnginePostgreSQL DBEngine = "postgresql"
)

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

// Webservers is the webserver catalog, in display order. All entries are
// selectable: the wizard provisions (installs + enables) any of them via
// BuildPlan. Note, though, that only OpenLiteSpeed has a config-rendering
// backend today (internal/webserver) — a site created after choosing Nginx,
// Apache or LiteSpeed Enterprise is still rendered as an OLS vhost until those
// backends land. This is a deliberate operator choice, not a guarantee that
// every webserver is fully managed yet.
func Webservers() []Option {
	return []Option{
		{ID: string(WebserverOpenLiteSpeed), Label: "OpenLiteSpeed", Supported: true},
		{ID: string(WebserverNginx), Label: "Nginx", Supported: true},
		{ID: string(WebserverApache), Label: "Apache", Supported: true},
		{ID: string(WebserverLiteSpeed), Label: "LiteSpeed Enterprise", Note: "license required", Supported: true},
	}
}

// DBEngines is the database-engine catalog, in display order. All entries are
// selectable. MariaDB and MySQL share the panel's dual-dialect MySQL backend
// (internal/database); PostgreSQL is provisioned but not yet managed by the
// database module — the same deliberate tradeoff as the webserver catalog.
func DBEngines() []Option {
	return []Option{
		{ID: string(DBEngineMariaDB), Label: "MariaDB", Supported: true},
		{ID: string(DBEngineMySQL), Label: "MySQL", Supported: true},
		{ID: string(DBEnginePostgreSQL), Label: "PostgreSQL", Supported: true},
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
// that already exist, so any value is valid.
func (s Selection) Validate() error {
	if !supported(Webservers(), string(s.Webserver)) {
		return errx.Validation("unknown_webserver", "Unknown webserver.")
	}
	if !supported(DBEngines(), string(s.DBEngine)) {
		return errx.Validation("unknown_db_engine", "Unknown database engine.")
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
	WebserverNginx:         {"nginx"},
	WebserverApache:        {"apache2"},
	WebserverLiteSpeed:     {"lsws"},
}

var webserverService = map[Webserver]string{
	WebserverOpenLiteSpeed: "lsws",
	WebserverNginx:         "nginx",
	WebserverApache:        "apache2",
	WebserverLiteSpeed:     "lsws",
}

var dbPackages = map[DBEngine][]string{
	DBEngineMariaDB:    {"mariadb-server"},
	DBEngineMySQL:      {"mysql-server"},
	DBEnginePostgreSQL: {"postgresql"},
}

var dbService = map[DBEngine]string{
	DBEngineMariaDB:    "mariadb",
	DBEngineMySQL:      "mysql",
	DBEnginePostgreSQL: "postgresql",
}

// BuildPlan turns a selection into an ordered provisioning plan: install and
// enable the chosen webserver and database engine, then turn the DNS and mail
// modules on or off. DNS uses BIND (named); mail uses Postfix + Dovecot — the
// packages the existing dns and mail modules drive.
func BuildPlan(sel Selection) Plan {
	steps := make([]Step, 0, 8)

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
func (s *Service) Status(ctx context.Context) (*State, error) {
	if !s.Available() {
		return nil, errx.New(errx.KindUnavailable, "setup_unavailable",
			"Setup is unavailable because the panel has no datastore.")
	}
	return s.store.Get(ctx)
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
// provisioned on the host, in install order: the webserver, the database engine,
// then BIND (when DNS is managed here) and Postfix + Dovecot (when a mail server
// is wanted). These are the contract with the broker's system.provision
// capability; they are distro-agnostic on purpose, because the broker is the only
// place that knows apt-vs-dnf and the matching package/service names.
func (s Selection) Components() []string {
	out := []string{string(s.Webserver), string(s.DBEngine)}
	if s.ManageDNS {
		out = append(out, "bind")
	}
	if s.CreateMail {
		out = append(out, "postfix", "dovecot")
	}
	return out
}
