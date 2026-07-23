package capabilities

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Creating containers, volumes and networks.
//
// This is the file where the module stops being a viewer and starts placing
// workloads on the host, so it is where the hardening lives. The rule
// throughout: the caller describes *what* it wants in typed fields, and the
// broker decides what argv that becomes. hpd never hands over flags.
//
// What is refused is as important as what is offered, and most of it is refused
// by construction — there is simply no input that produces it:
//
//   - `--privileged`, `--cap-add`, `--device`, `--security-opt`, `--userns`,
//     `--pid/--ipc/--uts=host`: no field maps to any of them.
//   - **host bind mounts.** Only *named volumes* can be mounted. This is the one
//     that matters most: `-v /:/host` or `-v /var/run/docker.sock:/…` inside a
//     container is a complete escape to host root, and it is the single most
//     common way a container manager becomes a privilege-escalation tool.
//   - **publishing on all interfaces.** Every published port is bound to
//     127.0.0.1 (see bindHost).
//
// And one thing is added rather than removed: `no-new-privileges`, so a setuid
// binary inside the image cannot raise privileges beyond what the container
// started with.

// bindHost is the interface published ports are bound to. Always loopback.
//
// This is not a preference. Docker installs its own iptables/nftables rules that
// are evaluated *before* the host firewall's, so a container publishing on
// 0.0.0.0 is reachable from the internet even on a host whose firewall denies
// that port — operators discover this after the fact, which is how databases end
// up publicly exposed. Everything HeroPanel puts in front of a container (the
// reverse proxy) runs on the same host, so loopback is all that is ever needed.
const bindHost = "127.0.0.1"

var (
	// reVolumeName is a *named volume*. It cannot contain "/" or ".." so it can
	// never denote a host path — which is what stops `-v /:/host`.
	reVolumeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
	// reNetworkName matches docker's own network naming.
	reNetworkName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
	// reEnvKey is an environment variable name.
	reEnvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	// reMountPath is an absolute path *inside* the container.
	reMountPath = regexp.MustCompile(`^/[A-Za-z0-9._/-]{0,255}$`)
)

// restartPolicies is the allowlist. "always" is deliberately included but
// "unless-stopped" is the sane default for panel workloads: it survives a reboot
// without also resurrecting something an operator deliberately stopped.
var restartPolicies = map[string]bool{
	"no": true, "on-failure": true, "unless-stopped": true, "always": true,
}

type portMapping struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Proto     string `json:"proto"`
}

type volumeMount struct {
	Volume   string `json:"volume"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"read_only"`
}

type containerCreateInput struct {
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Site     string            `json:"site"`
	Env      map[string]string `json:"env"`
	Ports    []portMapping     `json:"ports"`
	Volumes  []volumeMount     `json:"volumes"`
	Restart  string            `json:"restart"`
	Network  string            `json:"network"`
	MemoryMB int               `json:"memory_mb"`
	Command  []string          `json:"command"`
}

// ContainerCreate starts a new container the panel owns.
type ContainerCreate struct{}

func (ContainerCreate) Name() string { return "docker.container.create" }

