package apps

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/thisisnkp/heropanel/internal/docker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Deployer is the slice of the Docker service the Apps module needs. It is an
// interface so the app service depends on the capability, not the whole Docker
// package, and so tests can drive it without a daemon.
type Deployer interface {
	ComposeUp(ctx context.Context, project, site, file string) (string, error)
	ComposeDown(ctx context.Context, project string) error
	ComposePs(ctx context.Context, project string) ([]docker.ComposeService, error)
	ComposeLogs(ctx context.Context, project string, tail int) (*docker.Logs, error)
	// PublishedPorts reports the loopback host ports the panel's containers
	// already publish. Port allocation reads it so a fresh deploy never collides
	// with a running one — including across a restart, which the old in-memory
	// counter could not survive.
	PublishedPorts(ctx context.Context) ([]int, error)
	Available(ctx context.Context) bool
}

// Service deploys and manages one-click apps.
type Service struct {
	docker Deployer
	// availableMemMB is read from /proc/meminfo. The feasibility check compares a
	// template's floor against what the host actually has, so a deploy that cannot
	// run is refused before anything is pulled rather than found out when the OOM
	// killer arrives.
	availableMemMB func() int
}

// portRangeStart is the first loopback port apps are published on. Chosen high,
// out of the way of the panel and the usual system services. portRangeSpan
// bounds the search so a full range fails loudly rather than looping forever.
const (
	portRangeStart = 39000
	portRangeSpan  = 2000
)

// New constructs the service, reading available memory from the host.
func New(d Deployer) *Service { return NewWithMemory(d, availableMemoryMB) }

// NewWithMemory constructs the service with an explicit memory source, so a test
// can put the host at any memory pressure without touching /proc.
func NewWithMemory(d Deployer, availableMemMB func() int) *Service {
	if d == nil {
		return nil
	}
	return &Service{docker: d, availableMemMB: availableMemMB}
}

// allocatePort returns the lowest free loopback port at or above portRangeStart.
//
// It is derived from what docker actually publishes right now, not from an
// in-memory counter, so it is correct across a restart: the old counter reset to
// 39000 on every boot and would then hand a running app's port to a new deploy,
// which docker rejects with "port is already allocated". Reading the live set
// makes that impossible.
func (s *Service) allocatePort(ctx context.Context) (int, error) {
	used := map[int]bool{}
	if ports, err := s.docker.PublishedPorts(ctx); err == nil {
		for _, p := range ports {
			used[p] = true
		}
	}
	for p := portRangeStart; p < portRangeStart+portRangeSpan; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, errx.New(errx.KindInternal, "no_free_port",
		"No free loopback port is available for a new app.")
}

// Upstream returns the reverse-proxy target for an app's stack, resolved live
// from its published loopback port. It implements site.AppProxy, so a proxy site
// backed by this app always points at the port the app is actually on — an app
// redeployed on a new port is followed, and a torn-down app stops resolving.
func (s *Service) Upstream(ctx context.Context, project string) (string, bool) {
	if s == nil {
		return "", false
	}
	services, err := s.docker.ComposePs(ctx, project)
	if err != nil {
		return "", false
	}
	for _, svc := range services {
		if port, ok := parseLoopbackPort(svc.Ports); ok {
			return bindHost + ":" + strconv.Itoa(port), true
		}
	}
	return "", false
}

// bindHost is the loopback address every app publishes on and the panel proxies
// to. Duplicated as a constant here rather than imported from the broker — hpd
// does not depend on the broker package — but it is the same 127.0.0.1.
const bindHost = "127.0.0.1"

// reLoopbackPort pulls the host port out of a compose "Ports" string such as
// "127.0.0.1:39000->2368/tcp". Only a loopback mapping is matched: a port bound
// to anything else is not one the panel published and must not be proxied to.
var reLoopbackPort = regexp.MustCompile(`127\.0\.0\.1:(\d+)->`)

func parseLoopbackPort(ports string) (int, bool) {
	m := reLoopbackPort.FindStringSubmatch(ports)
	if m == nil {
		return 0, false
	}
	p, err := strconv.Atoi(m[1])
	if err != nil || p < 1 || p > 65535 {
		return 0, false
	}
	return p, true
}

// Templates returns the catalog with a feasibility verdict attached to each, so
// the UI can show "needs 512 MB, you have 240 MB" before the operator commits.
type TemplateView struct {
	Template
	Feasible    bool `json:"feasible"`
	AvailableMB int  `json:"available_mb"`
}

