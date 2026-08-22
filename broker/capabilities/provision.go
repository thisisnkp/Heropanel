package capabilities

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// SystemProvision installs and enables the hosting-stack components chosen in the
// first-run setup wizard. It is the privileged half of that flow: npd records the
// operator's choices and asks the broker to realize them on the host.
//
// It is deliberately not a generic package installer. The input is a list of
// *logical components* from a fixed allowlist (the contract with npd's
// internal/setup Selection.Components()); the broker is the only place that knows
// this host's distro family and the matching package names and service units, so
// npd stays distro-agnostic. Anything outside the allowlist is refused.

const (
	aptGetPath = "/usr/bin/apt-get"
	dnfPath    = "/usr/bin/dnf"
)

// provisionComponent maps one logical component to the packages to install on
// each distro family and the systemd unit to enable. Package names and service
// units differ between Debian and RHEL (dovecot-core/dovecot, bind9/bind), so
// both are recorded.
type provisionComponent struct {
	debianPkgs []string
	rhelPkgs   []string
	debianSvc  string
	rhelSvc    string
}

// provisionComponents is the allowlist. Keys match the component ids npd sends
// (webserver + db engine enum values, plus bind/postfix/dovecot).
var provisionComponents = map[string]provisionComponent{
	"openlitespeed":        {debianPkgs: []string{"openlitespeed"}, rhelPkgs: []string{"openlitespeed"}, debianSvc: "lshttpd", rhelSvc: "lshttpd"},
	"litespeed_enterprise": {debianPkgs: []string{"lsws"}, rhelPkgs: []string{"lsws"}, debianSvc: "lsws", rhelSvc: "lsws"},
	"mariadb":              {debianPkgs: []string{"mariadb-server"}, rhelPkgs: []string{"mariadb-server"}, debianSvc: "mariadb", rhelSvc: "mariadb"},
	"bind":                 {debianPkgs: []string{"bind9"}, rhelPkgs: []string{"bind"}, debianSvc: "named", rhelSvc: "named"},

	// The always-on baseline (internal/setup.baselineComponents). Only the ones
	// that run continuously get a service: clamav is scanned with clamscan on
	// demand, so the unit that matters is the signature updater, not a resident
	// daemon; modsecurity is a webserver module with no unit of its own.
	"phpmyadmin":  {debianPkgs: []string{"phpmyadmin"}, rhelPkgs: []string{"phpMyAdmin"}},
	"clamav":      {debianPkgs: []string{"clamav", "clamav-freshclam"}, rhelPkgs: []string{"clamav", "clamav-update"}, debianSvc: "clamav-freshclam"},
	"fail2ban":    {debianPkgs: []string{"fail2ban"}, rhelPkgs: []string{"fail2ban"}, debianSvc: "fail2ban", rhelSvc: "fail2ban"},
	"modsecurity": {debianPkgs: []string{"modsecurity-crs"}, rhelPkgs: []string{"mod_security_crs"}},
	"nftables":    {debianPkgs: []string{"nftables"}, rhelPkgs: []string{"nftables"}, debianSvc: "nftables", rhelSvc: "nftables"},
	"postfix":     {debianPkgs: []string{"postfix"}, rhelPkgs: []string{"postfix"}, debianSvc: "postfix", rhelSvc: "postfix"},
	"dovecot":     {debianPkgs: []string{"dovecot-core"}, rhelPkgs: []string{"dovecot"}, debianSvc: "dovecot", rhelSvc: "dovecot"},
}

// SystemProvision is the setup-wizard provisioning capability.
type SystemProvision struct{}

type systemProvisionInput struct {
	Components []string `json:"components"`
	// LicenseKey is the LiteSpeed Enterprise serial. When the litespeed_enterprise
	// component is provisioned and this is set, it is written to LSWS's serial
	// file to activate the license; empty means a trial.
	LicenseKey string `json:"license_key"`
}

// lswsSerialPath is where LiteSpeed Enterprise reads its license serial.
const lswsSerialPath = "/usr/local/lsws/conf/serial.no"

// Name implements capability.Capability.
func (SystemProvision) Name() string { return "system.provision" }

// Execute installs the packages for each requested component and enables its
// service. Components are validated up front, so an unknown one fails before any
// package is touched. A package-install failure aborts (the caller can retry —
// apt/dnf installs are idempotent), while a service that will not enable is
// reported per-component rather than failing the whole run: a freshly installed
// component that needs post-setup of its own is enabled by its own module, not
// here.
func (SystemProvision) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in systemProvisionInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for system.provision.")
	}
	if len(in.Components) == 0 {
		return capability.Result{}, errx.Validation("no_components", "No components to provision.")
	}
	for _, name := range in.Components {
		if _, ok := provisionComponents[name]; !ok {
			return capability.Result{}, errx.Validation("unknown_component", "Unknown component: "+name)
		}
	}

	debian := updatesIsDebian(c)
	// Refresh package indexes once on Debian so a fresh box can resolve the
	// packages. Best-effort: a mirror hiccup must not abort the whole provision.
	if debian {
		_, _ = c.Runner.Run(c.Ctx, exec.Command{
			Path: aptGetPath, Args: []string{"update"}, Env: aptEnv(), Timeout: 5 * time.Minute,
		})
	}

	status := map[string]any{}
	for _, name := range in.Components {
		comp := provisionComponents[name]
		pkgs, svc := comp.rhelPkgs, comp.rhelSvc
		if debian {
			pkgs, svc = comp.debianPkgs, comp.debianSvc
		}
		// LiteSpeed Enterprise is licensed: write the serial before installing so
		// the installer/activation picks it up (empty ⇒ trial, which LSWS accepts).
		if name == "litespeed_enterprise" && in.LicenseKey != "" {
			if err := c.FS.WriteFile(lswsSerialPath, []byte(in.LicenseKey+"\n"), 0o600); err != nil {
				return capability.Result{}, errx.Upstream(err, "license_write_failed",
					"Could not write the LiteSpeed license serial.")
			}
		}
		if err := installPackages(c, debian, pkgs); err != nil {
			return capability.Result{}, errx.Upstream(err, "install_failed",
				"Failed to install packages for "+name+".")
		}
		enabled := false
		if svc != "" {
			r, err := c.Runner.Run(c.Ctx, exec.Command{
				Path: systemctlPath, Args: []string{"enable", "--now", svc}, Timeout: 60 * time.Second,
			})
			enabled = err == nil && r.ExitCode == 0
		}
		status[name] = map[string]any{"installed": true, "service": svc, "enabled": enabled}
	}

	family := "rhel"
	if debian {
		family = "debian"
	}
	return capability.Result{Data: map[string]any{"provisioned": status, "distro": family}}, nil
}

// installPackages installs pkgs non-interactively via the host's package manager.
func installPackages(c capability.Context, debian bool, pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	var cmd exec.Command
	if debian {
		cmd = exec.Command{
			Path: aptGetPath, Args: append([]string{"install", "-y"}, pkgs...),
			Env: aptEnv(), Timeout: 10 * time.Minute,
		}
	} else {
		cmd = exec.Command{
			Path: dnfPath, Args: append([]string{"install", "-y"}, pkgs...),
			Timeout: 10 * time.Minute,
		}
	}
	res, err := c.Runner.Run(c.Ctx, cmd)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("package install returned exit code %d", res.ExitCode)
	}
	return nil
}

// aptEnv is the environment apt needs for an unattended install: no prompts, and
// a PATH so dpkg's maintainer scripts find their tools (Env is empty by default).
func aptEnv() []string {
	return []string{"DEBIAN_FRONTEND=noninteractive", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
}
