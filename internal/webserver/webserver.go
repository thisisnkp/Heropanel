// Package webserver renders web-server configuration for hosted sites and applies
// it through the privileged broker. npd owns rendering (from DB state); the broker
// writes the validated config, tests it, and reloads (ADR-0007, docs/05 §2). The
// full desired state is rendered into a single included config file per engine, so
// there is no per-site config drift.
//
// NexPanel is a single-stack panel: OpenLiteSpeed is the web server, and the one
// alternative is its commercial sibling.
//   - OpenLiteSpeed — the default and the only engine a normal install runs. Its
//     own config format: a single listener config with inline virtual hosts and
//     server-level FastCGI/proxy extProcessors.
//   - LiteSpeed Enterprise — the licensed upgrade. It is a drop-in Apache
//     replacement and parses Apache's config format, so it gets its own
//     httpd-syntax renderer; only the broker's write/reload path differs.
//
// Nginx and Apache were removed rather than left unsupported: an engine nobody
// installs is an engine nobody tests, and its renderer rots into a liability
// that still has to compile, still has to be reasoned about in every change to
// the Site struct, and would eventually ship a broken vhost to whoever tried it.
//
// OLS specifics learned against real OpenLiteSpeed:
//   - The PHP handler must be declared as a server-level (top-level)
//     extProcessor; a per-site FastCGI pool gets its own top-level extProcessor
//     (unique name + socket), and the vhost references it via scriptHandler.
//   - vhost blocks must be inline (a `configFile` include silently drops the
//     per-vhost scriptHandler), so the whole config is rendered into one file.
package webserver

