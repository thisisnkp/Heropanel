package mail

import (
	"crypto/rand"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/pkg/secrets"
)

func testCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	c, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGenerateDKIMProducesPEMAndTXT(t *testing.T) {
	priv, pub, err := generateDKIM()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(priv, "-----BEGIN RSA PRIVATE KEY-----") {
		t.Error("private key is not traditional PEM (what opendkim reads)")
	}
	if !strings.HasPrefix(pub, "v=DKIM1; k=rsa; p=") || len(pub) < 100 {
		t.Errorf("public TXT looks wrong: %.60s...", pub)
	}
}

// enableDKIM seals the private key (never plaintext at rest) and applyDKIM
// unseals it only for the broker hand-off, alongside rendered tables.
func TestDKIMSealedAtRestUnsealedForSigner(t *testing.T) {
	svc, repo, gw := newTestService()
	svc.cipher = testCipher(t)

	d, err := svc.CreateDomain(t.Context(), 1, "example.com")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if d.DKIMPublic == "" {
		t.Fatal("no DKIM public record on the created domain")
	}
	stored := repo.domains[0].DKIMPrivate
	if stored == "" || strings.Contains(stored, "PRIVATE KEY") {
		t.Error("the stored DKIM key is not sealed")
	}

	// The broker saw mail.dkim.apply with the UNSEALED PEM + rendered tables.
	var dkimIn map[string]any
	for i, cap := range gw.calls {
		if cap == "mail.dkim.apply" {
			dkimIn = gw.inputs[i]
		}
	}
	if dkimIn == nil {
		t.Fatal("mail.dkim.apply never reached the broker")
	}
	keys := dkimIn["keys"].([]map[string]any)
	if len(keys) != 1 || !strings.HasPrefix(keys[0]["private_pem"].(string), "-----BEGIN RSA PRIVATE KEY-----") {
		t.Error("the signer did not receive the unsealed key")
	}
	if kt := dkimIn["keytable"].(string); !strings.Contains(kt, "hp1._domainkey.example.com example.com:hp1:") {
		t.Errorf("keytable = %q", kt)
	}
	if st := dkimIn["signingtable"].(string); !strings.Contains(st, "*@example.com hp1._domainkey.example.com") {
		t.Errorf("signingtable = %q", st)
	}
}

// Without a cipher, domain creation still works — DKIM is simply absent, and
// the DNS check will show the record missing.
func TestNoCipherMeansNoDKIMNotNoDomain(t *testing.T) {
	svc, repo, gw := newTestService()

	d, err := svc.CreateDomain(t.Context(), 1, "example.com")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if d.DKIMPublic != "" || repo.domains[0].DKIMPrivate != "" {
		t.Error("DKIM material appeared without a data key")
	}
	for _, cap := range gw.calls {
		if cap == "mail.dkim.apply" {
			t.Error("the signer was configured without a key to seal with")
		}
	}
}

// The expected record set: MX and DMARC replace, SPF appends (the apex TXT
// label is shared with operator records), DKIM appears once generated.
func TestExpectedRecords(t *testing.T) {
	d := &DomainRecord{Domain: "example.com", DKIMSelector: "hp1"}
	recs := expectedRecords(d)
	if len(recs) != 3 {
		t.Fatalf("without DKIM: %d records, want 3", len(recs))
	}
	d.DKIMPublic = "v=DKIM1; k=rsa; p=abc"
	recs = expectedRecords(d)
	if len(recs) != 4 {
		t.Fatalf("with DKIM: %d records, want 4", len(recs))
	}
	byLabel := map[string]DNSRecord{}
	for _, r := range recs {
		byLabel[r.Label+"/"+r.Type] = r
	}
	if mx := byLabel["@/MX"]; !mx.Replace || mx.Value != "mail.example.com." || mx.Priority != 10 {
		t.Errorf("MX = %+v", mx)
	}
	if spf := byLabel["@/TXT"]; spf.Replace || spf.Value != "v=spf1 mx ~all" {
		t.Errorf("SPF = %+v (must append, never clobber operator TXT records)", spf)
	}
	if dk := byLabel["hp1._domainkey/TXT"]; !dk.Replace || dk.Value != d.DKIMPublic {
		t.Errorf("DKIM = %+v", dk)
	}
	if dm := byLabel["_dmarc/TXT"]; !strings.HasPrefix(dm.Value, "v=DMARC1; p=quarantine") {
		t.Errorf("DMARC = %+v", dm)
	}
}

func TestTxtEquivalentIgnoresResolverWhitespace(t *testing.T) {
	if !txtEquivalent("v=DKIM1;  k=rsa;\tp=abc", "v=DKIM1; k=rsa; p=abc") {
		t.Error("whitespace-only differences must match")
	}
	if txtEquivalent("v=spf1 -all", "v=spf1 ~all") {
		t.Error("different values matched")
	}
}
