package security

import (
	"context"
	"strings"
	"testing"
)

func TestParseJailList(t *testing.T) {
	raw := "Status\n|- Number of jail:\t2\n`- Jail list:\tsshd, nginx-http-auth\n"
	got := parseJailList(raw)
	if strings.Join(got, ",") != "sshd,nginx-http-auth" {
		t.Errorf("jails = %v", got)
	}
	if len(parseJailList("Status\n`- Jail list:\t\n")) != 0 {
		t.Error("empty jail list should parse to nothing")
	}
}

func TestParseBannedIPs(t *testing.T) {
	raw := "Status for the jail: sshd\n" +
		"|- Filter\n|  `- File list:\t/var/log/auth.log\n" +
		"`- Actions\n   |- Currently banned:\t2\n   `- Banned IP list:\t203.0.113.4 203.0.113.9\n"
	got := parseBannedIPs(raw)
	if len(got) != 2 || got[0] != "203.0.113.4" || got[1] != "203.0.113.9" {
		t.Errorf("banned = %v", got)
	}
	if len(parseBannedIPs("`- Banned IP list:\t\n")) != 0 {
		t.Error("no bans should parse to an empty list")
	}
}

// gwF2B mocks the broker for the fail2ban overview (list then per-jail).
type gwF2B struct {
	list    string
	perJail map[string]string
	calls   []string
}

func (g *gwF2B) Invoke(_ context.Context, cap string, input any) (map[string]any, error) {
	g.calls = append(g.calls, cap)
	m, _ := input.(map[string]any)
	jail, _ := m["jail"].(string)
	if cap == "fail2ban.status" {
		if jail == "" {
			return map[string]any{"raw": g.list, "running": true}, nil
		}
		return map[string]any{"raw": g.perJail[jail], "running": true}, nil
	}
	return map[string]any{"ok": true}, nil
}
func (g *gwF2B) Health(context.Context) error { return nil }

func TestOverviewListsJailsWithBans(t *testing.T) {
	gw := &gwF2B{
		list: "`- Jail list:\tsshd, nginx-http-auth\n",
		perJail: map[string]string{
			"sshd":            "`- Banned IP list:\t203.0.113.4\n",
			"nginx-http-auth": "`- Banned IP list:\t\n",
		},
	}
	f := NewFail2Ban(gw)
	jails, running, err := f.Overview(t.Context())
	if err != nil || !running {
		t.Fatalf("overview: %v running=%v", err, running)
	}
	if len(jails) != 2 || jails[0].Name != "sshd" || len(jails[0].Banned) != 1 {
		t.Fatalf("jails = %+v", jails)
	}
	if jails[0].Banned[0] != "203.0.113.4" {
		t.Errorf("banned = %v", jails[0].Banned)
	}
}

// A stopped fail2ban server reports running=false, not an error.
func TestOverviewStoppedServer(t *testing.T) {
	f := NewFail2Ban(&stoppedGW{})
	jails, running, err := f.Overview(t.Context())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if running || len(jails) != 0 {
		t.Errorf("stopped server: running=%v jails=%d", running, len(jails))
	}
}

type stoppedGW struct{}

func (stoppedGW) Invoke(context.Context, string, any) (map[string]any, error) {
	return map[string]any{"raw": "", "running": false}, nil
}
func (stoppedGW) Health(context.Context) error { return nil }
