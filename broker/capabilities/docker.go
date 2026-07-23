package capabilities

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// The Docker capabilities.
//
// Docker is the sharpest privilege in the whole panel: anyone who can reach the
// daemon socket can run `docker run -v /:/host --privileged` and own the
// machine. That is *why* these are broker capabilities rather than a module in
// the `docker` group — putting hpd, the network-facing process, in that group
// would make it root-equivalent and quietly retire the privilege separation the
// rest of the panel is built on. Here the socket stays reachable only by the
// broker, and every operation is named, validated, policy-gated and audited like
// any other privileged act.
//
// Two boundaries do the real work:
//
//  1. **Ownership.** Mutating a container requires it to carry the panel's
//     managed label. Without that check `docker.container.stop` is a button that
//     stops *any* container on the host — the site database, a CI runner, the
//     monitoring agent. This is Docker's equivalent of the File Manager's path
//     confinement: the op is only allowed to name things the panel owns.
//     Reading is deliberately not restricted this way (see ContainerList).
//
//  2. **Flag injection.** An argv array stops *shell* injection, and every
//     capability here builds one — but it does nothing about a value that is
//     itself a flag. A container named `--privileged` or an image called
//     `-v=/:/host` is parsed by docker as an option, not an operand. So every
//     user-supplied value is matched against an allowlist pattern that cannot
//     begin with `-`, and `--` terminates option parsing wherever docker
//     supports it. Both, not either.

const dockerPath = "/usr/bin/docker"

// Labels the panel stamps on everything it creates. `managed` is the ownership
// boundary; `site` scopes a container to the site that owns it.
const (
	LabelManaged = "io.heropanel.managed"
	LabelSite    = "io.heropanel.site"
)

var (
	// reContainerRef is a container name or id. It must start alphanumeric,
	// which is what makes a value beginning with "-" unrepresentable.
	reContainerRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	// reImageRef covers [registry/]name[:tag][@sha256:…]. Deliberately strict:
	// it is an argument to a privileged command, not a free-text field.
	reImageRef = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,199}(:[A-Za-z0-9_][A-Za-z0-9._-]{0,127})?(@sha256:[a-f0-9]{64})?$`)
)

// validateContainerRef checks a container name or id.
func validateContainerRef(s string) error {
	if !reContainerRef.MatchString(s) {
		return errx.Validation("invalid_container", "Invalid container name or id.",
			errx.Field{Field: "container", Code: "invalid_container", Message: "invalid format"})
	}
	return nil
}

// validateImageRef checks an image reference.
func validateImageRef(s string) error {
	if !reImageRef.MatchString(s) {
		return errx.Validation("invalid_image", "Invalid image reference.",
			errx.Field{Field: "image", Code: "invalid_image", Message: "invalid format"})
	}
	return nil
}

// runDocker executes the docker CLI as root, with no shell anywhere.
//
// The CLI is used rather than the Engine SDK on purpose: it keeps the dependency
// surface at zero, and it produces exactly the argv-array shape every other
// capability in this broker already has, so one reviewer habit ("read the args")
// covers all of them.
func runDocker(c capability.Context, timeout time.Duration, args ...string) (exec.Result, error) {
	return c.Runner.Run(c.Ctx, exec.Command{Path: dockerPath, Args: args, Timeout: timeout})
}

// dockerFailed turns a non-zero docker exit into a useful error. Docker writes
// its reason to stderr, and hiding it behind "the operation failed" is what
// makes container problems miserable to debug, so it is passed through.
func dockerFailed(res exec.Result, code, msg string) error {
	detail := strings.TrimSpace(string(res.Stderr))
	if detail == "" {
		detail = strings.TrimSpace(string(res.Stdout))
	}
	if detail != "" {
		msg = msg + " " + detail
	}
	return errx.New(errx.KindInternal, code, msg)
}

// requireManaged is the ownership boundary: it refuses to act on a container the
// panel did not create.
//
// The check is a live inspect rather than a name convention, because a name is
// something the caller supplies and a label is something the daemon reports. A
// container called `hp-site1-web` that the panel never created would sail
// through a prefix check; it cannot fake the label.
func requireManaged(c capability.Context, ref string) error {
	res, err := runDocker(c, 20*time.Second,
		"inspect", "--format", "{{index .Config.Labels \""+LabelManaged+"\"}}", ref)
	if err != nil {
		return errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return errx.NotFound("container_not_found", "No such container.")
	}
	if strings.TrimSpace(string(res.Stdout)) != "1" {
		// Said plainly: an operator who meant to stop their own container should
		// learn they named someone else's, not that "something went wrong".
		return errx.New(errx.KindForbidden, "container_not_managed",
			"That container was not created by HeroPanel, so the panel will not modify it.")
	}
	return nil
}

// ── docker.info ──────────────────────────────────────────────────────────────

// DockerInfo reports whether a usable daemon is present, and its version. It is
// what the panel asks before offering any Docker feature at all: a greyed-out
// section with a reason beats a page of buttons that all fail.
type DockerInfo struct{}

func (DockerInfo) Name() string { return "docker.info" }

func (DockerInfo) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	res, err := runDocker(c, 20*time.Second, "version", "--format", "{{json .}}")
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		// Not an error: "no daemon" is a legitimate state the UI renders. An
		// error here would make a panel on a host without Docker look broken.
		return capability.Result{Data: map[string]any{
			"available": false,
			"reason":    strings.TrimSpace(string(res.Stderr)),
		}}, nil
	}
	return capability.Result{Data: map[string]any{
		"available": true,
		"version":   json.RawMessage(res.Stdout),
	}}, nil
}

// ── docker.container.list ────────────────────────────────────────────────────

// ContainerList lists containers.
//
// It lists *every* container on the host, not only the panel's, and tags each
// with whether the panel manages it. Visibility is not the dangerous half —
// mutation is, and that is guarded separately. An admin looking at a host whose
// memory is gone needs to see the container eating it even if the panel did not
// start it; hiding it would make the panel lie about the machine it administers.
type ContainerList struct{}

func (ContainerList) Name() string { return "docker.container.list" }

func (ContainerList) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		All  bool   `json:"all"`
		Site string `json:"site"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.container.list.")
		}
	}
	args := []string{"ps", "--no-trunc", "--format", "{{json .}}"}
	if in.All {
		args = append(args, "--all")
	}
	if in.Site != "" {
		if err := validateContainerRef(in.Site); err != nil {
			return capability.Result{}, err
		}
		args = append(args, "--filter", "label="+LabelSite+"="+in.Site)
	}
	res, err := runDocker(c, 30*time.Second, args...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_list_failed", "Could not list containers.")
	}
	return capability.Result{Data: map[string]any{"containers": linesOf(res.Stdout)}}, nil
}

