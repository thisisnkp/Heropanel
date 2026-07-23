// Package docker is the in-core Docker module: it lets an operator see and
// control containers on the host.
//
// Nothing here is privileged. Every operation is a named broker capability
// (broker/capabilities/docker.go), because the Docker daemon socket is
// root-equivalent — anyone who can reach it can run `docker run -v /:/host
// --privileged` and own the machine. Putting hpd in the `docker` group would
// have made the network-facing process root by another name, so this package
// only ever asks the broker, exactly as the File Manager does.
//
// It is registered as a module Provider like any other feature, so the UI greys
// Docker out when the host has no daemon rather than offering buttons that fail.
package docker

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	brokerclient "github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Broker is the privileged gateway (a subset of internal/broker.Gateway).
type Broker interface {
	Invoke(ctx context.Context, capability string, input any) (map[string]any, error)
}

// Info is what the panel knows about the host's Docker daemon.
type Info struct {
	Available     bool   `json:"available"`
	ServerVersion string `json:"server_version,omitempty"`
	APIVersion    string `json:"api_version,omitempty"`
	// Reason is the daemon's own message when it is unreachable — "permission
	// denied" and "not installed" need different fixes, and only the daemon can
	// tell them apart.
	Reason string `json:"reason,omitempty"`
}

// Container is one container as the panel presents it.
type Container struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	State   string            `json:"state"`
	Status  string            `json:"status"`
	Ports   string            `json:"ports"`
	Created string            `json:"created"`
	Labels  map[string]string `json:"labels"`
	// Managed says whether the panel created this container — and therefore
	// whether it will act on it at all. The UI shows unmanaged containers
	// read-only rather than hiding them: an admin whose host is out of memory
	// needs to see the container eating it.
	Managed bool   `json:"managed"`
	SiteUID string `json:"site_uid,omitempty"`
}

// Image is one image on the host.
type Image struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       string `json:"size"`
	Created    string `json:"created"`
}

// Stats is a single resource sample for one container.
type Stats struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CPUPerc  string `json:"cpu_perc"`
	MemUsage string `json:"mem_usage"`
	MemPerc  string `json:"mem_perc"`
	NetIO    string `json:"net_io"`
	BlockIO  string `json:"block_io"`
	PIDs     string `json:"pids"`
}

// Logs is a container's captured output.
type Logs struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// Service is the Docker application service.
type Service struct {
	broker Broker

	// Daemon presence is cached briefly. Every page asks "is Docker here?" and
	// the answer changes about as often as a package is installed, so asking the
	// broker on each request would spend a privileged round trip on a constant.
	streams  brokerclient.StreamGateway
	mu       sync.Mutex
	cached   Info
	cachedAt time.Time
}

// infoTTL is how long a daemon-presence answer is reused.
const infoTTL = 30 * time.Second

// New constructs the service. A nil broker disables the module entirely, which
// is the correct state for a panel with no broker rather than an error at every
// call site.
func New(b Broker) *Service {
	if b == nil {
		return nil
	}
	return &Service{broker: b}
}

// Available reports whether a usable daemon is present. It never returns an
// error: "no Docker on this host" is a state to render, not a failure.
func (s *Service) Available(ctx context.Context) bool {
	return s.Info(ctx).Available
}

// Info returns (and briefly caches) the daemon's status.
func (s *Service) Info(ctx context.Context) Info {
	s.mu.Lock()
	if !s.cachedAt.IsZero() && time.Since(s.cachedAt) < infoTTL {
		defer s.mu.Unlock()
		return s.cached
	}
	s.mu.Unlock()

	info := s.probe(ctx)

	s.mu.Lock()
	s.cached, s.cachedAt = info, time.Now()
	s.mu.Unlock()
	return info
}

func (s *Service) probe(ctx context.Context) Info {
	out, err := s.broker.Invoke(ctx, "docker.info", map[string]any{})
	if err != nil {
		return Info{Available: false, Reason: err.Error()}
	}
	if avail, _ := out["available"].(bool); !avail {
		reason, _ := out["reason"].(string)
		return Info{Available: false, Reason: reason}
	}
	info := Info{Available: true}
	// The version payload is docker's own JSON, passed through rather than
	// re-modelled: only two fields are shown, and inventing a schema for the
	// rest would be a maintenance cost with no reader.
	if v, ok := out["version"]; ok {
		var ver struct {
			Server struct {
				Version    string `json:"Version"`
				APIVersion string `json:"ApiVersion"`
			} `json:"Server"`
		}
		if b, err := json.Marshal(v); err == nil {
			_ = json.Unmarshal(b, &ver)
			info.ServerVersion = ver.Server.Version
			info.APIVersion = ver.Server.APIVersion
		}
	}
	return info
}

