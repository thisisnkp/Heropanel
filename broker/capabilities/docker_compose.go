package capabilities

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Compose stacks.
//
// A compose stack is different in kind from a created container, and the honest
// framing matters: the container-create capability hardens what it builds
// because *it* builds the argv. A compose file is user-authored YAML, and it can
// ask for `privileged: true`, a host bind mount, `network_mode: host` — anything
// docker compose understands. The broker cannot make those safe without parsing
// and rewriting arbitrary compose, which is a losing game.
//
// So compose is treated as what it is: an advanced, explicit escape hatch,
// documented as such. What the broker still guarantees is narrower but real:
//
//   - the stack is **labelled** and can therefore be found and torn down;
//   - the project name is validated, so it cannot inject flags or escape the
//     stack directory;
//   - the operator's file is written to a **broker-chosen** directory, never a
//     path hpd names, so hpd cannot point compose at /etc/anything;
//   - nothing here runs a shell.
//
// The managed label is applied by deploying with a generated override file
// (`docker compose up` has no per-container label flag), so docker compose
// stamps it onto every container the stack creates — and the ownership boundary
// that guards single containers guards a stack's containers too.

const dockerComposePluginArg = "compose"

// A compose project name shares the volume-name shape: it starts alphanumeric,
// holds no slash, and cannot begin with a dash — so it is neither a path nor a
// flag. reVolumeName already encodes exactly that, so it is reused rather than
// duplicated.

func itoa(n int) string { return strconv.Itoa(n) }

// composeInput is a stack operation.
type composeInput struct {
	Project string `json:"project"`
	Site    string `json:"site"`
	// File is the compose YAML itself, written to a broker-chosen path. Empty for
	// operations that act on an already-running project (down, ps, logs).
	File string `json:"file"`
	Tail int    `json:"tail"`
}

func validateProject(p string) error {
	if !reVolumeName.MatchString(p) {
		return errx.Validation("invalid_project", "Invalid compose project name.",
			errx.Field{Field: "project", Code: "invalid_project", Message: "lowercase letters, digits, dash, underscore"})
	}
	return nil
}

// composeArgs builds the leading `docker compose -p <project>` argv common to
// every operation.
func composeArgs(project string, rest ...string) []string {
	return append([]string{dockerComposePluginArg, "-p", project}, rest...)
}

// ComposeUp brings a stack up, detached, from the operator's compose file.
type ComposeUp struct{}

func (ComposeUp) Name() string { return "docker.compose.up" }

func (ComposeUp) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in composeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.compose.up.")
	}
	if err := validateProject(in.Project); err != nil {
		return capability.Result{}, err
	}
	if in.File == "" {
		return capability.Result{}, errx.Validation("file_required", "A compose file is required.")
	}
	if in.Site != "" {
		if err := validateContainerRef(in.Site); err != nil {
			return capability.Result{}, err
		}
	}

	// The label has to be applied to every container the stack creates, or the
	// ownership boundary that guards single containers would not reach a stack's
	// containers — and `docker compose up` has no flag for it: compose labels
	// live inside each service definition.
	//
	// So the stack is deployed from a *broker-written directory* holding the
	// operator's file plus a generated override that adds the managed label to
	// every service. The override's service names come from compose itself
	// (`config --services`), so the broker never parses arbitrary YAML — it lets
	// compose do that and only weaves in labels. The directory is the broker's,
	// created with os.MkdirTemp, so the "hpd cannot name a path" property holds:
	// hpd supplies bytes, the broker chooses where they live and cleans up after.
	dir, err := os.MkdirTemp("", "hp-compose-")
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	base := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(base, []byte(in.File), 0o600); err != nil {
		return capability.Result{}, errx.Internal(err)
	}

	// Ask compose for the service names, so the override labels exactly the
	// services the file declares.
	namesRes, err := runDocker(c, 60*time.Second, "compose", "-f", base, "config", "--services")
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if namesRes.ExitCode != 0 {
		return capability.Result{}, dockerFailed(namesRes, "compose_invalid", "The compose file could not be read.")
	}

	override := buildLabelOverride(string(namesRes.Stdout), in.Site)
	overridePath := filepath.Join(dir, "hp-labels.yaml")
	if err := os.WriteFile(overridePath, []byte(override), 0o600); err != nil {
		return capability.Result{}, errx.Internal(err)
	}

	args := composeArgs(in.Project, "-f", base, "-f", overridePath, "up", "--detach", "--remove-orphans")
	res, err := runDocker(c, 15*time.Minute, args...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "compose_up_failed", "The stack did not come up.")
	}
	return capability.Result{Data: map[string]any{
		"project": in.Project,
		"log":     logTail(res, 8192),
	}}, nil
}