import (
	"bytes"
	"context"
	"strings"
	"text/template"

	"github.com/thisisnkp/nexpanel/internal/broker"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Engine identifies the web server the panel renders for. The values match the
// setup wizard's webserver ids (internal/setup), so no mapping is needed.
type Engine string

const (
	EngineOpenLiteSpeed Engine = "openlitespeed"
	EngineLiteSpeed     Engine = "litespeed_enterprise"
)

// normalizeEngine defaults an empty/unknown engine to OpenLiteSpeed — the safe
// baseline the panel has always shipped.
func normalizeEngine(e Engine) Engine {
	switch e {
	case EngineOpenLiteSpeed, EngineLiteSpeed:
		return e
	default:
		return EngineOpenLiteSpeed
	}
}

// Site is the per-site information needed to render a vhost.
type Site struct {
	VhostName     string   // internal id, e.g. "nps1"
	PrimaryDomain string   // e.g. "acme.example.com"
	Domains       []string // all domains mapped to this vhost
	DocumentRoot  string   // e.g. /srv/nexpanel/sites/1/public
	Home          string   // e.g. /srv/nexpanel/sites/1
	LogDir        string   // e.g. /srv/nexpanel/sites/1/logs
	IsPHP         bool
	FpmSocket     string // php-fpm pool socket (php only)
	PhpBin        string // php-fpm binary path (php only); OLS requires a path on
	// the fcgi extProcessor even with autoStart 0 (external pool).
	ProxyTarget string // "127.0.0.1:<port>" for a reverse-proxy (app) site; empty
	// otherwise. When set, the vhost proxies "/" to the app instead of serving files.
	ForceHTTPS bool       // redirect plain HTTP to HTTPS (opt-in; needs a cert)
	Redirects  []Redirect // per-domain redirects, evaluated before force-HTTPS
	// Suspended renders the vhost as a 503 wall: the domains stay mapped here,
	// but nothing is served and no PHP or app process is reachable.
	//
	// A suspended site keeps its vhost rather than being dropped from the config,
	// and that is not cosmetic. OpenLiteSpeed routes a request by the listener's
	// domain map and falls back to the *first* vhost for a host it does not
	// recognize. Omitting a suspended site would unmap its domains — and start
	// answering them with whichever site happens to be first, i.e. serving one
	// customer's content on another customer's domain.
	Suspended bool
	// WAFEnabled turns on the per-vhost ModSecurity + OWASP CRS web application
	// firewall. The module and rules must be present on the host; the render
	// references a pinned rules file the panel writes.
	WAFEnabled bool
}

// AliasDomains returns the site's domains other than the primary — the
// ServerAlias set for the httpd-syntax renderer, where the primary is the ServerName.
func (s Site) AliasDomains() []string {
	out := make([]string, 0, len(s.Domains))
	for _, d := range s.Domains {
		if d != s.PrimaryDomain {
			out = append(out, d)
		}
	}
	return out
}

// WAFRulesFile is the pinned path the WAF-enabled vhost references. The panel
// writes it (base ModSecurity config + the OWASP CRS + SecRuleEngine On).
const WAFRulesFile = "/etc/nexpanel/waf/main.conf"

// RenderWAFConfig renders the ModSecurity rules file the WAF vhosts load: the
// recommended base config, then the OWASP Core Rule Set, then SecRuleEngine On
// last so blocking is forced even if the base config shipped in DetectionOnly.
// IncludeOptional across the standard install locations keeps it robust to the
// exact package layout (Debian vs. the CRS tarball) without double-loading — a
// site that has no CRS installed simply gets the base engine.
func RenderWAFConfig() string {
	// libmodsecurity v3 (what OpenLiteSpeed's mod_security embeds) supports only
	// `Include`, not `IncludeOptional` — a single unsupported directive fails the
	// WHOLE rules file, so none may appear (this rules out including the distro's
	// owasp-crs.load verbatim, which itself uses IncludeOptional). The base
	// engine settings are stated inline rather than relied on from a base
	// modsecurity.conf that some packages do not ship; the CRS is then pulled in
	// by its concrete pieces (setup first, then every rule). SecDefaultAction is
	// deliberately left to crs-setup.conf, which may set it only once.
	return `# NexPanel WAF (ModSecurity + OWASP CRS; rendered, do not edit).
SecRuleEngine On
SecRequestBodyAccess On
SecResponseBodyAccess Off
SecRequestBodyLimit 13107200
SecRequestBodyNoFilesLimit 131072
SecPcreMatchLimit 100000
SecPcreMatchLimitRecursion 100000
Include /etc/modsecurity/crs/crs-setup.conf
Include /usr/share/modsecurity-crs/rules/*.conf
`
}

// Redirect sends one of the vhost's domains to another absolute URL.
type Redirect struct {
	From string // the requesting host, e.g. "old.example.com"
	To   string // an absolute URL, e.g. "https://new.example.com"
	Code int    // 301 | 302 | 307 | 308
}

// Applier applies the desired web-server state.
type Applier interface {
	Apply(ctx context.Context, sites []Site) error
}

// Service renders and applies web-server configuration via the broker.
type Service struct {
	broker broker.Gateway
	engine Engine
}

// NewService constructs the webserver Service, defaulting to OpenLiteSpeed.
func NewService(gw broker.Gateway) *Service {
	return &Service{broker: gw, engine: EngineOpenLiteSpeed}
}

// WithEngine returns the service configured for engine e (chainable).
func (s *Service) WithEngine(e Engine) *Service {
	s.engine = normalizeEngine(e)
	return s
}

// SetEngine switches the active engine in place (used when the setup wizard
// changes the stack at runtime).
func (s *Service) SetEngine(e Engine) { s.engine = normalizeEngine(e) }

// Engine returns the active engine.
func (s *Service) Engine() Engine { return normalizeEngine(s.engine) }

var _ Applier = (*Service)(nil)

// Apply renders the entire config for all sites in the active engine's format and
// asks the broker to apply, test, and reload. The config is a single included
// file, so vhosts is empty and the whole document is passed as the listener
// config; the engine tells the broker where to write it and how to reload.
func (s *Service) Apply(ctx context.Context, sites []Site) error {
	if s.broker == nil {
		return errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; the web server cannot be configured.")
	}
	engine := normalizeEngine(s.engine)
	cfg, err := RenderFor(engine, sites)
	if err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, "webserver.apply", map[string]any{
		"engine":   string(engine),
		"vhosts":   []any{},
		"listener": cfg,
	})
	return err
}