// Catalog returns every template with its current feasibility.
func (s *Service) Catalog() []TemplateView {
	avail := s.availableMemMB()
	all := All()
	views := make([]TemplateView, 0, len(all))
	for _, t := range all {
		views = append(views, TemplateView{
			Template:    t,
			Feasible:    avail == 0 || avail >= t.MinMemoryMB,
			AvailableMB: avail,
		})
	}
	return views
}

// DeployInput is a request to deploy a template.
type DeployInput struct {
	Slug   string            `json:"slug"`
	Name   string            `json:"name"` // the project/stack name
	Site   string            `json:"site,omitempty"`
	Values map[string]string `json:"values"`
}

// DeployResult reports what was created, including any generated secrets — shown
// once, because they are not stored in a form the panel can hand back later.
type DeployResult struct {
	Project string            `json:"project"`
	Port    int               `json:"port"`
	Secrets map[string]string `json:"secrets"`
}

// Deploy validates, generates secrets, checks memory, renders the compose file
// and brings the stack up.
func (s *Service) Deploy(ctx context.Context, in DeployInput) (*DeployResult, error) {
	if !s.docker.Available(ctx) {
		return nil, errx.New(errx.KindUnavailable, "docker_unavailable", "Docker is not available on this host.")
	}
	t, ok := Get(in.Slug)
	if !ok {
		return nil, errx.NotFound("template_not_found", "No such application template.")
	}

	project := strings.TrimSpace(in.Name)
	if project == "" {
		project = t.Slug
	}
	if !reProject.MatchString(project) {
		return nil, errx.Validation("invalid_name",
			"The app name must be lowercase letters, digits, dash or underscore.")
	}

	// Memory feasibility, before anything is pulled. A refused deploy here is a
	// clear message; the alternative is a stack that half-starts and is killed.
	if avail := s.availableMemMB(); avail > 0 && avail < t.MinMemoryMB {
		return nil, errx.Validation("insufficient_memory",
			"This app needs at least "+strconv.Itoa(t.MinMemoryMB)+" MB, but only "+
				strconv.Itoa(avail)+" MB is available.")
	}

	// Assemble the field values: operator entries validated, secrets generated.
	values := map[string]string{}
	secrets := map[string]string{}
	for _, f := range t.Fields {
		if f.Secret {
			secret, err := generateSecret()
			if err != nil {
				return nil, errx.Internal(err)
			}
			values[f.Key] = secret
			secrets[f.Key] = secret
			continue
		}
		v := strings.TrimSpace(in.Values[f.Key])
		if f.Required && v == "" {
			return nil, errx.Validation("field_required", f.Label+" is required.",
				errx.Field{Field: f.Key, Code: "required", Message: "required"})
		}
		// A field value ends up inside a compose file. A newline could forge
		// additional YAML, so it is refused rather than escaped — the same reason
		// the container-create env-file refuses one.
		if strings.ContainsAny(v, "\n\r") {
			return nil, errx.Validation("invalid_value", f.Label+" cannot contain a line break.")
		}
		values[f.Key] = v
	}

	port, err := s.allocatePort(ctx)
	if err != nil {
		return nil, err
	}
	compose := t.Render(RenderInput{Project: project, Values: values, HostPort: port})

	if _, err := s.docker.ComposeUp(ctx, project, in.Site, compose); err != nil {
		return nil, err
	}
	return &DeployResult{Project: project, Port: port, Secrets: secrets}, nil
}

// Remove tears an app's stack down.
func (s *Service) Remove(ctx context.Context, project string) error {
	return s.docker.ComposeDown(ctx, project)
}

// Status lists an app's running services.
func (s *Service) Status(ctx context.Context, project string) ([]docker.ComposeService, error) {
	return s.docker.ComposePs(ctx, project)
}

// Logs returns an app's combined logs.
func (s *Service) Logs(ctx context.Context, project string, tail int) (*docker.Logs, error) {
	return s.docker.ComposeLogs(ctx, project, tail)
}

// reProject mirrors the broker's compose project-name rule, so an app name hpd
// accepts is one the broker will accept too. Duplicated deliberately: a name
// rejected only at the broker would surface as an opaque failure rather than a
// clear "that name is not allowed" here.
var reProject = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

// generateSecret returns a URL-safe 32-byte random secret. crypto/rand, not
// math/rand: this is a password, and a predictable one is no password.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// availableMemoryMB reads MemAvailable from /proc/meminfo. It is world-readable,
// so hpd reads it directly rather than crossing the broker for a number that is
// not privileged. Returns 0 when it cannot be read (e.g. not Linux), which the
// feasibility check treats as "unknown, allow" rather than blocking every
// deploy on a platform where the file is absent.
func availableMemoryMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.Atoi(fields[1])
				return kb / 1024
			}
		}
	}
	return 0
}
