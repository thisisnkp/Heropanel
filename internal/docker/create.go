package docker

import (
	"context"
	"encoding/json"

	brokerclient "github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Creating containers, volumes and networks.
//
// The service does almost no validation of its own here, and that is deliberate:
// every rule that matters — no host bind mounts, loopback-only ports, the
// restart-policy allowlist, environment travelling by stdin rather than argv —
// is enforced in the broker, where it cannot be skipped by a caller that
// forgets. Duplicating those checks in hpd would create two definitions of the
// same boundary, and the weaker one would eventually win.

// PortMapping publishes a container port on the host. The bind address is not a
// field: the broker always binds loopback.
type PortMapping struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Proto     string `json:"proto,omitempty"`
}

// VolumeMount attaches a *named volume*. There is no host-path field by design.
type VolumeMount struct {
	Volume   string `json:"volume"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

// ContainerSpec describes a container to create.
type ContainerSpec struct {
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Site     string            `json:"site,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Ports    []PortMapping     `json:"ports,omitempty"`
	Volumes  []VolumeMount     `json:"volumes,omitempty"`
	Restart  string            `json:"restart,omitempty"`
	Network  string            `json:"network,omitempty"`
	MemoryMB int               `json:"memory_mb,omitempty"`
	Command  []string          `json:"command,omitempty"`
}

// Volume is one docker volume.
type Volume struct {
	Name    string            `json:"name"`
	Driver  string            `json:"driver"`
	Labels  map[string]string `json:"labels"`
	Managed bool              `json:"managed"`
	SiteUID string            `json:"site_uid,omitempty"`
}

// Network is one docker network.
type Network struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Driver  string            `json:"driver"`
	Scope   string            `json:"scope"`
	Labels  map[string]string `json:"labels"`
	Managed bool              `json:"managed"`
	SiteUID string            `json:"site_uid,omitempty"`
}

// CreateContainer starts a container the panel owns.
func (s *Service) CreateContainer(ctx context.Context, spec ContainerSpec) (string, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return "", err
	}
	if spec.Name == "" || spec.Image == "" {
		return "", errx.Validation("name_and_image_required", "A name and an image are required.")
	}
	// The image must be on the host before `run`, or docker pulls it inline and
	// the create appears to hang for however long a multi-gigabyte download
	// takes — past the request budget, with no progress anywhere.
	if _, err := s.PullImage(ctx, spec.Image); err != nil {
		return "", err
	}
	out, err := s.broker.Invoke(ctx, "docker.container.create", spec)
	if err != nil {
		return "", err
	}
	id, _ := out["id"].(string)
	return id, nil
}

// ListVolumes returns the host's volumes, each flagged with panel ownership.
func (s *Service) ListVolumes(ctx context.Context) ([]Volume, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.volume.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	rows, err := jsonRows(out["volumes"])
	if err != nil {
		return nil, err
	}
	volumes := make([]Volume, 0, len(rows))
	for _, r := range rows {
		var raw struct{ Name, Driver, Labels string }
		if err := json.Unmarshal(r, &raw); err != nil {
			continue
		}
		labels := parseLabels(raw.Labels)
		volumes = append(volumes, Volume{
			Name: raw.Name, Driver: raw.Driver, Labels: labels,
			Managed: labels[labelManaged] == "1", SiteUID: labels[labelSite],
		})
	}
	return volumes, nil
}

// VolumeConsumer is one container attached to a volume, as the detail view lists
// it. It intentionally includes unmanaged containers: an operator weighing
// whether a volume is safe to delete needs to see everything mounted on it.
type VolumeConsumer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	State string `json:"state"`
}

// VolumeDetail is a volume's full record plus the containers that mount it.
type VolumeDetail struct {
	Volume    json.RawMessage  `json:"volume"`
	Consumers []VolumeConsumer `json:"consumers"`
}

// InspectVolume returns a volume's detail and its consumers. Read-only, so it is
// not ownership-guarded — the panel shows the truth about the host, and answering
// "who is using this?" is exactly what makes the destructive remove safe to
// offer.
func (s *Service) InspectVolume(ctx context.Context, name string) (*VolumeDetail, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.volume.inspect", map[string]any{"name": name})
	if err != nil {
		return nil, err
	}
	vol, err := json.Marshal(out["volume"])
	if err != nil {
		return nil, errx.Internal(err)
	}
	rows, err := jsonRows(out["consumers"])
	if err != nil {
		return nil, err
	}
	consumers := make([]VolumeConsumer, 0, len(rows))
	for _, r := range rows {
		var raw struct{ ID, Names, Image, State string }
		if err := json.Unmarshal(r, &raw); err != nil {
			continue
		}
		consumers = append(consumers, VolumeConsumer{
			ID: raw.ID, Name: firstName(raw.Names), Image: raw.Image, State: raw.State,
		})
	}
	return &VolumeDetail{Volume: vol, Consumers: consumers}, nil
}

