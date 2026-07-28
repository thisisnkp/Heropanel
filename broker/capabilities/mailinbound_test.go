package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
)

// mail.inbound applies the level's postfix restrictions via postconf, keeping
// local submission (mynetworks / authenticated) exempt.
func TestMailInboundApplies(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()

	res, err := (capabilities.MailInbound{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"level": "standard",
	}))
	if err != nil {
		t.Fatalf("mail.inbound: %v", err)
	}
	if res.Data["level"] != "standard" {
		t.Errorf("level = %v", res.Data["level"])
	}
	var argv string
	for _, call := range fr.Calls {
		if call.Path == "/usr/sbin/postconf" && len(call.Args) > 0 && call.Args[0] == "-e" {
			argv = strings.Join(call.Args, " ")
		}
	}
	for _, want := range []string{
		"reject_unknown_sender_domain",
		"reject_non_fqdn_sender",
		"permit_mynetworks",
		"permit_sasl_authenticated",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("standard policy missing %q in postconf argv", want)
		}
	}
}

// An unknown level is refused and nothing is applied.
func TestMailInboundRejectsBadLevel(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	if _, err := (capabilities.MailInbound{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"level": "paranoid",
	})); err == nil {
		t.Fatal("an invalid level was accepted")
	}
	if len(fr.Calls) != 0 {
		t.Error("postconf ran for an invalid level")
	}
}

// "off" leaves a permissive recipient restriction that still blocks open relay.
func TestMailInboundOffKeepsRelayProtection(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	if _, err := (capabilities.MailInbound{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"level": "off",
	})); err != nil {
		t.Fatalf("off: %v", err)
	}
	var argv string
	for _, call := range fr.Calls {
		if call.Path == "/usr/sbin/postconf" && len(call.Args) > 0 && call.Args[0] == "-e" {
			argv = strings.Join(call.Args, " ")
		}
	}
	if !strings.Contains(argv, "reject_unauth_destination") {
		t.Error("off dropped open-relay protection")
	}
}