// requireDaemon turns an absent daemon into one clear error, instead of letting
// each operation fail with whatever docker happened to print.
func (s *Service) requireDaemon(ctx context.Context) error {
	if s.Info(ctx).Available {
		return nil
	}
	return errx.New(errx.KindUnavailable, "docker_unavailable",
		"Docker is not available on this host.")
}

// ListContainers returns containers, optionally scoped to one site. Stopped
// containers are included: a container that exited is exactly the one an
// operator is looking for.
func (s *Service) ListContainers(ctx context.Context, siteUID string) ([]Container, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.container.list",
		map[string]any{"all": true, "site": siteUID})
	if err != nil {
		return nil, err
	}
	rows, err := jsonRows(out["containers"])
	if err != nil {
		return nil, err
	}

	containers := make([]Container, 0, len(rows))
	for _, r := range rows {
		var raw struct {
			ID, Image, Names, State, Status, Ports, CreatedAt, Labels string
		}
		if err := json.Unmarshal(r, &raw); err != nil {
			continue
		}
		labels := parseLabels(raw.Labels)
		containers = append(containers, Container{
			ID: raw.ID, Name: firstName(raw.Names), Image: raw.Image,
			State: raw.State, Status: raw.Status, Ports: raw.Ports,
			Created: raw.CreatedAt, Labels: labels,
			Managed: labels[labelManaged] == "1",
			SiteUID: labels[labelSite],
		})
	}
	return containers, nil
}

// Inspect returns docker's full inspect payload for one container, untouched.
// The panel does not model it: it is docker's schema, it changes with docker,
// and an operator reading it wants what docker actually said.
func (s *Service) Inspect(ctx context.Context, ref string) (json.RawMessage, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.container.inspect", map[string]any{"container": ref})
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(out["container"])
	if err != nil {
		return nil, errx.Internal(err)
	}
	return b, nil
}

// Logs returns a bounded tail of a container's output.
func (s *Service) Logs(ctx context.Context, ref string, tail int, timestamps bool) (*Logs, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.container.logs",
		map[string]any{"container": ref, "tail": tail, "timestamps": timestamps})
	if err != nil {
		return nil, err
	}
	stdout, _ := out["stdout"].(string)
	stderr, _ := out["stderr"].(string)
	return &Logs{Stdout: stdout, Stderr: stderr}, nil
}

// Stats samples resource usage once, for every container or just one.
func (s *Service) Stats(ctx context.Context, ref string) ([]Stats, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.container.stats", map[string]any{"container": ref})
	if err != nil {
		return nil, err
	}
	rows, err := jsonRows(out["stats"])
	if err != nil {
		return nil, err
	}
	stats := make([]Stats, 0, len(rows))
	for _, r := range rows {
		var raw struct {
			ID, Name, CPUPerc, MemUsage, MemPerc, NetIO, BlockIO, PIDs string
		}
		if err := json.Unmarshal(r, &raw); err != nil {
			continue
		}
		stats = append(stats, Stats{
			ID: raw.ID, Name: raw.Name, CPUPerc: raw.CPUPerc, MemUsage: raw.MemUsage,
			MemPerc: raw.MemPerc, NetIO: raw.NetIO, BlockIO: raw.BlockIO, PIDs: raw.PIDs,
		})
	}
	return stats, nil
}

// Action performs a lifecycle operation. The broker refuses any container the
// panel does not manage, so this does not re-check ownership — one enforcement
// point, in the privileged process, is the point.
func (s *Service) Action(ctx context.Context, verb, ref string, force bool) error {
	if err := s.requireDaemon(ctx); err != nil {
		return err
	}
	capName, ok := map[string]string{
		"start":   "docker.container.start",
		"stop":    "docker.container.stop",
		"restart": "docker.container.restart",
		"remove":  "docker.container.remove",
	}[verb]
	if !ok {
		return errx.Validation("invalid_action", "Unknown container action.")
	}
	_, err := s.broker.Invoke(ctx, capName, map[string]any{"container": ref, "force": force})
	return err
}

