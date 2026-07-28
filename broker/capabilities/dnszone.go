package capabilities

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// BIND paths. The include file is wired into named.conf once at install time:
//
//	include "/etc/bind/named.conf.heropanel";
const (
	bindZonesDir    = "/etc/bind/zones"
	bindKeysDir     = "/etc/bind/keys"
	bindNamedConf   = "/etc/bind/named.conf.heropanel"
	namedCheckzone  = "/usr/bin/named-checkzone"
	rndcPath        = "/usr/sbin/rndc"
	dnssecDsFromKey = "/usr/bin/dnssec-dsfromkey"
	bindUser        = "bind"
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
	// When any zone in this include is DNSSEC-signed, BIND needs a key-directory
	// it can write to. Ensure it exists and belongs to bind — only when signing is
	// actually configured, so ordinary zones do not trigger a chown on every edit.
	if strings.Contains(in.NamedConf, "key-directory") {
		if err := c.FS.MkdirAll(bindKeysDir, 0o750); err != nil {
			return capability.Result{}, errx.Upstream(err, "keydir_failed", "Could not create the DNSSEC key directory.")
		}
		_, _ = c.Runner.Run(c.Ctx, exec.Command{
			Path: chownPath, Args: []string{"-R", bindUser + ":" + bindUser, bindKeysDir}, Timeout: 10 * time.Second,
		})
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

// ── dns.dnssec_status ─────────────────────────────────────────────────────────

// DNSSECStatus reports a signed zone's DNSKEY and the DS records the operator
// must hand the registrar. With dnssec-policy the keys live in the key-directory
// as K<zone>.+alg+id.key files; the KSK/CSK ones (flags 257) are what a DS is
// derived from, via dnssec-dsfromkey. A zone with no key files yet (named has
// not finished generating them) reports signed=false rather than erroring — the
// caller polls.
type DNSSECStatus struct{}

func (DNSSECStatus) Name() string { return "dns.dnssec_status" }

type dnssecStatusInput struct {
	Zone string `json:"zone"`
}

func (DNSSECStatus) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in dnssecStatusInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for dns.dnssec_status.")
	}
	if err := capability.ValidateFQDN(in.Zone); err != nil {
		return capability.Result{}, err
	}

	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: lsPath, Args: []string{"-1", "--", bindKeysDir}, Timeout: 10 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return capability.Result{Data: map[string]any{"signed": false, "ds": []string{}, "dnskey": []string{}}}, nil
	}

	prefix := "K" + in.Zone + "."
	var ds, dnskey []string
	for _, name := range strings.Split(string(res.Stdout), "\n") {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".key") {
			continue
		}
		path := bindKeysDir + "/" + name
		body, err := c.FS.ReadFile(path)
		if err != nil {
			continue
		}
		// A key-signing key carries flags 257 in its DNSKEY rdata; a zone-signing
		// key is 256. Only KSK/CSK material yields the DS a parent delegates to.
		if !dnskeyIsKSK(string(body)) {
			continue
		}
		dnskey = append(dnskey, dnskeyRdata(string(body)))
		out, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: dnssecDsFromKey, Args: []string{"-2", path}, Timeout: 15 * time.Second,
		})
		if err == nil && out.ExitCode == 0 {
			for _, line := range strings.Split(string(out.Stdout), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					ds = append(ds, line)
				}
			}
		}
	}
	return capability.Result{Data: map[string]any{
		"signed": len(ds) > 0,
		"ds":     ds,
		"dnskey": dnskey,
	}}, nil
}

// dnskeyIsKSK reports whether a .key file's DNSKEY record has the SEP flag (257).
func dnskeyIsKSK(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if i := strings.Index(line, "DNSKEY"); i >= 0 {
			fields := strings.Fields(line[i+len("DNSKEY"):])
			if len(fields) >= 1 && fields[0] == "257" {
				return true
			}
		}
	}
	return false
}

// dnskeyRdata returns the DNSKEY record line (without the leading comments).
func dnskeyRdata(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, ";") {
			return line
		}
	}
	return ""
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
