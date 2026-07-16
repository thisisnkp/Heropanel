package capabilities

import (
	"encoding/json"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// BIND paths. The include file is wired into named.conf once at install time:
//
//	include "/etc/bind/named.conf.heropanel";
const (
	bindZonesDir   = "/etc/bind/zones"
	bindNamedConf  = "/etc/bind/named.conf.heropanel"
	namedCheckzone = "/usr/bin/named-checkzone"
	rndcPath       = "/usr/sbin/rndc"
)

func bindZoneFile(zone string) string { return bindZonesDir + "/db." + zone }

// ── dns.write_zone ───────────────────────────────────────────────────────────

// DNSWriteZone writes a zone file and the declarative named.conf include,
// validates the zone with named-checkzone (rolling back on failure so a broken
// zone is never served), and reloads BIND.
type DNSWriteZone struct{}

func (DNSWriteZone) Name() string { return "dns.write_zone" }

type dnsWriteZoneInput struct {
	Zone      string `json:"zone"`
	ZoneFile  string `json:"zone_file"`
	NamedConf string `json:"named_conf"`
}

func (DNSWriteZone) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in dnsWriteZoneInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for dns.write_zone.")
	}
	if err := capability.ValidateFQDN(in.Zone); err != nil {
		return capability.Result{}, err
	}
	zonePath := bindZoneFile(in.Zone)

	// Capture prior state for rollback.
	priorZone, hadZone := readIfExists(c, zonePath)
	priorConf, hadConf := readIfExists(c, bindNamedConf)

	if err := c.FS.MkdirAll(bindZonesDir, 0o755); err != nil {
		return capability.Result{}, errx.Upstream(err, "zonedir_failed", "Could not create the zones directory.")
	}
	if err := c.FS.WriteFile(zonePath, []byte(in.ZoneFile), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "zone_write_failed", "Could not write the zone file.")
	}
	if err := c.FS.WriteFile(bindNamedConf, []byte(in.NamedConf), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "namedconf_write_failed", "Could not write the BIND include.")
	}

	// named-checkzone is the final authority — a bad zone is rolled back.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: namedCheckzone, Args: []string{in.Zone, zonePath}, Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		restore(c, zonePath, priorZone, hadZone)
		restore(c, bindNamedConf, priorConf, hadConf)
		return capability.Result{}, errx.New(errx.KindValidation, "zone_invalid",
			"The zone did not pass named-checkzone: "+string(res.Stdout)+string(res.Stderr))
	}

	if err := rndcReload(c); err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Data: map[string]any{"zone": in.Zone, "applied": true}}, nil
}

// ── dns.remove_zone ──────────────────────────────────────────────────────────

// DNSRemoveZone drops a zone from the declarative include, deletes its zone
// file, and reloads BIND.
type DNSRemoveZone struct{}

func (DNSRemoveZone) Name() string { return "dns.remove_zone" }

type dnsRemoveZoneInput struct {
	Zone      string `json:"zone"`
	NamedConf string `json:"named_conf"`
}

func (DNSRemoveZone) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in dnsRemoveZoneInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for dns.remove_zone.")
	}
	if err := capability.ValidateFQDN(in.Zone); err != nil {
		return capability.Result{}, err
	}
	if err := c.FS.WriteFile(bindNamedConf, []byte(in.NamedConf), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "namedconf_write_failed", "Could not update the BIND include.")
	}
	_ = c.FS.Remove(bindZoneFile(in.Zone))
	if err := rndcReload(c); err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Data: map[string]any{"zone": in.Zone, "removed": true}}, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func rndcReload(c capability.Context) error {
	res, err := c.Runner.Run(c.Ctx, exec.Command{Path: rndcPath, Args: []string{"reload"}, Timeout: 20 * time.Second})
	if err != nil {
		return errx.Upstream(err, "rndc_failed", "Could not reload BIND.")
	}
	if res.ExitCode != 0 {
		return errx.New(errx.KindUpstream, "rndc_failed", "rndc reload returned non-zero: "+string(res.Stderr))
	}
	return nil
}

func readIfExists(c capability.Context, path string) ([]byte, bool) {
	if b, err := c.FS.ReadFile(path); err == nil {
		return b, true
	}
	return nil, false
}

func restore(c capability.Context, path string, prior []byte, had bool) {
	if had {
		_ = c.FS.WriteFile(path, prior, 0o644)
	} else {
		_ = c.FS.Remove(path)
	}
}
