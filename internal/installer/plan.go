package installer

import "time"

// Options control what the installer will do (from CLI flags / answers file).
type Options struct {
	Channel     string   `json:"channel"`      // stable | beta | nightly
	DB          string   `json:"db"`           // mariadb | sqlite
	Port        int      `json:"port"`         // panel port
	Minimal     bool     `json:"minimal"`      // low-RAM preset
	NoWebServer bool     `json:"no_webserver"` // don't manage the site web server
	Modules     []string `json:"modules"`      // optional modules to install
}

// DefaultOptions returns sensible install defaults.
func DefaultOptions() Options {
	return Options{Channel: "stable", DB: "mariadb", Port: 8443}
}

// Step is a single, ordered, reversible install action.
type Step struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Kind        string `json:"kind"` // pkg | user | dir | binaries | config | db | service | webserver | firewall | verify
}

// Plan computes the ordered action list for a profile + options. It is the basis
// of the journal (each step records its outcome for resume/rollback, docs/07 §4).
func Plan(p Profile, o Options) []Step {
	if o.DB == "" {
		o.DB = "mariadb"
	}
	if o.Minimal {
		o.DB = "sqlite"
	}

	var steps []Step
	add := func(id, desc, kind string) { steps = append(steps, Step{ID: id, Description: desc, Kind: kind}) }

	add("deps.base", "Install base dependencies (ca-certificates, tar, zstd)", "pkg")
	if !o.NoWebServer {
		add("deps.webserver", "Install OpenLiteSpeed", "pkg")
	}
	if o.DB == "mariadb" {
		add("deps.db", "Install MariaDB", "pkg")
	}
	add("deps.redis", "Install Redis", "pkg")

	add("user", "Create the heropanel system user and group", "user")
	add("dirs", "Create /opt, /etc, /var, /run directories with correct modes", "dir")
	add("binaries", "Install hpd, hp-broker, and hpctl", "binaries")
	add("secrets", "Generate secrets (DB password, JWT key, broker token, master key)", "config")
	add("config", "Write /etc/heropanel/config.yaml", "config")

	if o.DB == "mariadb" {
		add("db.provision", "Create the panel database and user", "db")
	}
	add("db.migrate", "Run database migrations", "db")

	add("services", "Install and start hardened systemd units (broker, hpd)", "service")
	if !o.NoWebServer {
		add("webserver.panel", "Configure OpenLiteSpeed to serve the panel", "webserver")
	}
	add("firewall", "Open the panel port with a connectivity rollback timer", "firewall")

	for _, m := range o.Modules {
		add("module."+m, "Install module "+m, "service")
	}

	add("verify", "Health-check hpd and the broker; confirm the panel is reachable", "verify")
	return steps
}

// StepStatus is a step's recorded outcome.
type StepStatus string

const (
	StatusPending StepStatus = "pending"
	StatusDone    StepStatus = "done"
	StatusFailed  StepStatus = "failed"
)

// JournalStep is a planned step with its recorded outcome + inverse.
type JournalStep struct {
	Step   Step       `json:"step"`
	Status StepStatus `json:"status"`
	Error  string     `json:"error,omitempty"`
}

// Journal is the on-disk record enabling resume and reverse rollback (docs/07 §5).
type Journal struct {
	Version   string        `json:"version"`
	StartedAt string        `json:"started_at"`
	Profile   Profile       `json:"profile"`
	Options   Options       `json:"options"`
	Steps     []JournalStep `json:"steps"`
}

// NewJournal builds a fresh journal from a plan (all steps pending).
func NewJournal(version string, p Profile, o Options) Journal {
	plan := Plan(p, o)
	steps := make([]JournalStep, len(plan))
	for i, s := range plan {
		steps[i] = JournalStep{Step: s, Status: StatusPending}
	}
	return Journal{
		Version:   version,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Profile:   p,
		Options:   o,
		Steps:     steps,
	}
}