var tmplFuncs = template.FuncMap{
	"join": func(domains []string) string { return strings.Join(domains, " ") },
	// rxescape escapes a hostname for use inside a RewriteCond regex.
	"rxescape": func(host string) string { return strings.ReplaceAll(host, ".", `\.`) },
}

// RenderFor renders the full config for the given engine.
func RenderFor(engine Engine, sites []Site) (string, error) {
	switch normalizeEngine(engine) {
	case EngineLiteSpeed:
		return renderTemplate(lsweTmpl, sites)
	default:
		return RenderConfig(sites)
	}
}

func renderTemplate(t *template.Template, sites []Site) (string, error) {
	var b bytes.Buffer
	if err := t.Execute(&b, sites); err != nil {
		return "", errx.Wrap(err, errx.KindInternal, "config_render_failed", "Could not render the web server config.")
	}
	return b.String(), nil
}

// ── OpenLiteSpeed ────────────────────────────────────────────────────────────

// olsJoin keeps the OLS listener's comma-separated domain map (OLS wants commas;
// the httpd-syntax renderer wants spaces).
var olsFuncs = template.FuncMap{
	"join":     func(domains []string) string { return strings.Join(domains, ", ") },
	"rxescape": func(host string) string { return strings.ReplaceAll(host, ".", `\.`) },
}

// configTmpl renders the complete OpenLiteSpeed config: per-site FastCGI
// handlers (server-level), inline virtual hosts, and the listener with its
// domain map.
var configTmpl = template.Must(template.New("olsconfig").Funcs(olsFuncs).Parse(
	`{{range .}}{{if and .IsPHP (not .Suspended)}}extProcessor nps_{{.VhostName}} {
  type                    fcgi
  address                 uds://{{.FpmSocket}}
  maxConns                10
  path                    {{.PhpBin}}
  autoStart               0
}
{{end}}{{if and .ProxyTarget (not .Suspended)}}extProcessor proxy_{{.VhostName}} {
  type                    proxy
  address                 {{.ProxyTarget}}
  maxConns                100
  respBuffer              0
}
{{end}}{{end}}{{range .}}virtualhost {{.VhostName}} {
  vhRoot                  {{.Home}}
  docRoot                 {{.DocumentRoot}}
  allowSymbolLink         1
  enableScript            1
  restrained              1
  enableGzip              1
  index  {
    useServer             0
    indexFiles            index.html, index.htm{{if .IsPHP}}, index.php{{end}}
    autoIndex             0
  }
  errorlog {{.LogDir}}/error.log {
    useServer             0
    logLevel              WARN
  }
  accesslog {{.LogDir}}/access.log {
    useServer             0
  }
{{if .Suspended}}  rewrite  {
    enable                1
    autoLoadHtaccess      0
    rules                 <<<END_rules
RewriteRule ^(.*)$ - [R=503,L]
END_rules
  }
{{else}}{{if or .ForceHTTPS .Redirects}}  rewrite  {
    enable                1
    autoLoadHtaccess      0
    rules                 <<<END_rules
{{range .Redirects}}RewriteCond %{HTTP_HOST} ^{{rxescape .From}}$ [NC]
RewriteRule ^(.*)$ {{.To}}$1 [R={{.Code}},L]
{{end}}{{if .ForceHTTPS}}RewriteCond %{HTTPS} !on
RewriteRule ^(.*)$ https://%{HTTP_HOST}$1 [R=301,L]
{{end}}END_rules
  }
{{end}}{{if and .WAFEnabled (not .Suspended)}}  module mod_security {
    modsecurity           on
    modsecurity_rules_file /etc/nexpanel/waf/main.conf
  }
{{end}}{{if .IsPHP}}  scriptHandler  {
    add                   fcgi:nps_{{.VhostName}} php
  }
{{end}}{{if .ProxyTarget}}  context / {
    type                  proxy
    handler               proxy_{{.VhostName}}
    addDefaultCharset     off
  }
{{end}}{{end}}}
{{end}}listener NexPanelHTTP {
  address                 *:80
  secure                  0
{{range .}}  map                     {{.VhostName}} {{join .Domains}}
{{end}}}
`))