// InspectNetwork returns a network's full record. Docker's inspect payload
// already carries the connected containers, so this needs no second call.
func (s *Service) InspectNetwork(ctx context.Context, name string) (json.RawMessage, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.network.inspect", map[string]any{"name": name})
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(out["network"])
	if err != nil {
		return nil, errx.Internal(err)
	}
	return b, nil
}

// CreateVolume creates a named volume the panel owns.
func (s *Service) CreateVolume(ctx context.Context, name, site string) error {
	if err := s.requireDaemon(ctx); err != nil {
		return err
	}
	_, err := s.broker.Invoke(ctx, "docker.volume.create", map[string]any{"name": name, "site": site})
	return err
}

// RemoveVolume deletes a volume and everything in it.
func (s *Service) RemoveVolume(ctx context.Context, name string) error {
	if err := s.requireDaemon(ctx); err != nil {
		return err
	}
	_, err := s.broker.Invoke(ctx, "docker.volume.remove", map[string]any{"name": name})
	return err
}

// ListNetworks returns the host's networks.
func (s *Service) ListNetworks(ctx context.Context) ([]Network, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.network.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	rows, err := jsonRows(out["networks"])
	if err != nil {
		return nil, err
	}
	networks := make([]Network, 0, len(rows))
	for _, r := range rows {
		var raw struct{ ID, Name, Driver, Scope, Labels string }
		if err := json.Unmarshal(r, &raw); err != nil {
			continue
		}
		labels := parseLabels(raw.Labels)
		networks = append(networks, Network{
			ID: raw.ID, Name: raw.Name, Driver: raw.Driver, Scope: raw.Scope, Labels: labels,
			Managed: labels[labelManaged] == "1", SiteUID: labels[labelSite],
		})
	}
	return networks, nil
}

// CreateNetwork creates a bridge network the panel owns.
func (s *Service) CreateNetwork(ctx context.Context, name, site string) error {
	if err := s.requireDaemon(ctx); err != nil {
		return err
	}
	_, err := s.broker.Invoke(ctx, "docker.network.create", map[string]any{"name": name, "site": site})
	return err
}

// RemoveNetwork deletes a network the panel owns.
func (s *Service) RemoveNetwork(ctx context.Context, name string) error {
	if err := s.requireDaemon(ctx); err != nil {
		return err
	}
	_, err := s.broker.Invoke(ctx, "docker.network.remove", map[string]any{"name": name})
	return err
}

// ExecCapability is the broker capability that opens a shell inside a
// container. It is a *stream*, so it is opened with OpenStream rather than
// Invoke — the same upgrade the site terminal uses.
const ExecCapability = "docker.container.exec"

// The stream types are the broker client's own, not re-declared here. A
// narrower local interface looked tidier until it needed an adapter in the
// middle — and an adapter over a byte stream is a place for frame boundaries to
// go wrong for no benefit.

// WithStreams enables container shells. Without it the module still manages
// containers; it simply cannot open one, which is the honest degraded state for
// a panel wired to a broker that cannot stream.
func (s *Service) WithStreams(g brokerclient.StreamGateway) *Service {
	s.streams = g
	return s
}

// ExecEnabled reports whether container shells can be opened at all, so the UI
// can hide the button rather than offer one that always fails.
func (s *Service) ExecEnabled() bool { return s != nil && s.streams != nil }

// OpenExec starts a shell inside a container. Ownership is enforced in the
// broker — a shell inside a container the panel did not create would bypass
// every other refusal in the module, since you could simply stop the process
// from within.
func (s *Service) OpenExec(ctx context.Context, container, shell string, cols, rows uint16) (brokerclient.Stream, error) {
	if s.streams == nil {
		return nil, errx.New(errx.KindUnavailable, "exec_unavailable",
			"Container shells require the privileged broker, which is not configured.")
	}
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	return s.streams.OpenStream(ctx, ExecCapability, map[string]any{
		"container": container, "shell": shell, "cols": cols, "rows": rows,
	})
}

