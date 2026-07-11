// Package webserver renders OpenLiteSpeed configuration for hosted sites and
// applies it through the privileged broker. hpd owns rendering (from DB state);
// the broker writes the validated config, tests it, and reloads (ADR-0007,
// docs/05 §2). The full desired state is applied each time, so there is no
// per-site config drift.
package webserver

import (
	"bytes"
	"context"
	"strings"
	"text/template"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Site is the per-site information needed to render a vhost.
type Site struct {
	VhostName     string   // internal id, e.g. "hps1"
	PrimaryDomain string   // e.g. "acme.example.com"
	Domains       []string // all domains mapped to this vhost
	DocumentRoot  string   // e.g. /srv/heropanel/sites/1/public
	Home          string   // e.g. /srv/heropanel/sites/1
	LogDir        string   // e.g. /srv/heropanel/sites/1/logs
	IsPHP         bool
	FpmSocket     string // uds path (php only)
	PhpBin        string // lsphp binary path (php only)
}

// Applier applies the desired web-server state.
type Applier interface {
	Apply(ctx context.Context, sites []Site) error
}

// Service renders and applies OpenLiteSpeed configuration via the broker.
type Service struct {
	broker broker.Gateway
}

// NewService constructs the webserver Service.
func NewService(gw broker.Gateway) *Service { return &Service{broker: gw} }

var _ Applier = (*Service)(nil)

type vhostPayload struct {
	Name   string `json:"name"`
	Config string `json:"config"`
}

// Apply renders every site's vhost plus the aggregate listener and asks the
// broker to apply, test, and reload.
func (s *Service) Apply(ctx context.Context, sites []Site) error {
	if s.broker == nil {
		return errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; the web server cannot be configured.")
	}

	vhosts := make([]vhostPayload, 0, len(sites))
	for i := range sites {
		cfg, err := RenderVhost(sites[i])
		if err != nil {
			return err
		}
		vhosts = append(vhosts, vhostPayload{Name: sites[i].VhostName, Config: cfg})
	}
	listener, err := RenderListener(sites)
	if err != nil {
		return err
	}

	_, err = s.broker.Invoke(ctx, "webserver.apply", map[string]any{
		"vhosts":   vhosts,
		"listener": listener,
	})
	return err
}

var tmplFuncs = template.FuncMap{
	"join": func(domains []string) string { return strings.Join(domains, ", ") },
}

var vhostTmpl = template.Must(template.New("vhost").Parse(`docRoot                   {{.DocumentRoot}}
vhDomain                  {{.PrimaryDomain}}
enableGzip                1
index  {
  useServer               0
  indexFiles              index.html, index.htm{{if .IsPHP}}, index.php{{end}}
  autoIndex               0
}
errorlog {{.LogDir}}/error.log {
  useServer               0
  logLevel                WARN
}
accesslog {{.LogDir}}/access.log {
  useServer               0
}
{{- if .IsPHP}}
scripthandler  {
  add                     lsapi:{{.VhostName}}_lsphp php
}
extprocessor {{.VhostName}}_lsphp {
  type                    lsapi
  address                 uds://{{.FpmSocket}}
  maxConns                10
  path                    {{.PhpBin}}
  autoStart               0
}
{{- end}}
`))

// RenderVhost renders a single site's OpenLiteSpeed vhost config.
func RenderVhost(s Site) (string, error) {
	var b bytes.Buffer
	if err := vhostTmpl.Execute(&b, s); err != nil {
		return "", errx.Wrap(err, errx.KindInternal, "vhost_render_failed", "Could not render the vhost config.")
	}
	return b.String(), nil
}

var listenerTmpl = template.Must(template.New("listener").Funcs(tmplFuncs).Parse(`{{range .}}virtualhost {{.VhostName}} {
  vhRoot                  {{.Home}}
  configFile              /usr/local/lsws/conf/vhosts/{{.VhostName}}/vhconf.conf
  allowSymbolLink         1
  enableScript            1
  restrained              1
}
{{end}}listener HeroPanelHTTP {
  address                 *:80
  secure                  0
{{range .}}  map                     {{.VhostName}} {{join .Domains}}
{{end}}}
`))

// RenderListener renders the aggregate listener + virtualhost declarations.
func RenderListener(sites []Site) (string, error) {
	var b bytes.Buffer
	if err := listenerTmpl.Execute(&b, sites); err != nil {
		return "", errx.Wrap(err, errx.KindInternal, "listener_render_failed", "Could not render the listener config.")
	}
	return b.String(), nil
}