func (ContainerCreate) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in containerCreateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.container.create.")
	}
	if err := validateContainerRef(in.Name); err != nil {
		return capability.Result{}, err
	}
	if err := validateImageRef(in.Image); err != nil {
		return capability.Result{}, err
	}
	if in.Site != "" {
		if err := validateContainerRef(in.Site); err != nil {
			return capability.Result{}, err
		}
	}

	args := []string{"run", "--detach", "--name", in.Name,
		// The ownership label. Everything the panel will later be willing to
		// stop, restart or remove is stamped here and nowhere else.
		"--label", LabelManaged + "=1",
		// A setuid binary inside the image must not be able to gain more than
		// the container started with.
		"--security-opt", "no-new-privileges",
	}
	if in.Site != "" {
		args = append(args, "--label", LabelSite+"="+in.Site)
	}

	restart := in.Restart
	if restart == "" {
		restart = "unless-stopped"
	}
	if !restartPolicies[restart] {
		return capability.Result{}, errx.Validation("invalid_restart_policy",
			"Restart policy must be one of: no, on-failure, unless-stopped, always.")
	}
	args = append(args, "--restart", restart)

	if in.MemoryMB > 0 {
		if in.MemoryMB < 16 || in.MemoryMB > 1024*1024 {
			return capability.Result{}, errx.Validation("invalid_memory",
				"Memory limit must be between 16 MB and 1 TB.")
		}
		args = append(args, "--memory", strconv.Itoa(in.MemoryMB)+"m")
	}

	if in.Network != "" {
		if !reNetworkName.MatchString(in.Network) {
			return capability.Result{}, errx.Validation("invalid_network", "Invalid network name.")
		}
		args = append(args, "--network", in.Network)
	}

	for _, p := range in.Ports {
		if p.Host < 1 || p.Host > 65535 || p.Container < 1 || p.Container > 65535 {
			return capability.Result{}, errx.Validation("invalid_port",
				"Ports must be between 1 and 65535.")
		}
		proto := p.Proto
		if proto == "" {
			proto = "tcp"
		}
		if proto != "tcp" && proto != "udp" {
			return capability.Result{}, errx.Validation("invalid_protocol", "Protocol must be tcp or udp.")
		}
		// host:container is built here, so the caller cannot supply the bind
		// address and cannot reach 0.0.0.0.
		args = append(args, "--publish",
			bindHost+":"+strconv.Itoa(p.Host)+":"+strconv.Itoa(p.Container)+"/"+proto)
	}

	for _, v := range in.Volumes {
		// A *name*, never a path. reVolumeName admits no "/" and no "..", so
		// there is no input here that denotes anything on the host filesystem.
		if !reVolumeName.MatchString(v.Volume) {
			return capability.Result{}, errx.Validation("invalid_volume",
				"Only named volumes can be mounted. A host path is never accepted.",
				errx.Field{Field: "volume", Code: "invalid_volume", Message: "must be a named volume"})
		}
		if !reMountPath.MatchString(v.Path) || strings.Contains(v.Path, "..") {
			return capability.Result{}, errx.Validation("invalid_mount_path",
				"The mount path must be an absolute path inside the container.")
		}
		spec := v.Volume + ":" + v.Path
		if v.ReadOnly {
			spec += ":ro"
		}
		args = append(args, "--volume", spec)
	}

	// Environment travels through stdin as an env-file, never argv: argv is
	// world-readable via /proc, and these values are exactly where a generated
	// database password lives. It never touches disk either.
	var envFile []byte
	if len(in.Env) > 0 {
		keys := make([]string, 0, len(in.Env))
		for k := range in.Env {
			if !reEnvKey.MatchString(k) {
				return capability.Result{}, errx.Validation("invalid_env_key",
					"Invalid environment variable name.")
			}
			keys = append(keys, k)
		}
		// Sorted so the same request produces the same bytes — a stable input is
		// worth having when something has to be reproduced from an audit entry.
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			v := in.Env[k]
			// A newline would let one variable's value forge another line of the
			// env-file, which is injection by a different door.
			if strings.ContainsAny(v, "\n\r") {
				return capability.Result{}, errx.Validation("invalid_env_value",
					"An environment value cannot contain a line break.")
			}
			b.WriteString(k + "=" + v + "\n")
		}
		envFile = []byte(b.String())
		args = append(args, "--env-file", "/dev/stdin")
	}

	// The image operand, then the container's own command. Anything after the
	// image is the *container's* argv — docker has stopped parsing its own
	// options by then — so a leading "-" here is the program's flag, not
	// docker's, and needs no rejection.
	args = append(args, in.Image)
	args = append(args, in.Command...)

	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: dockerPath, Args: args, Stdin: envFile, Timeout: 10 * time.Minute,
	})
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_create_failed", "Could not create the container.")
	}
	return capability.Result{Data: map[string]any{
		"name": in.Name,
		"id":   strings.TrimSpace(string(res.Stdout)),
	}}, nil
}