// RenderConfig renders the entire OpenLiteSpeed config for the given sites.
func RenderConfig(sites []Site) (string, error) {
	var b bytes.Buffer
	// OpenLiteSpeed only activates the ModSecurity module when it is declared at
	// server level; the per-vhost `module mod_security` block alone is inert. So
	// when any site has the WAF on, emit the server-level enable at the head —
	// per-vhost `modsecurity on/off` then decides which sites it applies to.
	if anyWAF(sites) {
		b.WriteString("module mod_security {\n  ls_enabled              1\n}\n")
	}
	if err := configTmpl.Execute(&b, sites); err != nil {
		return "", errx.Wrap(err, errx.KindInternal, "config_render_failed", "Could not render the web server config.")
	}
	return b.String(), nil
}

// anyWAF reports whether any non-suspended site has the WAF enabled.
func anyWAF(sites []Site) bool {
	for _, s := range sites {
		if s.WAFEnabled && !s.Suspended {
			return true
		}
	}
	return false
}

// ── LiteSpeed Enterprise ─────────────────────────────────────────────────────

// lsweTmpl renders one <VirtualHost *:80> per site in httpd syntax, which is what
// LiteSpeed Enterprise parses — it is a drop-in Apache replacement, so its config
// is Apache's config. The primary domain is the ServerName and the rest are
// ServerAlias. PHP is handed to php-fpm via mod_proxy_fcgi; an app site
// reverse-proxies to its upstream; a suspended site answers 503. AllowOverride
// All keeps .htaccess working as operators expect.
var lsweTmpl = template.Must(template.New("lswe").Funcs(tmplFuncs).Parse(
	`# NexPanel LiteSpeed Enterprise configuration (rendered, do not edit).
{{range .}}<VirtualHost *:80>
    ServerName {{.PrimaryDomain}}
{{range .AliasDomains}}    ServerAlias {{.}}
{{end}}    ErrorLog {{.LogDir}}/error.log
    CustomLog {{.LogDir}}/access.log combined
{{if .Suspended}}    RedirectMatch 503 ^/.*$
{{else}}    DocumentRoot {{.DocumentRoot}}
    <Directory {{.DocumentRoot}}>
        Options -Indexes +FollowSymLinks
        AllowOverride All
        Require all granted
        DirectoryIndex {{if .IsPHP}}index.php {{end}}index.html index.htm
    </Directory>
{{if or .Redirects .ForceHTTPS}}    RewriteEngine On
{{range .Redirects}}    RewriteCond %{HTTP_HOST} ^{{rxescape .From}}$ [NC]
    RewriteRule ^(.*)$ {{.To}}$1 [R={{.Code}},L]
{{end}}{{if .ForceHTTPS}}    RewriteCond %{HTTPS} !=on
    RewriteRule ^(.*)$ https://%{HTTP_HOST}$1 [R=301,L]
{{end}}{{end}}{{if .WAFEnabled}}    <IfModule security2_module>
        SecRuleEngine On
        Include /etc/nexpanel/waf/main.conf
    </IfModule>
{{end}}{{if .ProxyTarget}}    ProxyPreserveHost On
    ProxyPass / http://{{.ProxyTarget}}/
    ProxyPassReverse / http://{{.ProxyTarget}}/
{{else}}{{if .IsPHP}}    <FilesMatch \.php$>
        SetHandler "proxy:unix:{{.FpmSocket}}|fcgi://localhost"
    </FilesMatch>
{{end}}{{end}}{{end}}</VirtualHost>
{{end}}`))