// BuildLabelOverride renders a compose override that stamps the panel's labels
// onto every named service. It is generated, not parsed: the service names come
// from `docker compose config --services`, so the broker never interprets the
// operator's YAML itself. Exported so its label-injection can be pinned directly
// — it is the part that makes a stack's containers manageable.
func BuildLabelOverride(serviceList, site string) string {
	return buildLabelOverride(serviceList, site)
}

func buildLabelOverride(serviceList, site string) string {
	var b strings.Builder
	b.WriteString("services:\n")
	for _, name := range strings.Fields(serviceList) {
		b.WriteString("  " + name + ":\n")
		b.WriteString("    labels:\n")
		b.WriteString("      " + LabelManaged + ": \"1\"\n")
		if site != "" {
			b.WriteString("      " + LabelSite + ": " + site + "\n")
		}
	}
	return b.String()
}

// ComposeDown tears a stack down. It removes the stack's containers and networks
// but not its named volumes, for the same reason a single container remove does
// not: stopping is routine, destroying data is a separate, explicit act.
type ComposeDown struct{}

func (ComposeDown) Name() string { return "docker.compose.down" }

func (ComposeDown) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in composeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.compose.down.")
	}
	if err := validateProject(in.Project); err != nil {
		return capability.Result{}, err
	}
	if err := composeProjectIsManaged(c, in.Project); err != nil {
		return capability.Result{}, err
	}
	res, err := runDocker(c, 5*time.Minute, composeArgs(in.Project, "down", "--remove-orphans")...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "compose_down_failed", "The stack did not come down.")
	}
	return capability.Result{Data: map[string]any{"project": in.Project}}, nil
}

// ComposePs lists a stack's containers.
type ComposePs struct{}

func (ComposePs) Name() string { return "docker.compose.ps" }

func (ComposePs) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in composeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.compose.ps.")
	}
	if err := validateProject(in.Project); err != nil {
		return capability.Result{}, err
	}
	res, err := runDocker(c, 30*time.Second, composeArgs(in.Project, "ps", "--format", "{{json .}}")...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "compose_ps_failed", "Could not list the stack.")
	}
	return capability.Result{Data: map[string]any{"services": linesOf(res.Stdout)}}, nil
}

// ComposeLogs returns a bounded tail of the whole stack's logs.
type ComposeLogs struct{}

func (ComposeLogs) Name() string { return "docker.compose.logs" }

func (ComposeLogs) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in composeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.compose.logs.")
	}
	if err := validateProject(in.Project); err != nil {
		return capability.Result{}, err
	}
	tail := in.Tail
	if tail <= 0 || tail > maxDockerLogLines {
		tail = maxDockerLogLines
	}
	res, err := runDocker(c, 60*time.Second,
		composeArgs(in.Project, "logs", "--no-color", "--tail", itoa(tail))...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "compose_logs_failed", "Could not read the stack's logs.")
	}
	return capability.Result{Data: map[string]any{
		"stdout": string(res.Stdout),
		"stderr": string(res.Stderr),
	}}, nil
}

// composeProjectIsmanaged guards tear-down: a stack the panel did not create is
// not the panel's to remove. It checks the stack's own containers carry the
// managed label — a compose project name is not proof of ownership on its own,
// since anyone can name a stack anything.
func composeProjectIsManaged(c capability.Context, project string) error {
	res, err := runDocker(c, 20*time.Second,
		"ps", "--all", "--filter", "label=com.docker.compose.project="+project,
		"--filter", "label="+LabelManaged+"=1", "--format", "{{.ID}}")
	if err != nil {
		return errx.Internal(err)
	}
	if len(res.Stdout) == 0 {
		return errx.New(errx.KindForbidden, "compose_not_managed",
			"That stack was not created by HeroPanel, so the panel will not tear it down.")
	}
	return nil
}
