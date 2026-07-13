package capabilities

import (
	"encoding/json"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// sslRoot is where per-domain certificate material is stored.
const sslRoot = "/etc/heropanel/ssl"

// CertInstall writes a domain's certificate chain and private key. The domain
// is validated as an FQDN, so the derived path cannot escape sslRoot.
type CertInstall struct{}

func (CertInstall) Name() string { return "cert.install" }

func (CertInstall) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Domain  string `json:"domain"`
		CertPEM string `json:"cert_pem"`
		KeyPEM  string `json:"key_pem"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for cert.install.")
	}
	if err := capability.ValidateFQDN(in.Domain); err != nil {
		return capability.Result{}, err
	}
	if in.CertPEM == "" || in.KeyPEM == "" {
		return capability.Result{}, errx.Validation("missing_pem", "Both cert_pem and key_pem are required.")
	}

	dir := sslRoot + "/" + in.Domain
	if err := c.FS.MkdirAll(dir, 0o750); err != nil {
		return capability.Result{}, errx.Upstream(err, "ssl_mkdir_failed", "Could not create the certificate directory.")
	}
	if err := c.FS.WriteFile(dir+"/fullchain.pem", []byte(in.CertPEM), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "cert_write_failed", "Could not write the certificate.")
	}
	if err := c.FS.WriteFile(dir+"/privkey.pem", []byte(in.KeyPEM), 0o600); err != nil {
		return capability.Result{}, errx.Upstream(err, "key_write_failed", "Could not write the private key.")
	}
	return capability.Result{Data: map[string]any{
		"domain":    in.Domain,
		"cert_path": dir + "/fullchain.pem",
		"key_path":  dir + "/privkey.pem",
	}}, nil
}

// CertWriteChallenge writes an ACME HTTP-01 challenge file into a site's webroot.
// The webroot must be confined to an allowed policy root.
type CertWriteChallenge struct{}

func (CertWriteChallenge) Name() string { return "cert.write_challenge" }

func (CertWriteChallenge) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Webroot string `json:"webroot"`
		Token   string `json:"token"`
		KeyAuth string `json:"key_auth"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for cert.write_challenge.")
	}
	if err := capability.ValidatePath(in.Webroot, c.Policy); err != nil {
		return capability.Result{}, err
	}
	// The token is part of a filesystem path; restrict it to the ACME charset.
	if !validACMEToken(in.Token) {
		return capability.Result{}, errx.Validation("invalid_token", "Invalid ACME challenge token.")
	}

	dir := in.Webroot + "/.well-known/acme-challenge"
	if err := c.FS.MkdirAll(dir, 0o755); err != nil {
		return capability.Result{}, errx.Upstream(err, "challenge_mkdir_failed", "Could not create the challenge directory.")
	}
	if err := c.FS.WriteFile(dir+"/"+in.Token, []byte(in.KeyAuth), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "challenge_write_failed", "Could not write the challenge file.")
	}
	return capability.Result{Data: map[string]any{"path": dir + "/" + in.Token}}, nil
}

// validACMEToken restricts the token to the base64url charset used by ACME.
func validACMEToken(t string) bool {
	if t == "" || len(t) > 128 {
		return false
	}
	for _, r := range t {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
