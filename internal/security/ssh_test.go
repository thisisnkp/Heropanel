package security

import (
	"strings"
	"testing"
)

// The rendered drop-in reflects the validated options and always carries the
// fixed hardening block (empty passwords off, X11 off, ...).
func TestRenderSSHDConfig(t *testing.T) {
	cfg := RenderSSHDConfig(SSHOptions{
		Port: 2222, PermitRootLogin: "no",
		PasswordAuthentication: false, PubkeyAuthentication: true, MaxAuthTries: 3,
		AllowUsers: []string{"deploy", "admin"},
	})
	for _, want := range []string{
		"Port 2222",
		"PermitRootLogin no",
		"PasswordAuthentication no",
		"PubkeyAuthentication yes",
		"MaxAuthTries 3",
		"PermitEmptyPasswords no",
		"X11Forwarding no",
		"AllowUsers admin deploy", // sorted
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q\n%s", want, cfg)
		}
	}
}

// Validation fills defaults and rejects the dangerous combinations.
func TestSSHOptionsValidate(t *testing.T) {
	// The hardened defaults validate and fill numeric/string blanks.
	o := DefaultSSHOptions()
	o.Port, o.PermitRootLogin, o.MaxAuthTries = 0, "", 0 // blanks refill on validate
	if err := o.validate(); err != nil {
		t.Fatalf("default validate: %v", err)
	}
	if o.Port != 22 || o.PermitRootLogin != "prohibit-password" || o.MaxAuthTries != 4 {
		t.Errorf("defaults not applied: %+v", o)
	}
	// The zero value (pubkey off, password off) is a self-lockout and is refused.
	if err := (&SSHOptions{}).validate(); err == nil {
		t.Error("the zero value (no auth method) was accepted")
	}

	// Disabling BOTH auth methods is a self-lockout and must be refused.
	if err := (&SSHOptions{PubkeyAuthentication: false, PasswordAuthentication: false}).validate(); err == nil {
		t.Error("disabling both auth methods was accepted")
	}

	for _, bad := range []SSHOptions{
		{Port: 70000, PubkeyAuthentication: true},
		{PermitRootLogin: "maybe", PubkeyAuthentication: true},
		{MaxAuthTries: 99, PubkeyAuthentication: true},
		{AllowUsers: []string{"bad user!"}, PubkeyAuthentication: true},
	} {
		b := bad
		if err := b.validate(); err == nil {
			t.Errorf("invalid options accepted: %+v", bad)
		}
	}
}

// The effective-config parser lifts the managed keys out of `sshd -T` output.
func TestParseSSHDEffective(t *testing.T) {
	raw := "port 2222\npermitrootlogin no\npasswordauthentication no\npubkeyauthentication yes\nmaxauthtries 3\nx11forwarding no\nunrelatedkey value\n"
	m := ParseSSHDEffective(raw)
	if m["port"] != "2222" || m["permitrootlogin"] != "no" || m["passwordauthentication"] != "no" {
		t.Errorf("parsed = %+v", m)
	}
	if _, ok := m["unrelatedkey"]; ok {
		t.Error("parser kept an unmanaged key")
	}
}
