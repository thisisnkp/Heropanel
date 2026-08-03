package setup

import "context"

// ProvisionCapability is the broker capability the provisioner calls to install
// and enable the chosen hosting-stack components on the host.
const ProvisionCapability = "system.provision"

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
	gw Invoker
}

// NewBrokerProvisioner wires a provisioner over the broker gateway.
func NewBrokerProvisioner(gw Invoker) *BrokerProvisioner {
	return &BrokerProvisioner{gw: gw}
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
	_, err := p.gw.Invoke(ctx, ProvisionCapability, input)
	return err
}