// ── docker.container.inspect ─────────────────────────────────────────────────

// ContainerInspect returns one container's full state.
type ContainerInspect struct{}

func (ContainerInspect) Name() string { return "docker.container.inspect" }

func (ContainerInspect) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Container string `json:"container"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.container.inspect.")
	}
	if err := validateContainerRef(in.Container); err != nil {
		return capability.Result{}, err
	}
	res, err := runDocker(c, 20*time.Second, "inspect", "--format", "{{json .}}", in.Container)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.NotFound("container_not_found", "No such container.")
	}
	return capability.Result{Data: map[string]any{"container": json.RawMessage(res.Stdout)}}, nil
}

// ── docker.container.logs ────────────────────────────────────────────────────

// ContainerLogs returns a bounded tail of a container's logs.
//
// Bounded on purpose: a container that has been looping an error for a week has
// gigabytes of logs, and the broker's wire frame caps at 1 MiB. The tail is
// clamped here rather than trusted from the caller — an unbounded request is how
// a log viewer becomes a way to exhaust the broker's memory.
type ContainerLogs struct{}

func (ContainerLogs) Name() string { return "docker.container.logs" }

// maxDockerLogLines bounds a single log request.
const maxDockerLogLines = 2000

func (ContainerLogs) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Container  string `json:"container"`
		Tail       int    `json:"tail"`
		Timestamps bool   `json:"timestamps"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.container.logs.")
	}
	if err := validateContainerRef(in.Container); err != nil {
		return capability.Result{}, err
	}
	if in.Tail <= 0 || in.Tail > maxDockerLogLines {
		in.Tail = maxDockerLogLines
	}
	args := []string{"logs", "--tail", strconv.Itoa(in.Tail)}
	if in.Timestamps {
		args = append(args, "--timestamps")
	}
	args = append(args, in.Container)

	res, err := runDocker(c, 60*time.Second, args...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.NotFound("container_not_found", "No such container.")
	}
	// Docker splits container output across stdout and stderr; both are the
	// container's log and the viewer wants them interleaved as the program wrote
	// them, so they are returned separately and joined by the caller rather than
	// silently dropping one.
	return capability.Result{Data: map[string]any{
		"stdout": string(res.Stdout),
		"stderr": string(res.Stderr),
	}}, nil
}

