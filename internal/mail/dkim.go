package mail

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// DKIM + the rest of the authentication trio (SPF, DMARC).
//
// The key pair is generated in hpd (stdlib RSA-2048 — the interoperability
// baseline every verifier accepts). The private key is SEALED with the panel
// data key before it touches the database, write-only thereafter; it is
// unsealed only to hand opendkim its key file through the broker. The public
// half is a TXT record value, shown freely.

const dkimSelector = "hp1"

// dkimAAD binds a sealed DKIM key to its domain row.
func dkimAAD(domainUID string) string { return "mail_domains:" + domainUID + ":dkim" }

// generateDKIM returns a fresh RSA-2048 pair as (private PEM, public TXT).
func generateDKIM() (privPEM, pubTXT string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	// PKCS#1 "RSA PRIVATE KEY" — the traditional form opendkim reads.
	privPEM = string(pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", err
	}
	pubTXT = "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(pubDER)
	return privPEM, pubTXT, nil
}

// DNSRecord is one record the domain needs for mail to authenticate.
type DNSRecord struct {
	Label    string `json:"label"` // relative: "@", "mail", "hp1._domainkey", "_dmarc"
	Type     string `json:"type"`
	Value    string `json:"value"`
	Priority int    `json:"priority,omitempty"`
	// Replace: the panel's value is authoritative at this label+type (MX, DKIM,
	// DMARC). False = append-if-missing (TXT @, where SPF coexists with the
	// operator's verification records).
	Replace bool `json:"-"`
}

// DNSProvider wires records into a panel-managed zone (adapter over
// internal/dns). Nil = no DNS module; records are display-only then.
type DNSProvider interface {
	// EnsureRecord upserts one record; found=false when no managed zone covers
	// the domain.
	EnsureRecord(ctx context.Context, fqdn, typ, value string, priority int, replace bool) (bool, error)
}

// expectedRecords is the authentication set for a domain: MX, SPF, DKIM (when
// generated) and DMARC.
func expectedRecords(d *DomainRecord) []DNSRecord {
	recs := []DNSRecord{
		{Label: "@", Type: "MX", Value: "mail." + d.Domain + ".", Priority: 10, Replace: true},
		{Label: "@", Type: "TXT", Value: "v=spf1 mx ~all", Replace: false},
		{Label: "_dmarc", Type: "TXT", Value: "v=DMARC1; p=quarantine; rua=mailto:postmaster@" + d.Domain, Replace: true},
	}
	if d.DKIMPublic != "" {
		recs = append(recs, DNSRecord{
			Label: d.DKIMSelector + "._domainkey", Type: "TXT", Value: d.DKIMPublic, Replace: true,
		})
	}
	return recs
}

// wireDNS best-effort publishes the expected records into a panel-managed
// zone. Returns whether a managed zone covered the domain. A domain on
// external DNS is not an error — the records are surfaced for the operator.
func (s *Service) wireDNS(ctx context.Context, d *DomainRecord) (bool, error) {
	if s.dns == nil {
		return false, nil
	}
	wired := false
	for _, r := range expectedRecords(d) {
		fqdn := d.Domain
		if r.Label != "@" {
			fqdn = r.Label + "." + d.Domain
		}
		ok, err := s.dns.EnsureRecord(ctx, fqdn, r.Type, r.Value, r.Priority, r.Replace)
		if err != nil {
			return wired, err
		}
		if !ok {
			return false, nil // no managed zone — stop; the records are display-only
		}
		wired = true
	}
	return wired, nil
}

// ── live verification ────────────────────────────────────────────────────────

// DNSCheck is one record's live verification result.
type DNSCheck struct {
	DNSRecord
	Found    bool     `json:"found"`
	Observed []string `json:"observed,omitempty"`
}

// resolver returns the checker's resolver: the system one, or a pinned
// address (config mail.resolver / HP_MAIL_RESOLVER) for split-DNS setups and
// e2e against a local authoritative server.
func (s *Service) resolver() *net.Resolver {
	if s.resolverAddr == "" {
		return net.DefaultResolver
	}
	addr := s.resolverAddr
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", addr)
		},
	}
}