// PublishedPorts reports every loopback host port a container on the host
// currently publishes. App port allocation reads it to avoid handing out a port
// that is already bound — the check that makes deploys safe across a restart.
//
// It scans *all* containers, not only managed ones: docker rejects a bind to any
// host port already in use, whoever owns it, so the free set has to account for
// everything on the host.
func (s *Service) PublishedPorts(ctx context.Context) ([]int, error) {
	containers, err := s.ListContainers(ctx, "")
	if err != nil {
		return nil, err
	}
	var ports []int
	for _, c := range containers {
		ports = append(ports, parseLoopbackPorts(c.Ports)...)
	}
	return ports, nil
}

// LogsFollowCapability is the broker capability that streams a container's logs
// live. Like exec it is a *stream*, opened with OpenStream rather than Invoke.
const LogsFollowCapability = "docker.container.logs.follow"

// LogStreamEnabled reports whether live log streaming can be opened at all, so
// the UI offers a follow toggle only when the broker can actually stream. It is
// the same condition as exec (both need a streaming gateway), but named
// separately so a caller reads the intent rather than borrowing exec's.
func (s *Service) LogStreamEnabled() bool { return s != nil && s.streams != nil }

// OpenLogStream starts following a container's logs. Not ownership-gated — the
// broker allows reading any container's logs, the same as the polled read — but
// audited there, because logs carry secrets.
func (s *Service) OpenLogStream(ctx context.Context, container string, tail int, timestamps bool) (brokerclient.Stream, error) {
	if s.streams == nil {
		return nil, errx.New(errx.KindUnavailable, "logs_stream_unavailable",
			"Live log streaming requires the privileged broker, which is not configured.")
	}
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	return s.streams.OpenStream(ctx, LogsFollowCapability, map[string]any{
		"container": container, "tail": ClampTail(tail), "timestamps": timestamps,
	})
}

// ── compose ──────────────────────────────────────────────────────────────────

// ComposeService is one service line from `docker compose ps`.
type ComposeService struct {
	Name    string `json:"name"`
	Service string `json:"service"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Ports   string `json:"ports"`
}

// ComposeUp brings a stack up from a compose file. The file is user-authored
// YAML and can ask for anything docker compose understands — this is the module's
// explicit escape hatch, and the honest boundary is that the broker labels and
// scopes the stack but does not (and cannot) harden arbitrary compose the way it
// hardens a container it builds itself.
func (s *Service) ComposeUp(ctx context.Context, project, site, file string) (string, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return "", err
	}
	if project == "" || file == "" {
		return "", errx.Validation("project_and_file_required", "A project name and a compose file are required.")
	}
	out, err := s.broker.Invoke(ctx, "docker.compose.up",
		map[string]any{"project": project, "site": site, "file": file})
	if err != nil {
		return "", err
	}
	log, _ := out["log"].(string)
	return log, nil
}

// ComposeDown tears a stack down (containers and networks, never volumes).
func (s *Service) ComposeDown(ctx context.Context, project string) error {
	if err := s.requireDaemon(ctx); err != nil {
		return err
	}
	_, err := s.broker.Invoke(ctx, "docker.compose.down", map[string]any{"project": project})
	return err
}

// ComposePs lists a stack's services.
func (s *Service) ComposePs(ctx context.Context, project string) ([]ComposeService, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.compose.ps", map[string]any{"project": project})
	if err != nil {
		return nil, err
	}
	rows, err := jsonRows(out["services"])
	if err != nil {
		return nil, err
	}
	services := make([]ComposeService, 0, len(rows))
	for _, r := range rows {
		// Only the string fields are modelled. compose also emits a "Publishers"
		// array, which is deliberately *not* a field here: declaring it as a
		// string (its display form) makes json.Unmarshal fail on the array and
		// drop the whole row, so a running stack lists as empty. The "Ports"
		// string carries the same information for display.
		var raw struct {
			Name, Service, Image, State, Status, Ports string
		}
		if err := json.Unmarshal(r, &raw); err != nil {
			continue
		}
		services = append(services, ComposeService{
			Name: raw.Name, Service: raw.Service, Image: raw.Image,
			State: raw.State, Status: raw.Status, Ports: raw.Ports,
		})
	}
	return services, nil
}

// ComposeLogs returns a bounded tail of the whole stack's output.
func (s *Service) ComposeLogs(ctx context.Context, project string, tail int) (*Logs, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.compose.logs",
		map[string]any{"project": project, "tail": ClampTail(tail)})
	if err != nil {
		return nil, err
	}
	stdout, _ := out["stdout"].(string)
	stderr, _ := out["stderr"].(string)
	return &Logs{Stdout: stdout, Stderr: stderr}, nil
}