// ── docker.container.stats ───────────────────────────────────────────────────

// ContainerStats samples resource usage once.
//
// One sample, not a stream: `docker stats` without --no-stream never returns,
// and a capability that never returns holds a broker connection open forever.
// Live charts are the caller's job — it polls this.
type ContainerStats struct{}

func (ContainerStats) Name() string { return "docker.container.stats" }

func (ContainerStats) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Container string `json:"container"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.container.stats.")
		}
	}
	args := []string{"stats", "--no-stream", "--format", "{{json .}}"}
	if in.Container != "" {
		if err := validateContainerRef(in.Container); err != nil {
			return capability.Result{}, err
		}
		args = append(args, in.Container)
	}
	res, err := runDocker(c, 30*time.Second, args...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_stats_failed", "Could not read container stats.")
	}
	return capability.Result{Data: map[string]any{"stats": linesOf(res.Stdout)}}, nil
}

// ── lifecycle: start / stop / restart / remove ───────────────────────────────

// containerAction is the shared body of the lifecycle capabilities. Every one of
// them passes through requireManaged first — that single line is the difference
// between a container manager and a remote off-switch for the whole host.
func containerAction(c capability.Context, raw json.RawMessage, name string,
	build func(ref string, force bool) []string, timeout time.Duration) (capability.Result, error) {
	var in struct {
		Container string `json:"container"`
		Force     bool   `json:"force"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for "+name+".")
	}
	if err := validateContainerRef(in.Container); err != nil {
		return capability.Result{}, err
	}
	if err := requireManaged(c, in.Container); err != nil {
		return capability.Result{}, err
	}
	res, err := runDocker(c, timeout, build(in.Container, in.Force)...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_action_failed", "The container operation failed.")
	}
	return capability.Result{Data: map[string]any{"container": in.Container}}, nil
}

// ContainerStart starts a stopped container.
type ContainerStart struct{}

func (ContainerStart) Name() string { return "docker.container.start" }

func (ContainerStart) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	return containerAction(c, raw, "docker.container.start",
		func(ref string, _ bool) []string { return []string{"start", ref} }, 2*time.Minute)
}

// stopGrace is how long a container gets between SIGTERM and SIGKILL.
//
// It is docker's own default, and it is deliberately well under hpd's 30s HTTP
// write timeout. A longer grace looks harmless and is not: a container that
// ignores SIGTERM — `sleep`, and plenty of real images — then consumes the
// entire request budget, the connection is closed before the handler answers,
// and the operator sees a failed request for an operation that in fact
// succeeded. The live e2e is what surfaced that, as a bare `000` from curl.
const stopGrace = "10"

// ContainerStop stops a running container, giving it a chance to shut down
// cleanly first.
type ContainerStop struct{}

func (ContainerStop) Name() string { return "docker.container.stop" }

func (ContainerStop) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	return containerAction(c, raw, "docker.container.stop",
		func(ref string, _ bool) []string { return []string{"stop", "--time", stopGrace, ref} }, 2*time.Minute)
}

// ContainerRestart restarts a container.
type ContainerRestart struct{}

func (ContainerRestart) Name() string { return "docker.container.restart" }

func (ContainerRestart) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	return containerAction(c, raw, "docker.container.restart",
		func(ref string, _ bool) []string { return []string{"restart", "--time", stopGrace, ref} }, 3*time.Minute)
}

// ContainerRemove deletes a container. `force` kills it first; the volumes it
// owns are *not* removed, because deleting a container is routine and deleting
// data is not — that is a separate, explicit act.
type ContainerRemove struct{}

func (ContainerRemove) Name() string { return "docker.container.remove" }

func (ContainerRemove) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	return containerAction(c, raw, "docker.container.remove", func(ref string, force bool) []string {
		args := []string{"rm"}
		if force {
			args = append(args, "--force")
		}
		return append(args, ref)
	}, 2*time.Minute)
}

