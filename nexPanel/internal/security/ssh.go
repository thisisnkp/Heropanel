package security

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/thisisnkp/nexpanel/internal/broker"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// SSH hardening: a rendered sshd drop-in the panel owns, applied through the
// broker with an `sshd -t` config test and a reload (not a restart, so live
// sessions survive) — the render-all/validate/apply discipline the firewall and
// webserver use. npd renders the complete drop-in from validated fields; the
// broker writes the one pinned path, tests it, reloads, and rolls back on
// failure. Nothing an attacker could smuggle in becomes a directive: the field
// set is fixed and every value is validated before it is rendered.

// The sshd drop-in path the broker writes. Modern OpenSSH's sshd_config ends
// with `Include /etc/ssh/sshd_config.d/*.conf`, so a 50- drop-in is authoritative
// for the keys it sets.
const sshDropinPath = "/etc/ssh/sshd_config.d/50-nexpanel.conf"

var reSSHUser = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// SSHOptions is the validated hardening surface. Zero value renders safe
// defaults (key-only auth, root login by key only, port 22).
type SSHOptions struct {
	Port                   int      `json:"port"`
	PermitRootLogin        string   `json:"permit_root_login"` // no | prohibit-password | yes
	PasswordAuthentication bool     `json:"password_authentication"`
	PubkeyAuthentication   bool     `json:"pubkey_authentication"`
	MaxAuthTries           int      `json:"max_auth_tries"`
	AllowUsers             []string `json:"allow_users,omitempty"`
}

// DefaultSSHOptions is the hardened baseline: key-only, root by key only, the
// standard port, a tight auth-try budget.
func DefaultSSHOptions() SSHOptions {
	return SSHOptions{
		Port: 22, PermitRootLogin: "prohibit-password",
		PasswordAuthentication: false, PubkeyAuthentication: true, MaxAuthTries: 4,
	}
}

// SSH manages the host's sshd hardening.
type SSH struct {
	broker broker.Gateway
}

// NewSSH constructs the service.
func NewSSH(gw broker.Gateway) *SSH { return &SSH{broker: gw} }

// Available reports whether SSH hardening can be applied.
func (s *SSH) Available() bool { return s != nil && s.broker != nil }

func (s *SSH) requireAvailable() error {
	if s.Available() {
		return nil
	}
	return errx.New(errx.KindUnavailable, "ssh_unavailable", "SSH hardening needs the broker.")
}

// validate checks every field before it can be rendered.
func (o *SSHOptions) validate() error {
	if o.Port == 0 {
		o.Port = 22
	}
	if o.Port < 1 || o.Port > 65535 {
		return errx.Validation("invalid_port", "The SSH port must be between 1 and 65535.")
	}
	if o.PermitRootLogin == "" {
		o.PermitRootLogin = "prohibit-password"
	}
	switch o.PermitRootLogin {
	case "no", "prohibit-password", "yes":
	default:
		return errx.Validation("invalid_permit_root", "PermitRootLogin must be no, prohibit-password or yes.")
	}
	if o.MaxAuthTries == 0 {
		o.MaxAuthTries = 4
	}
	if o.MaxAuthTries < 1 || o.MaxAuthTries > 10 {
		return errx.Validation("invalid_max_auth", "MaxAuthTries must be between 1 and 10.")
	}
	// Key-only auth with no way to authenticate at all is a self-lockout: refuse
	// disabling both password and public-key authentication.
	if !o.PasswordAuthentication && !o.PubkeyAuthentication {
		return errx.Validation("no_auth_method",
			"Disabling both password and public-key auth would lock everyone out.")
	}
	seen := map[string]bool{}
	out := o.AllowUsers[:0]
	for _, u := range o.AllowUsers {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if !reSSHUser.MatchString(u) {
			return errx.Validation("invalid_allow_user", "An allow-list user name is invalid: "+u)
		}
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	o.AllowUsers = out
	return nil
}

// Harden validates the options, renders the drop-in, and applies it through the
// broker (sshd -t, then reload). It returns the effective configuration the
// broker read back with `sshd -T`.
func (s *SSH) Harden(ctx context.Context, o SSHOptions) (map[string]string, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	if err := o.validate(); err != nil {
		return nil, err
	}
	if _, err := s.broker.Invoke(ctx, "ssh.harden", map[string]any{
		"config": RenderSSHDConfig(o),
	}); err != nil {
		return nil, err
	}
	return s.Status(ctx)
}

// Status returns the effective sshd configuration for the keys the panel manages.
func (s *SSH) Status(ctx context.Context) (map[string]string, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	res, err := s.broker.Invoke(ctx, "ssh.status", map[string]any{})
	if err != nil {
		return nil, err
	}
	raw, _ := res["effective"].(string)
	return ParseSSHDEffective(raw), nil
}

// RenderSSHDConfig renders the complete NexPanel sshd drop-in. Pure over
// validated options: fixed keys, validated values, plus a block of fixed
// hardening that is never a knob (empty passwords off, challenge-response off).
func RenderSSHDConfig(o SSHOptions) string {
	var b strings.Builder
	b.WriteString("# NexPanel SSH hardening (rendered; do not edit).\n")
	fmt.Fprintf(&b, "Port %d\n", o.Port)
	fmt.Fprintf(&b, "PermitRootLogin %s\n", o.PermitRootLogin)
	fmt.Fprintf(&b, "PasswordAuthentication %s\n", yesno(o.PasswordAuthentication))
	fmt.Fprintf(&b, "PubkeyAuthentication %s\n", yesno(o.PubkeyAuthentication))
	fmt.Fprintf(&b, "MaxAuthTries %d\n", o.MaxAuthTries)
	// Fixed hardening — not exposed as options because there is no good reason to
	// turn any of them the other way.
	b.WriteString("PermitEmptyPasswords no\n")
	b.WriteString("KbdInteractiveAuthentication no\n")
	b.WriteString("ChallengeResponseAuthentication no\n")
	b.WriteString("X11Forwarding no\n")
	b.WriteString("AllowAgentForwarding no\n")
	b.WriteString("LoginGraceTime 30\n")
	b.WriteString("ClientAliveInterval 300\n")
	b.WriteString("ClientAliveCountMax 2\n")
	if len(o.AllowUsers) > 0 {
		users := append([]string(nil), o.AllowUsers...)
		sort.Strings(users)
		fmt.Fprintf(&b, "AllowUsers %s\n", strings.Join(users, " "))
	}
	return b.String()
}

// ParseSSHDEffective parses `sshd -T` output (lower-cased keys, space-separated)
// into the subset of keys the panel manages.
func ParseSSHDEffective(raw string) map[string]string {
	want := map[string]bool{
		"port": true, "permitrootlogin": true, "passwordauthentication": true,
		"pubkeyauthentication": true, "maxauthtries": true, "allowusers": true,
		"permitemptypasswords": true, "x11forwarding": true,
	}
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		if want[key] {
			out[key] = strings.Join(fields[1:], " ")
		}
	}
	return out
}

func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
