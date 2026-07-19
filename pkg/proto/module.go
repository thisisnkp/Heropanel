// Package proto defines the module contract — the manifest a satellite module
// ships and the lifecycle types hpd exchanges with it — as plain Go types.
//
// The name is `proto` because docs/06 describes this contract as gRPC, and one
// day it will be: the satellite tier (Phase 9/10) transports these over gRPC on
// a Unix socket. But that day is not today, and committing to protobuf now would
// pull in the toolchain and the codegen for a wire nobody speaks yet.
//
// So this is ADR-0007's discipline applied a second time. The broker already
// proved that a transport-agnostic contract expressed in Go — length-prefixed
// JSON there — lets the wire format be chosen late without the callers caring.
// These types are that contract for modules: an in-process module and a future
// gRPC module both satisfy the same Go interfaces ([pkg/plugin]), and the
// registry ([internal/registry]) treats them identically. When gRPC lands, it
// serializes *these* types; it does not replace them.
//
// The types therefore carry JSON tags (the manifest is read from `module.yaml`
// and this is how config crosses the current in-process boundary) and stay free
// of any transport detail.
package proto

// APIVersion is the module-contract version hpd speaks. A module declares the
// version it was built against in its manifest; hpd refuses to enable one it is
// not compatible with (see Compatible), because a lifecycle mismatch is how a
// module ends up half-wired — advertising capabilities it cannot actually serve.
const APIVersion = "heropanel.io/v1"

// Manifest is a module's `module.yaml` (docs/06 §2): who it is, what it
// provides, what it needs, and how it is verified. It is data, not behaviour —
// hpd reads it to decide whether and how to run the module, before any code from
// the module runs.
type Manifest struct {
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string   `json:"kind"       yaml:"kind"`
	Metadata   Metadata `json:"metadata"   yaml:"metadata"`
	Spec       Spec     `json:"spec"       yaml:"spec"`
}

// Metadata identifies a module.
type Metadata struct {
	Slug        string `json:"slug"        yaml:"slug"`
	Name        string `json:"name"        yaml:"name"`
	Version     string `json:"version"     yaml:"version"`
	Description string `json:"description" yaml:"description"`
	Category    string `json:"category"    yaml:"category"`
	// Icon is a key resolved by the UI's icon set, never a copied asset: a
	// manifest ships a name like "box", not an SVG, so a third-party module
	// cannot smuggle markup into the panel chrome.
	Icon string `json:"icon" yaml:"icon"`
}

// Spec is how a module runs and what it is allowed to reach.
type Spec struct {
	// Binary is the module executable, resolved per-arch at install time via
	// pkg/arch — the manifest names "hp-mod-docker", the installer fetches
	// "hp-mod-docker-linux-arm64".
	Binary string `json:"binary" yaml:"binary"`
	// Socket is where the module listens; hpd dials in (the module never dials
	// out — docs/06 §3, the handshake direction that lets hpd stay in control).
	Socket string `json:"socket" yaml:"socket"`
	RunAs  RunAs  `json:"runAs"  yaml:"runAs"`
	// Capabilities are what the module provides. The registry advertises these
	// once the module is enabled, and services gate features on them.
	Capabilities []string `json:"capabilities" yaml:"capabilities"`
	// RequiresBroker is the module's allowlist of privileged operations. A
	// module cannot reach the broker directly; hpd mediates and refuses any
	// broker call not named here. This is the security seam that keeps a
	// compromised module from becoming root — it is bounded by what it declared.
	RequiresBroker []string     `json:"requiresBroker" yaml:"requiresBroker"`
	Dependencies   Dependencies `json:"dependencies"   yaml:"dependencies"`
	// Arch is the set of architectures a build exists for; the installer picks
	// the matching one and refuses a host whose arch is absent.
	Arch      []string  `json:"arch"      yaml:"arch"`
	Resources Resources `json:"resources" yaml:"resources"`
	Health    Health    `json:"health"    yaml:"health"`
	Signing   Signing   `json:"signing"   yaml:"signing"`
}

// RunAs is a module's least-privilege identity.
type RunAs struct {
	User   string   `json:"user"   yaml:"user"`
	Groups []string `json:"groups" yaml:"groups"`
}

// Dependencies are host services and other modules a module needs before it can
// enable.
type Dependencies struct {
	Services []string `json:"services" yaml:"services"`
	Modules  []string `json:"modules"  yaml:"modules"`
}

// Resources are advisory limits rendered into the module's systemd unit — the
// same MemoryMax/CPUQuota discipline the site slice uses.
type Resources struct {
	MemoryMax string `json:"memoryMax" yaml:"memoryMax"`
	CPUQuota  string `json:"cpuQuota"  yaml:"cpuQuota"`
}

// Health is how hpd probes the module once it is running.
type Health struct {
	Endpoint    string `json:"endpoint"    yaml:"endpoint"`
	IntervalSec int    `json:"intervalSec" yaml:"intervalSec"`
}

// Signing binds a module to a checksum and signature, verified against a pinned
// key at install time. An unsigned or unknown-key module is refused unless the
// operator has explicitly opened a dev channel — the manifest alone is never
// trusted to be what it says.
type Signing struct {
	Checksum  string `json:"checksum"  yaml:"checksum"`
	Signature string `json:"signature" yaml:"signature"`
}