// ListImages returns the images on the host.
func (s *Service) ListImages(ctx context.Context) ([]Image, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return nil, err
	}
	out, err := s.broker.Invoke(ctx, "docker.image.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	rows, err := jsonRows(out["images"])
	if err != nil {
		return nil, err
	}
	images := make([]Image, 0, len(rows))
	for _, r := range rows {
		var raw struct {
			ID, Repository, Tag, Size, CreatedSince string
		}
		if err := json.Unmarshal(r, &raw); err != nil {
			continue
		}
		images = append(images, Image{
			ID: raw.ID, Repository: raw.Repository, Tag: raw.Tag,
			Size: raw.Size, Created: raw.CreatedSince,
		})
	}
	return images, nil
}

// PullImage fetches an image. It can take minutes, so callers should treat it as
// a long operation rather than a click that blocks a page.
func (s *Service) PullImage(ctx context.Context, image string) (string, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return "", err
	}
	out, err := s.broker.Invoke(ctx, "docker.image.pull", map[string]any{"image": image})
	if err != nil {
		return "", err
	}
	log, _ := out["log"].(string)
	return log, nil
}

// RemoveImage deletes an image. The broker passes docker's own "in use by a
// container" refusal straight back, so removing an image an app still needs
// fails with that reason rather than orphaning the app.
func (s *Service) RemoveImage(ctx context.Context, image string, force bool) (string, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return "", err
	}
	out, err := s.broker.Invoke(ctx, "docker.image.remove", map[string]any{"image": image, "force": force})
	if err != nil {
		return "", err
	}
	log, _ := out["log"].(string)
	return log, nil
}

// PruneImages reclaims disk from images no container needs. With all=false it
// drops only dangling (untagged) layers, which is always safe; all=true also
// removes every image not used by some container.
func (s *Service) PruneImages(ctx context.Context, all bool) (string, error) {
	if err := s.requireDaemon(ctx); err != nil {
		return "", err
	}
	out, err := s.broker.Invoke(ctx, "docker.image.prune", map[string]any{"all": all})
	if err != nil {
		return "", err
	}
	log, _ := out["log"].(string)
	return log, nil
}

// ── parsing docker's CLI output ──────────────────────────────────────────────

// The label keys the broker stamps. Duplicated here rather than imported: hpd
// does not depend on the broker package (they are separate binaries with
// separate privilege), and the wire between them is the capability contract.
const (
	labelManaged = "io.heropanel.managed"
	labelSite    = "io.heropanel.site"
)

// jsonRows normalises the broker's list payload. It survives a round trip
// through map[string]any, where a []json.RawMessage has become []any of strings
// or maps depending on how it was encoded.
func jsonRows(v any) ([]json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		if rows, ok := v.([]json.RawMessage); ok {
			return rows, nil
		}
		return nil, errx.Internal(errx.New(errx.KindInternal, "bad_docker_payload",
			"The broker returned an unexpected shape for a Docker listing."))
	}
	out := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		switch t := it.(type) {
		case string:
			out = append(out, json.RawMessage(t))
		default:
			if b, err := json.Marshal(t); err == nil {
				out = append(out, b)
			}
		}
	}
	return out, nil
}

// parseLabels reads docker's CLI label rendering: "k=v,k2=v2".
//
// This format is lossy — a label value containing a comma cannot be
// distinguished from a separator — which is tolerable here because the only
// labels the panel *decides* on are its own, and it writes them itself. Anything
// mis-split belongs to someone else's container, which the panel will not touch
// regardless.
func parseLabels(s string) map[string]string {
	labels := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}
		k, v, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		labels[k] = v
	}
	return labels
}

// firstName takes the primary name from docker's comma-separated Names field and
// drops the leading slash the API adds.
func firstName(s string) string {
	name, _, _ := strings.Cut(s, ",")
	return strings.TrimPrefix(strings.TrimSpace(name), "/")
}

// reLoopbackPort matches one loopback publish in docker's Ports string, e.g.
// "127.0.0.1:39000->2368/tcp". Only loopback mappings count toward the used set:
// those are the ports the panel's proxy story is built on.
var reLoopbackPort = regexp.MustCompile(`127\.0\.0\.1:(\d+)->`)

// parseLoopbackPorts pulls every loopback host port from a Ports string. The
// field can list several mappings, comma-separated; all are returned.
func parseLoopbackPorts(ports string) []int {
	var out []int
	for _, m := range reLoopbackPort.FindAllStringSubmatch(ports, -1) {
		if p, err := strconv.Atoi(m[1]); err == nil && p >= 1 && p <= 65535 {
			out = append(out, p)
		}
	}
	return out
}

// ClampTail bounds a caller-supplied log tail. The broker clamps too; this keeps
// the API's documented maximum honest rather than silently differing from it.
func ClampTail(n int) int {
	const max = 2000
	if n <= 0 || n > max {
		return max
	}
	return n
}
