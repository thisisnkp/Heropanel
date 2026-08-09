package mail

import (
	"context"
	"strings"
	"testing"
	"time"
)

// memSSO is an in-memory WebmailSSORepo.
type memSSO struct {
	rows []WebmailSSORecord
}

func (m *memSSO) InsertWebmailSSO(_ context.Context, r *WebmailSSORecord) error {
	m.rows = append(m.rows, *r)
	return nil
}
func (m *memSSO) ListActiveWebmailSSO(_ context.Context, now string) ([]WebmailSSORecord, error) {
	var out []WebmailSSORecord
	for _, r := range m.rows {
		if r.ExpiresAt > now {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memSSO) DeleteExpiredWebmailSSO(_ context.Context, now string) (int64, error) {
	kept := m.rows[:0]
	var n int64
	for _, r := range m.rows {
		if r.ExpiresAt <= now {
			n++
		} else {
			kept = append(kept, r)
		}
	}
	m.rows = append([]WebmailSSORecord(nil), kept...)
	return n, nil
}

// renderMasterFile is deterministic, one line per session, empty when none.
func TestRenderMasterFile(t *testing.T) {
	if got := renderMasterFile(nil); got != "" {
		t.Fatalf("empty set should render empty, got %q", got)
	}
	rows := []WebmailSSORecord{
		{MasterUser: "npsso_bbb", PWHash: "{BLF-CRYPT}h2"},
		{MasterUser: "npsso_aaa", PWHash: "{BLF-CRYPT}h1"},
	}
	got := renderMasterFile(rows)
	want := "npsso_aaa:{BLF-CRYPT}h1\nnpsso_bbb:{BLF-CRYPT}h2\n"
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

// StartWebmailSSO mints a one-time master credential bound to the mailbox, hands
// off `mailbox*master` + a one-time password, and applies the master file
// through the broker. The plaintext is never stored — only its hash.
func TestStartWebmailSSO(t *testing.T) {
	svc, repo, gw := newTestService()
	sso := &memSSO{}
	svc = svc.WithWebmailSSO("https://webmail.example.com/", sso)

	// Seed a domain + mailbox.
	repo.domains = []DomainRecord{{ID: 1, UID: "d1", Domain: "shop.test"}}
	repo.accounts = []AccountRecord{{ID: 1, UID: "acc1", DomainID: 1, LocalPart: "info"}}

	ho, err := svc.StartWebmailSSO(context.Background(), "acc1")
	if err != nil {
		t.Fatalf("StartWebmailSSO: %v", err)
	}
	if ho.URL != "https://webmail.example.com/" {
		t.Errorf("url = %q", ho.URL)
	}
	if !strings.HasPrefix(ho.User, "info@shop.test*npsso_") {
		t.Errorf("user = %q, want info@shop.test*npsso_…", ho.User)
	}
	if ho.Pass == "" {
		t.Error("no one-time password returned")
	}
	// The row stores the HASH, never the plaintext.
	if len(sso.rows) != 1 {
		t.Fatalf("stored %d sessions, want 1", len(sso.rows))
	}
	if sso.rows[0].PWHash == ho.Pass || !strings.Contains(sso.rows[0].PWHash, "hashed:") {
		t.Errorf("stored password not hashed: %q", sso.rows[0].PWHash)
	}
	if sso.rows[0].Mailbox != "info@shop.test" {
		t.Errorf("session mailbox = %q", sso.rows[0].Mailbox)
	}
	// The master file was applied through the broker with the master user in it.
	applied := ""
	for i, c := range gw.calls {
		if c == "mail.sso.apply" {
			applied, _ = gw.inputs[i]["master"].(string)
		}
	}
	if !strings.Contains(applied, sso.rows[0].MasterUser+":") {
		t.Errorf("master file not applied with the new user: %q", applied)
	}
}

// The sweeper drops expired sessions and re-applies the (now smaller) file.
func TestSweepWebmailSSO(t *testing.T) {
	svc, _, gw := newTestService()
	sso := &memSSO{}
	svc = svc.WithWebmailSSO("https://webmail.example.com/", sso)

	past := time.Now().UTC().Add(-time.Hour).Format(sqlTimeMail)
	future := time.Now().UTC().Add(time.Hour).Format(sqlTimeMail)
	sso.rows = []WebmailSSORecord{
		{UID: "a", MasterUser: "npsso_old", PWHash: "h", Mailbox: "a@x", ExpiresAt: past},
		{UID: "b", MasterUser: "npsso_new", PWHash: "h", Mailbox: "b@x", ExpiresAt: future},
	}
	n, err := svc.SweepWebmailSSO(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("sweep removed %d (err %v), want 1", n, err)
	}
	if len(sso.rows) != 1 || sso.rows[0].UID != "b" {
		t.Fatalf("wrong row survived: %+v", sso.rows)
	}
	// It re-applied with only the surviving master user.
	var applied string
	for i, c := range gw.calls {
		if c == "mail.sso.apply" {
			applied, _ = gw.inputs[i]["master"].(string)
		}
	}
	if strings.Contains(applied, "npsso_old") || !strings.Contains(applied, "npsso_new") {
		t.Errorf("re-apply after sweep wrong: %q", applied)
	}
}

// Without a webmail URL or store, SSO is unavailable.
func TestWebmailSSOUnavailable(t *testing.T) {
	svc, _, _ := newTestService()
	if svc.WebmailSSOAvailable() {
		t.Fatal("SSO available with nothing wired")
	}
	if _, err := svc.StartWebmailSSO(context.Background(), "acc1"); err == nil {
		t.Fatal("want unavailable error")
	}
}