// ── volumes ──────────────────────────────────────────────────────────────────

// VolumeList lists volumes, each flagged with whether the panel owns it.
type VolumeList struct{}

func (VolumeList) Name() string { return "docker.volume.list" }

func (VolumeList) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	res, err := runDocker(c, 30*time.Second, "volume", "ls", "--format", "{{json .}}")
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_volumes_failed", "Could not list volumes.")
	}
	return capability.Result{Data: map[string]any{"volumes": linesOf(res.Stdout)}}, nil
}

// VolumeCreate creates a named volume, labelled as the panel's.
type VolumeCreate struct{}

func (VolumeCreate) Name() string { return "docker.volume.create" }

func (VolumeCreate) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
		Site string `json:"site"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.volume.create.")
	}
	if !reVolumeName.MatchString(in.Name) {
		return capability.Result{}, errx.Validation("invalid_volume", "Invalid volume name.")
	}
	args := []string{"volume", "create", "--label", LabelManaged + "=1"}
	if in.Site != "" {
		if err := validateContainerRef(in.Site); err != nil {
			return capability.Result{}, err
		}
		args = append(args, "--label", LabelSite+"="+in.Site)
	}
	args = append(args, in.Name)

	res, err := runDocker(c, 30*time.Second, args...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_volume_create_failed", "Could not create the volume.")
	}
	return capability.Result{Data: map[string]any{"name": in.Name}}, nil
}

// VolumeRemove deletes a volume — and its contents.
//
// Ownership-guarded like a container, and for a sharper reason: this is the one
// operation in the module that destroys data. A volume the panel did not create
// belongs to something else on the host, and "something else on the host" is
// usually a database.
type VolumeRemove struct{}

func (VolumeRemove) Name() string { return "docker.volume.remove" }

func (VolumeRemove) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.volume.remove.")
	}
	if !reVolumeName.MatchString(in.Name) {
		return capability.Result{}, errx.Validation("invalid_volume", "Invalid volume name.")
	}
	if err := requireManagedObject(c, "volume", in.Name); err != nil {
		return capability.Result{}, err
	}
	res, err := runDocker(c, 60*time.Second, "volume", "rm", in.Name)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_volume_remove_failed", "Could not remove the volume.")
	}
	return capability.Result{Data: map[string]any{"name": in.Name}}, nil
}

// VolumeInspect returns a volume's full detail plus the containers that use it.
//
// Not ownership-guarded: reading is never the dangerous half (mutation is, and
// that stays guarded), and an operator deciding whether a volume is safe to
// remove needs to see who is attached to it — including containers the panel did
// not create. The consumer list is a live `ps` filtered by the volume, because a
// name the caller supplies proves nothing; the daemon's own view of what is
// mounted where is the answer.
type VolumeInspect struct{}

func (VolumeInspect) Name() string { return "docker.volume.inspect" }

func (VolumeInspect) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.volume.inspect.")
	}
	if !reVolumeName.MatchString(in.Name) {
		return capability.Result{}, errx.Validation("invalid_volume", "Invalid volume name.")
	}
	res, err := runDocker(c, 20*time.Second, "volume", "inspect", "--format", "{{json .}}", in.Name)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.NotFound("volume_not_found", "No such volume.")
	}
	// Which containers mount this volume. A separate call because `volume inspect`
	// does not report its consumers — docker tracks the reference the other way.
	users, err := runDocker(c, 20*time.Second, "ps", "--all", "--no-trunc",
		"--filter", "volume="+in.Name, "--format", "{{json .}}")
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	return capability.Result{Data: map[string]any{
		"volume":    json.RawMessage(res.Stdout),
		"consumers": linesOf(users.Stdout),
	}}, nil
}

// ── networks ─────────────────────────────────────────────────────────────────

// NetworkList lists networks.
type NetworkList struct{}

func (NetworkList) Name() string { return "docker.network.list" }

func (NetworkList) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	res, err := runDocker(c, 30*time.Second, "network", "ls", "--format", "{{json .}}")
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_networks_failed", "Could not list networks.")
	}
	return capability.Result{Data: map[string]any{"networks": linesOf(res.Stdout)}}, nil
}

// NetworkCreate creates a bridge network the panel owns.
//
// Always a bridge, never `host` or `macvlan`: a container on the host network
// shares the host's stack outright, which discards the isolation that made
// putting the workload in a container worthwhile.
type NetworkCreate struct{}

func (NetworkCreate) Name() string { return "docker.network.create" }

func (NetworkCreate) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
		Site string `json:"site"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.network.create.")
	}
	if !reNetworkName.MatchString(in.Name) {
		return capability.Result{}, errx.Validation("invalid_network", "Invalid network name.")
	}
	args := []string{"network", "create", "--driver", "bridge", "--label", LabelManaged + "=1"}
	if in.Site != "" {
		if err := validateContainerRef(in.Site); err != nil {
			return capability.Result{}, err
		}
		args = append(args, "--label", LabelSite+"="+in.Site)
	}
	args = append(args, in.Name)

	res, err := runDocker(c, 30*time.Second, args...)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_network_create_failed", "Could not create the network.")
	}
	return capability.Result{Data: map[string]any{"name": in.Name}}, nil
}

