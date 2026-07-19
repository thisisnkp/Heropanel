package proto

import (
	"fmt"
	"regexp"
	"strings"
)

// The lifecycle contract (docs/06 §3). These are the requests and responses hpd
// exchanges with a module across its life: a handshake on connect, health
// probes, config pushes, graceful shutdown. Expressed as Go types so an
// in-process module and a future gRPC module answer the same shapes.

// ServingState is a module's health, mirroring gRPC's health convention so the
// eventual transport is a direct mapping rather than a translation.
type ServingState string

const (
	Serving    ServingState = "SERVING"
	NotServing ServingState = "NOT_SERVING"
	Degraded   ServingState = "DEGRADED"
)

// HandshakeResponse is what a module answers when hpd first connects. hpd checks
// APIVersion for compatibility before it will enable the module; a mismatch
// leaves the module in `error`, never half-enabled.
type HandshakeResponse struct {
	APIVersion   string   `json:"apiVersion"`
	Slug         string   `json:"slug"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

// HealthResponse reports serving state plus a human-readable detail for the
// panel to show when a module is degraded.
type HealthResponse struct {
	State  ServingState `json:"state"`
	Detail string       `json:"detail,omitempty"`
}

// ConfigureRequest pushes validated config to a module for a hot reload. The
// config has already been checked against the module's schema by hpd, so the
// module can apply it without re-validating the shape.
type ConfigureRequest struct {
	ConfigJSON []byte `json:"config_json"`
}

// ConfigureResponse reports whether the module accepted the config.
type ConfigureResponse struct {
	Accepted bool   `json:"accepted"`
	Detail   string `json:"detail,omitempty"`
}

// State is a module's position in the lifecycle state machine (docs/06 §4). It
// lives here, with the contract, because both hpd and the registry reason about
// it and it must mean the same thing to each.
type State string

const (
	StateNone      State = "none"      // not installed
	StateInstalled State = "installed" // binary + manifest present; no process
	StateEnabled   State = "enabled"   // unit up; handshake done
	StateRunning   State = "running"   // enabled and healthy
	StateDegraded  State = "degraded"  // running but unhealthy
	StateError     State = "error"     // failed to enable, or crash-looped
)

// reSlug bounds a module slug. It becomes part of a systemd unit name
// (heropanel-mod@<slug>.service), a socket path, and an install directory, so it
// is confined to the characters that are safe in all three — no separators that
// could climb a path or split a unit name.
var reSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// reCapability bounds a capability name. Capabilities are dotted identifiers
// ("docker.compose", "app.template.deploy"); the registry keys on them and
// services compare against them, so an odd character here is a capability that
// can never match and a feature that can never unlock.
var reCapability = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// Validate checks a manifest is well-formed and compatible enough to act on. It
// is the gate before hpd does anything with a module — the manifest is untrusted
// input (it arrives with a downloaded package), so nothing derived from it
// touches the system until this passes.
func (m *Manifest) Validate() error {
	if m.APIVersion != APIVersion {
		return fmt.Errorf("proto: unsupported apiVersion %q (this hpd speaks %q)", m.APIVersion, APIVersion)
	}
	if m.Kind != "Module" {
		return fmt.Errorf("proto: manifest kind is %q, want %q", m.Kind, "Module")
	}
	if !reSlug.MatchString(m.Metadata.Slug) {
		return fmt.Errorf("proto: invalid module slug %q", m.Metadata.Slug)
	}
	if strings.TrimSpace(m.Metadata.Version) == "" {
		return fmt.Errorf("proto: module %q has no version", m.Metadata.Slug)
	}
	if strings.TrimSpace(m.Spec.Binary) == "" {
		return fmt.Errorf("proto: module %q declares no binary", m.Metadata.Slug)
	}
	if len(m.Spec.Capabilities) == 0 {
		return fmt.Errorf("proto: module %q advertises no capabilities", m.Metadata.Slug)
	}
	for _, c := range m.Spec.Capabilities {
		if !reCapability.MatchString(c) {
			return fmt.Errorf("proto: module %q has invalid capability %q", m.Metadata.Slug, c)
		}
	}
	if len(m.Spec.Arch) == 0 {
		return fmt.Errorf("proto: module %q lists no architectures", m.Metadata.Slug)
	}
	return nil
}

// Compatible reports whether a module built for apiVersion can run against this
// hpd. It is intentionally exact today: the contract is v1 and there is no range
// to negotiate yet. It exists as the single decision point so that when a v2
// arrives, the semver range lives here and nowhere else has to learn about it.
func Compatible(apiVersion string) bool {
	return apiVersion == APIVersion
}