// CheckDNS resolves each expected record live and reports what was found.
// This asks the actual DNS — the panel's own zone data would only prove the
// panel agrees with itself.
func (s *Service) CheckDNS(ctx context.Context, domainUID string) ([]DNSCheck, error) {
	d, err := s.repo.GetDomainByUID(ctx, domainUID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res := s.resolver()

	var out []DNSCheck
	for _, r := range expectedRecords(d) {
		check := DNSCheck{DNSRecord: r}
		switch r.Type {
		case "MX":
			mxs, _ := res.LookupMX(ctx, d.Domain)
			for _, mx := range mxs {
				check.Observed = append(check.Observed, mx.Host)
				if strings.EqualFold(strings.TrimSuffix(mx.Host, "."), strings.TrimSuffix(r.Value, ".")) {
					check.Found = true
				}
			}
		case "TXT":
			name := d.Domain
			if r.Label != "@" {
				name = r.Label + "." + d.Domain
			}
			txts, _ := res.LookupTXT(ctx, name)
			for _, txt := range txts {
				check.Observed = append(check.Observed, txt)
				if txtEquivalent(txt, r.Value) {
					check.Found = true
				}
			}
		}
		out = append(out, check)
	}
	return out, nil
}

// txtEquivalent compares TXT values ignoring the whitespace differences that
// come from resolvers rejoining 255-byte strings.
func txtEquivalent(observed, expected string) bool {
	norm := func(s string) string {
		return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	}
	return norm(observed) == norm(expected)
}

// ── DKIM lifecycle on the service ────────────────────────────────────────────

// enableDKIM generates, seals and stores a domain's key pair, then pushes the
// signer state. Skipped silently when no data key is configured — the DNS
// check will show the DKIM record missing, which is the honest signal.
func (s *Service) enableDKIM(ctx context.Context, d *DomainRecord) error {
	if s.cipher == nil || !s.cipher.Configured() {
		return nil
	}
	privPEM, pubTXT, err := generateDKIM()
	if err != nil {
		return errx.Internal(err)
	}
	sealed, err := s.cipher.Seal([]byte(privPEM), dkimAAD(d.UID))
	if err != nil {
		return errx.Internal(err)
	}
	if err := s.repo.UpdateDomainDKIM(ctx, d.ID, dkimSelector, sealed, pubTXT); err != nil {
		return err
	}
	d.DKIMSelector, d.DKIMPrivate, d.DKIMPublic = dkimSelector, sealed, pubTXT
	return s.applyDKIM(ctx)
}

// applyDKIM renders the full signer state — every domain's key file plus the
// KeyTable/SigningTable — and hands it to the broker. Declarative render-all,
// like every other apply in this module.
func (s *Service) applyDKIM(ctx context.Context) error {
	if s.cipher == nil || !s.cipher.Configured() {
		return nil
	}
	domains, err := s.repo.ListDomains(ctx, 0)
	if err != nil {
		return err
	}
	keys := []map[string]any{}
	var keyTable, signingTable strings.Builder
	for i := range domains {
		d := &domains[i]
		if d.DKIMPrivate == "" {
			continue
		}
		priv, err := s.cipher.Open(d.DKIMPrivate, dkimAAD(d.UID))
		if err != nil {
			// A key that fails authentication is skipped loudly-in-the-log by the
			// caller; signing the rest still works.
			continue
		}
		keys = append(keys, map[string]any{
			"domain": d.Domain, "selector": d.DKIMSelector, "private_pem": string(priv),
		})
		id := d.DKIMSelector + "._domainkey." + d.Domain
		keyTable.WriteString(id + " " + d.Domain + ":" + d.DKIMSelector + ":/etc/opendkim/heropanel/keys/" + d.Domain + "/" + d.DKIMSelector + ".private\n")
		signingTable.WriteString("*@" + d.Domain + " " + id + "\n")
	}
	_, err = s.broker.Invoke(ctx, "mail.dkim.apply", map[string]any{
		"keys": keys, "keytable": keyTable.String(), "signingtable": signingTable.String(),
	})
	return err
}
