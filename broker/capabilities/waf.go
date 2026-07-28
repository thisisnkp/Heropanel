package capabilities

import (
	"encoding/json"
	"strings"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// WAF: write the pinned ModSecurity rules file the OpenLiteSpeed vhosts
// reference when a site's web application firewall is on. hpd renders the
// content (base config + the OWASP CRS includes + SecRuleEngine On); the broker
// writes only the one pinned path. The WAF module and the CRS rule files
// themselves are installed on the host out of band (package/installer) — this
// capability composes them into the file the vhost loads.

const (
	wafDir       = "/etc/heropanel/waf"
	wafMainConf  = wafDir + "/main.conf"
	wafMaxConfig = 64 << 10
)

// WAFProvision writes the WAF rules file.
type WAFProvision struct{}

type wafProvisionInput struct {
	Config string `json:"config"`
}

// Name implements capability.Capability.
func (WAFProvision) Name() string { return "waf.provision" }

// Execute implements capability.Capability.
func (WAFProvision) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in wafProvisionInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for waf.provision.")
	}
	if len(in.Config) == 0 || len(in.Config) > wafMaxConfig || strings.ContainsRune(in.Config, 0) {
		return capability.Result{}, errx.Validation("bad_config", "The WAF config is missing or invalid.")
	}
	if err := c.FS.MkdirAll(wafDir, 0o755); err != nil {
		return capability.Result{}, errx.Upstream(err, "waf_mkdir_failed", "Could not create the WAF directory.")
	}
	if err := c.FS.WriteFile(wafMainConf, []byte(in.Config), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "waf_write_failed", "Could not write the WAF rules file.")
	}
	return capability.Result{Data: map[string]any{"provisioned": true, "path": wafMainConf}}, nil
}
