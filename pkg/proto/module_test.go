package proto_test

import (
	"testing"

	"github.com/thisisnkp/heropanel/pkg/proto"
)

func validManifest() proto.Manifest {
	return proto.Manifest{
		APIVersion: proto.APIVersion,
		Kind:       "Module",
		Metadata:   proto.Metadata{Slug: "docker", Name: "Docker", Version: "1.4.2"},
		Spec: proto.Spec{
			Binary:       "hp-mod-docker",
			Capabilities: []string{"docker.container", "docker.compose"},
			Arch:         []string{"amd64", "arm64"},
		},
	}
}

func TestValidManifestPasses(t *testing.T) {
	m := validManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
}

// The manifest arrives with a downloaded package: it is untrusted, and nothing
// derived from it may touch the system until Validate passes. Each of these is a
// way it could be malformed.
func TestValidateRejectsMalformedManifests(t *testing.T) {
	cases := map[string]func(*proto.Manifest){
		"wrong api version": func(m *proto.Manifest) { m.APIVersion = "heropanel.io/v2" },
		"wrong kind":        func(m *proto.Manifest) { m.Kind = "Pod" },
		"empty slug":        func(m *proto.Manifest) { m.Metadata.Slug = "" },
		"slug with slash":   func(m *proto.Manifest) { m.Metadata.Slug = "../etc" },
		"slug with space":   func(m *proto.Manifest) { m.Metadata.Slug = "my module" },
		"slug uppercase":    func(m *proto.Manifest) { m.Metadata.Slug = "Docker" },
		"no version":        func(m *proto.Manifest) { m.Metadata.Version = "" },
		"no binary":         func(m *proto.Manifest) { m.Spec.Binary = "" },
		"no capabilities":   func(m *proto.Manifest) { m.Spec.Capabilities = nil },
		"bad capability":    func(m *proto.Manifest) { m.Spec.Capabilities = []string{"NotDotted"} },
		"no arch":           func(m *proto.Manifest) { m.Spec.Arch = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := validManifest()
			mutate(&m)
			if err := m.Validate(); err == nil {
				t.Errorf("Validate accepted a manifest with: %s", name)
			}
		})
	}
}

// A slug becomes a systemd unit name, a socket path, and an install directory. A
// separator in it could climb a path or split a unit name.
func TestSlugCannotContainPathOrUnitSeparators(t *testing.T) {
	for _, slug := range []string{"a/b", "a.b", "a@b", "a b", "-lead", "UP", "x/../y"} {
		m := validManifest()
		m.Metadata.Slug = slug
		if err := m.Validate(); err == nil {
			t.Errorf("Validate accepted dangerous slug %q", slug)
		}
	}
}

func TestCapabilityMustBeDotted(t *testing.T) {
	good := []string{"docker.compose", "app.template.deploy", "a.b.c"}
	bad := []string{"single", "Trailing.", ".leading", "has space", "UP.per"}
	for _, c := range good {
		m := validManifest()
		m.Spec.Capabilities = []string{c}
		if err := m.Validate(); err != nil {
			t.Errorf("rejected valid capability %q: %v", c, err)
		}
	}
	for _, c := range bad {
		m := validManifest()
		m.Spec.Capabilities = []string{c}
		if err := m.Validate(); err == nil {
			t.Errorf("accepted invalid capability %q", c)
		}
	}
}

func TestCompatibleIsExactForV1(t *testing.T) {
	if !proto.Compatible(proto.APIVersion) {
		t.Error("the current API version is not compatible with itself")
	}
	if proto.Compatible("heropanel.io/v2") {
		t.Error("a future API version reported compatible")
	}
	if proto.Compatible("") {
		t.Error("an empty API version reported compatible")
	}
}