// ── images ───────────────────────────────────────────────────────────────────

// ImageList lists images on the host.
type ImageList struct{}

func (ImageList) Name() string { return "docker.image.list" }

func (ImageList) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	res, err := runDocker(c, 30*time.Second, "images", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_images_failed", "Could not list images.")
	}
	return capability.Result{Data: map[string]any{"images": linesOf(res.Stdout)}}, nil
}

// ImagePull fetches an image.
//
// The long timeout is the point of a separate capability: pulling a multi-
// gigabyte image over a slow link is normal, and a 30-second cap would make
// larger images simply impossible to use.
type ImagePull struct{}

func (ImagePull) Name() string { return "docker.image.pull" }

func (ImagePull) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Image string `json:"image"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.image.pull.")
	}
	if err := validateImageRef(in.Image); err != nil {
		return capability.Result{}, err
	}
	res, err := runDocker(c, 30*time.Minute, "pull", "--", in.Image)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_pull_failed", "Could not pull the image.")
	}
	return capability.Result{Data: map[string]any{"image": in.Image, "log": logTail(res, 8192)}}, nil
}

// ── docker.image.remove ──────────────────────────────────────────────────────

// ImageRemove deletes an image from the host.
//
// Images do not carry the panel's managed label — the same image (postgres:16)
// backs a panel app and, plausibly, something the operator ran by hand, so there
// is no honest way to call one copy "ours". The ownership boundary that protects
// containers is therefore absent here, and a different guarantee takes its place:
// docker itself refuses to remove an image that a container (running *or*
// stopped) still references, and that refusal is passed straight through rather
// than papered over. Removing an image out from under a managed app is thus not
// possible without first removing the app's container, which *is* ownership-
// gated. `force` only detaches extra tags; it does not override the in-use check.
type ImageRemove struct{}

func (ImageRemove) Name() string { return "docker.image.remove" }

func (ImageRemove) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Image string `json:"image"`
		Force bool   `json:"force"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.image.remove.")
	}
	if err := validateImageRef(in.Image); err != nil {
		return capability.Result{}, err
	}
	args := []string{"rmi"}
	if in.Force {
		args = append(args, "--force")
	}
	args = append(args, "--", in.Image)
	res, err := runDocker(c, 60*time.Second, args...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		// "image is being used by container …" arrives here verbatim: the operator
		// learns an app still depends on it, not that "removal failed".
		return capability.Result{}, dockerFailed(res, "docker_rmi_failed", "Could not remove the image.")
	}
	return capability.Result{Data: map[string]any{"image": in.Image, "log": logTail(res, 8192)}}, nil
}

// ── docker.image.prune ───────────────────────────────────────────────────────

// ImagePrune reclaims disk from images no container needs.
//
// The default is dangling-only — untagged layers left behind by rebuilds, which
// nothing can reference by name. That is always safe to drop. `all` extends it to
// every image not used by *some* container, which is a bigger hammer: an image
// pulled ahead of a deploy, or one shared by a stack that is currently down,
// would go too. So `all` is opt-in and never the default, and even then docker's
// own "used by a container" test still protects anything running or stopped.
type ImagePrune struct{}

func (ImagePrune) Name() string { return "docker.image.prune" }

func (ImagePrune) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		All bool `json:"all"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.image.prune.")
		}
	}
	// --force skips the interactive confirmation docker would otherwise wait on;
	// the broker has no tty and an unanswered prompt would hang the capability.
	args := []string{"image", "prune", "--force"}
	if in.All {
		args = append(args, "--all")
	}
	res, err := runDocker(c, 5*time.Minute, args...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_prune_failed", "Could not prune images.")
	}
	return capability.Result{Data: map[string]any{"log": logTail(res, 8192)}}, nil
}

// linesOf splits docker's newline-delimited JSON output into raw messages. Each
// line is one object; docker emits nothing at all for an empty result, which
// must come back as an empty list rather than a list holding one empty string.
func linesOf(b []byte) []json.RawMessage {
	out := []json.RawMessage{}
	for _, line := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, json.RawMessage(s))
		}
	}
	return out
}

// ValidateContainerRef is validateContainerRef exported for the streaming exec
// path, which lives outside the capability registry but must apply the same
// rule — one definition of "a container name the broker will accept".
func ValidateContainerRef(s string) error { return validateContainerRef(s) }
