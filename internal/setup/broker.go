package setup

import (
	"context"
	"log/slog"
	"strings"
)

// ProvisionCapability is the broker capability the provisioner calls to install
// and enable the chosen hosting-stack components on the host.
const ProvisionCapability = "system.provision"

// MaldetInstallCapability installs Linux Malware Detect. It is separate from
// ProvisionCapability because maldet has no distribution package: the broker
// resolves every other baseline component to apt or dnf, and cannot resolve
// this one.
const MaldetInstallCapability = "maldet.install"

// PHPMyAdminCapability writes the phpMyAdmin sign-on script and config drop-in.
// It runs after the package is installed, because it writes into the tree that
// package creates.
const PHPMyAdminCapability = "phpmyadmin.provision"

// Invoker is the narrow slice of the privileged broker gateway the provisioner
// needs — a single capability call. internal/broker.Gateway satisfies it, so the
// setup package need not import the broker.
type Invoker interface {
	Invoke(ctx context.Context, capability string, input any) (map[string]any, error)
}

// BrokerProvisioner is the real, host-mutating Provisioner: it asks the broker to
// provision each logical component of the selection (install packages + enable
// the service). A nil Provisioner (record-only) is the alternative — the panel
// then persists the choices without touching the host.
type BrokerProvisioner struct {
	gw  Invoker
	log *slog.Logger
	// redeemURL is where phpMyAdmin's sign-on script exchanges a ticket for
	// database credentials: npd's own loopback address. Empty skips the
	// phpMyAdmin wiring — a panel that cannot say where it listens must not
	// write a script pointing at a guess.
	redeemURL string
}

// NewBrokerProvisioner wires a provisioner over the broker gateway.
func NewBrokerProvisioner(gw Invoker) *BrokerProvisioner {
	return &BrokerProvisioner{gw: gw, log: slog.Default()}
}

// WithLogger sets the logger used to report a baseline component that could not
// be installed (chainable).
func (p *BrokerProvisioner) WithLogger(l *slog.Logger) *BrokerProvisioner {
	if l != nil {
		p.log = l
	}
	return p
}

// WithRedeemURL sets npd's loopback ticket-redemption URL, which is baked into
// the phpMyAdmin sign-on script (chainable).
func (p *BrokerProvisioner) WithRedeemURL(u string) *BrokerProvisioner {
	p.redeemURL = strings.TrimSpace(u)
	return p
}

// Provision realizes the plan on the host in one privileged call: the broker
// resolves each component to distro-specific packages and a service, installs and
// enables them. Module-only steps (DNS/mail intent) carry no host action here —
// the packages they need are already components — so a plan with just those is a
// no-op.
func (p *BrokerProvisioner) Provision(ctx context.Context, plan Plan) error {
	comps := plan.Selection.Components()
	if len(comps) == 0 {
		return nil
	}
	input := map[string]any{"components": comps}
	// A LiteSpeed Enterprise serial is passed through so the broker can activate
	// the license during provisioning; empty means a trial.
	if plan.Selection.LicenseKey != "" {
		input["license_key"] = plan.Selection.LicenseKey
	}
	if _, err := p.gw.Invoke(ctx, ProvisionCapability, input); err != nil {
		return err
	}

	// phpMyAdmin's sign-on wiring, once its package is on disk.
	//
	// Like maldet below, a failure here is logged rather than fatal: the panel
	// works without it — the hand-off button reports that phpMyAdmin is not
	// wired — and holding a first-run install hostage to a config drop-in would
	// be a worse trade than an operator seeing one feature unavailable.
	if p.redeemURL != "" {
		if _, err := p.gw.Invoke(ctx, PHPMyAdminCapability, map[string]any{
			"redeem_url": p.redeemURL,
		}); err != nil {
			p.log.Warn("setup: phpMyAdmin sign-on could not be configured", "err", err)
		}
	}

	// maldet last, and its failure does not fail setup.
	//
	// That is a deliberate asymmetry with the packages above. Those come from
	// the host's own configured repositories; maldet comes from a third party
	// over the public internet, and rfxn.com being briefly unreachable must not
	// hold a whole first-run install hostage. The failure is not swallowed: it
	// is logged, and the malware screen shows maldet as not installed with a
	// one-click Install — so the gap is visible and fixable rather than silent.
	if hasMaldetStep(plan) {
		if _, err := p.gw.Invoke(ctx, MaldetInstallCapability, map[string]any{
			"path": DefaultMaldetPath,
		}); err != nil {
			p.log.Warn("setup: maldet could not be installed; install it from the malware screen",
				"err", err)
		}
	}
	return nil
}

// DefaultMaldetPath is the tarball maldet is fetched from, relative to the host
// the broker pins. It matches internal/security's default; setup provisioning
// and the malware screen's Install button install the same thing.
const DefaultMaldetPath = "/downloads/maldetect-current.tar.gz"

func hasMaldetStep(plan Plan) bool {
	for _, s := range plan.Steps {
		if s.Kind == StepMaldet && s.Enable {
			return true
		}
	}
	return false
}
