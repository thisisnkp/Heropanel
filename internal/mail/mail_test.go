package mail

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeGW records invocations and can fail a named capability.
type fakeGW struct {
	calls  []string
	inputs []map[string]any
	fail   string
}

func (g *fakeGW) Invoke(_ context.Context, cap string, input any) (map[string]any, error) {
	g.calls = append(g.calls, cap)
	if m, ok := input.(map[string]any); ok {
		g.inputs = append(g.inputs, m)
	} else {
		g.inputs = append(g.inputs, nil)
	}
	if cap == g.fail {
		return nil, errors.New(cap + " failed")
	}
	return map[string]any{}, nil
}
func (g *fakeGW) Health(context.Context) error { return nil }

// memRepo is an in-memory Repo.
type memRepo struct {
	domains  []DomainRecord
	accounts []AccountRecord
	aliases  []AliasRecord
	nextID   int64
}

func (m *memRepo) id() int64 { m.nextID++; return m.nextID }

func (m *memRepo) InsertDomain(_ context.Context, r *DomainRecord) error {
	r.ID = m.id()
	m.domains = append(m.domains, *r)
	return nil
}
func (m *memRepo) ListDomains(_ context.Context, ownerID int64) ([]DomainRecord, error) {
	return m.domains, nil
}
func (m *memRepo) GetDomainByUID(_ context.Context, uid string) (*DomainRecord, error) {
	for i := range m.domains {
		if m.domains[i].UID == uid {
			return &m.domains[i], nil
		}
	}
	return nil, errors.New("no such domain")
}
func (m *memRepo) UpdateDomainDKIM(_ context.Context, id int64, sel, priv, pub string) error {
	for i := range m.domains {
		if m.domains[i].ID == id {
			m.domains[i].DKIMSelector, m.domains[i].DKIMPrivate, m.domains[i].DKIMPublic = sel, priv, pub
		}
	}
	return nil
}
func (m *memRepo) DeleteDomain(_ context.Context, uid string) error {
	for i := range m.domains {
		if m.domains[i].UID == uid {
			m.domains = append(m.domains[:i], m.domains[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *memRepo) InsertAccount(_ context.Context, r *AccountRecord) error {
	r.ID = m.id()
	m.accounts = append(m.accounts, *r)
	return nil
}
func (m *memRepo) ListAccounts(_ context.Context, domainID int64) ([]AccountRecord, error) {
	var out []AccountRecord
	for _, a := range m.accounts {
		if a.DomainID == domainID {
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *memRepo) GetAccountByUID(_ context.Context, uid string) (*AccountRecord, error) {
	for i := range m.accounts {
		if m.accounts[i].UID == uid {
			return &m.accounts[i], nil
		}
	}
	return nil, errors.New("no such account")
}
func (m *memRepo) UpdateAccountPassword(_ context.Context, id int64, hash string) error {
	for i := range m.accounts {
		if m.accounts[i].ID == id {
			m.accounts[i].PasswordHash = hash
		}
	}
	return nil
}
func (m *memRepo) UpdateAccountQuota(_ context.Context, id int64, q int) error {
	for i := range m.accounts {
		if m.accounts[i].ID == id {
			m.accounts[i].QuotaMB = q
		}
	}
	return nil
}
func (m *memRepo) UpdateAccountStatus(_ context.Context, id int64, st string) error {
	for i := range m.accounts {
		if m.accounts[i].ID == id {
			m.accounts[i].Status = st
		}
	}
	return nil
}
func (m *memRepo) DeleteAccount(_ context.Context, uid string) error {
	for i := range m.accounts {
		if m.accounts[i].UID == uid {
			m.accounts = append(m.accounts[:i], m.accounts[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *memRepo) InsertAlias(_ context.Context, r *AliasRecord) error {
	r.ID = m.id()
	m.aliases = append(m.aliases, *r)
	return nil
}
func (m *memRepo) ListAliases(_ context.Context, domainID int64) ([]AliasRecord, error) {
	var out []AliasRecord
	for _, a := range m.aliases {
		if a.DomainID == domainID {
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *memRepo) GetAliasByUID(_ context.Context, uid string) (*AliasRecord, error) {
	for i := range m.aliases {
		if m.aliases[i].UID == uid {
			return &m.aliases[i], nil
		}
	}
	return nil, errors.New("no such alias")
}
func (m *memRepo) DeleteAlias(_ context.Context, uid string) error {
	for i := range m.aliases {
		if m.aliases[i].UID == uid {
			m.aliases = append(m.aliases[:i], m.aliases[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *memRepo) RenderAccounts(_ context.Context) ([]RenderAccountRow, error) {
	var out []RenderAccountRow
	for _, a := range m.accounts {
		dom := ""
		for _, d := range m.domains {
			if d.ID == a.DomainID {
				dom = d.Domain
			}
		}
		out = append(out, RenderAccountRow{
			Domain: dom, LocalPart: a.LocalPart, PasswordHash: a.PasswordHash,
			QuotaMB: a.QuotaMB, Active: a.Status == "active",
		})
	}
	return out, nil
}
func (m *memRepo) RenderAliases(_ context.Context) ([]RenderAliasRow, error) {
	var out []RenderAliasRow
	for _, a := range m.aliases {
		dom := ""
		for _, d := range m.domains {
			if d.ID == a.DomainID {
				dom = d.Domain
			}
		}
		out = append(out, RenderAliasRow{Domain: dom, Source: a.Source, Destination: a.Destination})
	}
	return out, nil
}
func (m *memRepo) AllDomainNames(_ context.Context) ([]string, error) {
	var out []string
	for _, d := range m.domains {
		out = append(out, d.Domain)
	}
	return out, nil
}

func newTestService() (*Service, *memRepo, *fakeGW) {
	repo := &memRepo{}
	gw := &fakeGW{}
	svc := NewService(repo, gw)
	// bcrypt is deliberately slow; tests only need a stable marker.
	svc.hash = func(p string) (string, error) { return "{BLF-CRYPT}hashed:" + p, nil }
	return svc, repo, gw
}

// Creating a domain provisions the host first, then applies the rendered maps.
func TestCreateDomainProvisionsThenApplies(t *testing.T) {
	svc, _, gw := newTestService()

	d, err := svc.CreateDomain(t.Context(), 1, "Example.COM")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if d.Domain != "example.com" {
		t.Errorf("domain not normalised: %q", d.Domain)
	}
	if strings.Join(gw.calls, ",") != "mail.provision,mail.apply" {
		t.Errorf("calls = %v, want provision then apply", gw.calls)
	}
	// The rendered domains map reached the broker.
	last := gw.inputs[len(gw.inputs)-1]
	if got := last["domains"].(string); got != "example.com OK\n" {
		t.Errorf("rendered domains = %q", got)
	}

	if _, err := svc.CreateDomain(t.Context(), 1, "not a domain"); err == nil {
		t.Error("an invalid domain was accepted")
	}
}

// A failed apply rolls the row back — no domain the MTA never learned.
func TestCreateDomainRollsBackWhenApplyFails(t *testing.T) {
	svc, repo, gw := newTestService()
	gw.fail = "mail.apply"

	if _, err := svc.CreateDomain(t.Context(), 1, "example.com"); err == nil {
		t.Fatal("a failed apply reported success")
	}
	if len(repo.domains) != 0 {
		t.Error("the domain row survived a failed apply")
	}
}

// Accounts: created with a hashed password, quota bounds enforced, apply gets
// the full rendered state including the passwd-file line.
func TestCreateAccountHashesAndApplies(t *testing.T) {
	svc, repo, gw := newTestService()
	d, _ := svc.CreateDomain(t.Context(), 1, "example.com")

	a, err := svc.CreateAccount(t.Context(), d.UID, "Info", "s3cretpass", 0)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if a.Address != "info@example.com" || a.QuotaMB != 1024 {
		t.Errorf("account = %+v", a)
	}
	if repo.accounts[0].PasswordHash != "{BLF-CRYPT}hashed:s3cretpass" {
		t.Errorf("stored hash = %q", repo.accounts[0].PasswordHash)
	}
	last := gw.inputs[len(gw.inputs)-1]
	if users := last["users"].(string); !strings.Contains(users, "info@example.com:{BLF-CRYPT}hashed:s3cretpass") {
		t.Errorf("users file missing the account: %q", users)
	}

	if _, err := svc.CreateAccount(t.Context(), d.UID, "bad", "short", 0); err == nil {
		t.Error("a 5-char password was accepted")
	}
	if _, err := svc.CreateAccount(t.Context(), d.UID, "UP CASE", "longenough", 0); err == nil {
		t.Error("an invalid local part was accepted")
	}
}

// Suspension: the account leaves the passwd-file but keeps its mailbox map.
func TestSuspendBlocksLoginButKeepsReceiving(t *testing.T) {
	svc, _, gw := newTestService()
	d, _ := svc.CreateDomain(t.Context(), 1, "example.com")
	a, _ := svc.CreateAccount(t.Context(), d.UID, "info", "s3cretpass", 0)

	if err := svc.SetAccountStatus(t.Context(), a.UID, "suspended"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	last := gw.inputs[len(gw.inputs)-1]
	if users := last["users"].(string); strings.Contains(users, "info@example.com") {
		t.Error("a suspended account can still log in")
	}
	if boxes := last["mailboxes"].(string); !strings.Contains(boxes, "info@example.com") {
		t.Error("a suspended account stopped receiving")
	}
}

// Deleting with purge derives the broker input from the stored rows.
func TestDeleteAccountPurgeUsesStoredParts(t *testing.T) {
	svc, _, gw := newTestService()
	d, _ := svc.CreateDomain(t.Context(), 1, "example.com")
	a, _ := svc.CreateAccount(t.Context(), d.UID, "info", "s3cretpass", 0)

	if err := svc.DeleteAccount(t.Context(), d.UID, a.UID, true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	lastCap := gw.calls[len(gw.calls)-1]
	lastIn := gw.inputs[len(gw.inputs)-1]
	if lastCap != "mail.purge" || lastIn["domain"] != "example.com" || lastIn["local_part"] != "info" {
		t.Errorf("purge call = %s %v", lastCap, lastIn)
	}
}

// Aliases validate both halves and refuse self-loops.
func TestCreateAliasValidates(t *testing.T) {
	svc, _, _ := newTestService()
	d, _ := svc.CreateDomain(t.Context(), 1, "example.com")

	if _, err := svc.CreateAlias(t.Context(), d.UID, "sales", "info@example.com"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	if _, err := svc.CreateAlias(t.Context(), d.UID, "x", "x@example.com"); err == nil {
		t.Error("a self-loop alias was accepted")
	}
	if _, err := svc.CreateAlias(t.Context(), d.UID, "y", "not-an-address"); err == nil {
		t.Error("a bad destination was accepted")
	}
}