// NetworkRemove deletes a network the panel owns.
type NetworkRemove struct{}

func (NetworkRemove) Name() string { return "docker.network.remove" }

func (NetworkRemove) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.network.remove.")
	}
	if !reNetworkName.MatchString(in.Name) {
		return capability.Result{}, errx.Validation("invalid_network", "Invalid network name.")
	}
	if err := requireManagedObject(c, "network", in.Name); err != nil {
		return capability.Result{}, err
	}
	res, err := runDocker(c, 30*time.Second, "network", "rm", in.Name)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, dockerFailed(res, "docker_network_remove_failed", "Could not remove the network.")
	}
	return capability.Result{Data: map[string]any{"name": in.Name}}, nil
}

// NetworkInspect returns a network's full detail. Docker's own inspect payload
// already carries the map of connected containers, so unlike a volume this needs
// no second call to answer "who is on this network?". Read-only and unguarded for
// the same reason VolumeInspect is.
type NetworkInspect struct{}

func (NetworkInspect) Name() string { return "docker.network.inspect" }

func (NetworkInspect) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for docker.network.inspect.")
	}
	if !reNetworkName.MatchString(in.Name) {
		return capability.Result{}, errx.Validation("invalid_network", "Invalid network name.")
	}
	res, err := runDocker(c, 20*time.Second, "network", "inspect", "--format", "{{json .}}", in.Name)
	if err != nil {
		return capability.Result{}, errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.NotFound("network_not_found", "No such network.")
	}
	return capability.Result{Data: map[string]any{"network": json.RawMessage(res.Stdout)}}, nil
}

// requireManagedObject is requireManaged for things that are not containers.
// Volumes and networks carry the same label and answer the same inspect, but
// their labels live at the top level rather than under .Config.
func requireManagedObject(c capability.Context, kind, name string) error {
	res, err := runDocker(c, 20*time.Second, kind, "inspect",
		"--format", "{{index .Labels \""+LabelManaged+"\"}}", name)
	if err != nil {
		return errx.Internal(err)
	}
	if res.ExitCode != 0 {
		return errx.NotFound(kind+"_not_found", "No such "+kind+".")
	}
	if strings.TrimSpace(string(res.Stdout)) != "1" {
		return errx.New(errx.KindForbidden, kind+"_not_managed",
			"That "+kind+" was not created by HeroPanel, so the panel will not remove it.")
	}
	return nil
}
