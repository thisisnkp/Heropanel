package security

import (
	"context"
	"strings"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Fail2Ban surfacing: which jails exist, who is banned, and manual ban/unban.
// The broker returns fail2ban-client's raw output; the parsing lives here,
// tested over fixtures rather than against a running daemon.

// Jail is one fail2ban jail with its current bans.
type Jail struct {
	Name   string   `json:"name"`
	Banned []string `json:"banned"`
}

// Fail2Ban orchestrates the intrusion-prevention view.
type Fail2Ban struct {
	broker broker.Gateway
}

// NewFail2Ban constructs the service.
func NewFail2Ban(gw broker.Gateway) *Fail2Ban { return &Fail2Ban{broker: gw} }

// Available reports whether fail2ban can be queried.
func (f *Fail2Ban) Available() bool { return f != nil && f.broker != nil }

func (f *Fail2Ban) requireAvailable() error {
	if f.Available() {
		return nil
	}
	return errx.New(errx.KindUnavailable, "fail2ban_unavailable", "Fail2Ban needs the broker.")
}

// Overview lists every jail and its banned IPs. running=false means the
// fail2ban server is not up — an answer, not an error.
func (f *Fail2Ban) Overview(ctx context.Context) ([]Jail, bool, error) {
	if err := f.requireAvailable(); err != nil {
		return nil, false, err
	}
	res, err := f.broker.Invoke(ctx, "fail2ban.status", map[string]any{})
	if err != nil {
		return nil, false, err
	}
	running, _ := res["running"].(bool)
	if !running {
		return []Jail{}, false, nil
	}
	raw, _ := res["raw"].(string)
	names := parseJailList(raw)
	jails := make([]Jail, 0, len(names))
	for _, name := range names {
		jr, err := f.broker.Invoke(ctx, "fail2ban.status", map[string]any{"jail": name})
		if err != nil {
			jails = append(jails, Jail{Name: name})
			continue
		}
		jraw, _ := jr["raw"].(string)
		jails = append(jails, Jail{Name: name, Banned: parseBannedIPs(jraw)})
	}
	return jails, true, nil
}

// Ban manually bans an IP in a jail.
func (f *Fail2Ban) Ban(ctx context.Context, jail, ip string) error {
	if err := f.requireAvailable(); err != nil {
		return err
	}
	_, err := f.broker.Invoke(ctx, "fail2ban.ban", map[string]any{"jail": jail, "ip": ip})
	return err
}

// Unban lifts a ban.
func (f *Fail2Ban) Unban(ctx context.Context, jail, ip string) error {
	if err := f.requireAvailable(); err != nil {
		return err
	}
	_, err := f.broker.Invoke(ctx, "fail2ban.unban", map[string]any{"jail": jail, "ip": ip})
	return err
}

// parseJailList pulls the jail names out of `fail2ban-client status`:
//
//	`- Jail list:  sshd, nginx-http-auth
func parseJailList(raw string) []string {
	for _, line := range strings.Split(raw, "\n") {
		i := strings.Index(line, "Jail list:")
		if i < 0 {
			continue
		}
		rest := strings.TrimSpace(line[i+len("Jail list:"):])
		if rest == "" {
			return nil
		}
		var out []string
		for _, name := range strings.Split(rest, ",") {
			if n := strings.TrimSpace(name); n != "" {
				out = append(out, n)
			}
		}
		return out
	}
	return nil
}

// parseBannedIPs pulls the addresses out of `fail2ban-client status <jail>`:
//
//	`- Banned IP list:   203.0.113.4 203.0.113.9
func parseBannedIPs(raw string) []string {
	for _, line := range strings.Split(raw, "\n") {
		i := strings.Index(line, "Banned IP list:")
		if i < 0 {
			continue
		}
		out := []string{}
		for _, ip := range strings.Fields(line[i+len("Banned IP list:"):]) {
			out = append(out, ip)
		}
		return out
	}
	return []string{}
}
