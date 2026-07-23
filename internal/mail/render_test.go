package mail

import (
	"strings"
	"testing"
)

func TestRenderDomainsIsSortedPostfixHashSource(t *testing.T) {
	got := RenderDomains([]string{"zeta.example", "alpha.example"})
	want := "alpha.example OK\nzeta.example OK\n"
	if got != want {
		t.Errorf("domains = %q, want %q", got, want)
	}
	if RenderDomains(nil) != "" {
		t.Error("no domains must render an empty file, not a stray newline")
	}
}

// Every account keeps its mailbox mapping regardless of status: a suspended
// account stops logging in, not receiving.
func TestRenderMailboxesIncludesSuspendedAccounts(t *testing.T) {
	got := RenderMailboxes([]RenderAccountRow{
		{Domain: "example.com", LocalPart: "info", Active: true},
		{Domain: "example.com", LocalPart: "old", Active: false},
	})
	if !strings.Contains(got, "old@example.com example.com/old/Maildir/") {
		t.Errorf("suspended account lost its mailbox mapping: %q", got)
	}
}

// The passwd-file carries ONLY active accounts, with quota as a userdb extra
// field in dovecot's documented format.
func TestRenderUsersDropsSuspendedAndCarriesQuota(t *testing.T) {
	got := RenderUsers([]RenderAccountRow{
		{Domain: "example.com", LocalPart: "info", PasswordHash: "{BLF-CRYPT}$2a$x", QuotaMB: 2048, Active: true},
		{Domain: "example.com", LocalPart: "old", PasswordHash: "{BLF-CRYPT}$2a$y", QuotaMB: 1024, Active: false},
	})
	want := "info@example.com:{BLF-CRYPT}$2a$x::::::userdb_quota_rule=*:storage=2048M\n"
	if got != want {
		t.Errorf("users = %q, want %q", got, want)
	}
}

// Aliases group destinations per source, the form postfix expects.
func TestRenderAliasesGroupsDestinations(t *testing.T) {
	got := RenderAliases([]RenderAliasRow{
		{Domain: "example.com", Source: "sales", Destination: "z@ext.example"},
		{Domain: "example.com", Source: "sales", Destination: "info@example.com"},
		{Domain: "example.com", Source: "abuse", Destination: "info@example.com"},
	})
	want := "abuse@example.com info@example.com\nsales@example.com info@example.com,z@ext.example\n"
	if got != want {
		t.Errorf("aliases = %q, want %q", got, want)
	}
}
