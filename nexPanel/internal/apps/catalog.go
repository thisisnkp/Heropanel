// Package apps is the one-click application catalog: curated templates that
// deploy as Docker compose stacks, with generated secrets and a memory
// feasibility check before anything is pulled.
//
// It sits entirely on top of the Docker module — an app *is* a labelled compose
// stack — so it inherits that module's ownership boundary and adds nothing to
// the privilege surface. The value it adds is the parts that make a one-click
// deploy safe rather than merely quick: a secret that is generated rather than
// defaulted, a memory check that refuses a deploy the host cannot run instead of
// letting the OOM killer discover it, and ports bound to loopback so an app is
// reachable only through a reverse proxy the operator puts in front.
package apps

import (
	"sort"
)

// Field is one value the operator supplies when deploying a template.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	// Secret marks a value that is generated, not entered: the operator never
	// sees or chooses it, so it cannot be weak or reused. Rendered into the
	// stack's environment and shown once on success.
	Secret bool `json:"secret"`
	// Required fields with no value block the deploy.
	Required bool   `json:"required"`
	Help     string `json:"help,omitempty"`
}

// Template is one deployable application.
type Template struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Icon        string `json:"icon"` // an icon *key*, resolved by the UI — no copied assets
	// MinMemoryMB is the floor below which the app will not run usefully. It is
	// the number the feasibility check compares against available memory, and it
	// is deliberately the app's realistic minimum, not its comfortable one.
	MinMemoryMB int `json:"min_memory_mb"`
	// HTTPPort is the container port the app serves on, published to loopback so
	// a reverse proxy can front it. Zero for apps with no web UI.
	HTTPPort int     `json:"http_port"`
	Fields   []Field `json:"fields"`
	// compose renders the stack. It is a function rather than a static string so
	// a template can weave generated secrets and operator values into the YAML
	// without a second templating language in the mix.
	compose func(in RenderInput) string
}

// RenderInput is what a template's compose function is given.
type RenderInput struct {
	Project string
	// Values holds every field keyed by its Key: operator entries and generated
	// secrets together, already validated.
	Values map[string]string
	// HostPort is the loopback port the app's HTTP port is published on, chosen
	// by the service so two apps never collide.
	HostPort int
}

// Catalog is the built-in template set, keyed by slug.
var catalog = map[string]Template{}

func register(t Template) { catalog[t.Slug] = t }

// Get returns a template by slug.
func Get(slug string) (Template, bool) {
	t, ok := catalog[slug]
	return t, ok
}

// All returns every template, sorted by name for a stable catalog.
func All() []Template {
	out := make([]Template, 0, len(catalog))
	for _, t := range catalog {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Render produces the compose YAML for a template and an input.
func (t Template) Render(in RenderInput) string { return t.compose(in) }
