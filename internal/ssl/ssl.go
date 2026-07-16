// Package ssl issues and installs TLS certificates for hosted sites:
// self-signed (immediate), custom upload, and Let's Encrypt via ACME. hpd
// obtains the material; the broker writes it to disk and (for ACME HTTP-01) the
// challenge to the site webroot. See docs/03 §6.
package ssl

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Providers.
const (
	ProviderSelfSigned  = "self_signed"
	ProviderCustom      = "custom"
	ProviderLetsEncrypt = "letsencrypt"
)

// Cert is the API view of a certificate (never includes the private key).
type Cert struct {
	UID        string   `json:"uid"`
	Provider   string   `json:"provider"`
	CommonName string   `json:"common_name"`
	SANs       []string `json:"sans"`
	Status     string   `json:"status"`
	IssuedAt   string   `json:"issued_at"`
	ExpiresAt  string   `json:"expires_at"`
	AutoRenew  bool     `json:"auto_renew"`
	CreatedAt  string   `json:"created_at"`
}

// Record is the persistence row.
type Record struct {
	ID         int64  `db:"id"`
	UID        string `db:"uid"`
	OwnerID    int64  `db:"owner_id"`
	Provider   string `db:"provider"`
	CommonName string `db:"common_name"`
	SANs       []byte `db:"sans"`
	IsWildcard int    `db:"is_wildcard"`
	CertPEM    string `db:"cert_pem"`
	PrivkeyEnc []byte `db:"privkey_enc"`
	IssuedAt   string `db:"issued_at"`
	ExpiresAt  string `db:"expires_at"`
	AutoRenew  int    `db:"auto_renew"`
	Status     string `db:"status"`
	CreatedAt  string `db:"created_at"`
	// Webroot records how an ACME cert was obtained so the renewer can repeat it:
	// a path => HTTP-01, empty => DNS-01 (always the case for wildcards).
	Webroot string `db:"webroot"`
}

// Repo is the persistence contract (implemented by internal/repository).
type Repo interface {
	Insert(ctx context.Context, r *Record) error
	List(ctx context.Context, ownerID int64, limit, offset int) ([]Record, error)
	GetByUID(ctx context.Context, uid string) (*Record, error)
	Delete(ctx context.Context, uid string) error
	// ListDueForRenewal returns auto-renewing certificates that expire at or
	// before the given timestamp.
	ListDueForRenewal(ctx context.Context, before string) ([]Record, error)
}

// ACME issues a certificate for a domain using an HTTP-01 challenge. The
// writeChallenge callback publishes the challenge file (via the broker).
type ACME interface {
	Issue(ctx context.Context, domain string, writeChallenge func(token, keyAuth string) error) (certPEM, keyPEM string, notAfter time.Time, err error)
}

// ACMEDNS issues a certificate using a DNS-01 challenge, publishing the token as
// a TXT record. It is the only way to obtain a wildcard certificate. An issuer
// may implement both ACME and ACMEDNS.
type ACMEDNS interface {
	IssueDNS01(ctx context.Context, domain string,
		setTXT func(fqdn, value string) error, deleteTXT func(fqdn string) error) (certPEM, keyPEM string, notAfter time.Time, err error)
}

// DNSProvider publishes ACME DNS-01 challenge records into a zone HeroPanel is
// authoritative for. Implemented by an adapter over internal/dns.
type DNSProvider interface {
	SetTXT(ctx context.Context, fqdn, value string) error
	DeleteTXT(ctx context.Context, fqdn string) error
}

// Service issues, installs, and records certificates.
type Service struct {
	repo   Repo
	broker broker.Gateway
	acme   ACME        // optional (nil => Let's Encrypt disabled)
	dns    DNSProvider // optional (nil => DNS-01 / wildcard unavailable)
}

// NewService constructs the SSL Service. acme may be nil.
func NewService(repo Repo, gw broker.Gateway, acme ACME) *Service {
	return &Service{repo: repo, broker: gw, acme: acme}
}

// WithDNS wires the DNS provider used for DNS-01 (and therefore wildcard)
// issuance. Returns s for chaining.
func (s *Service) WithDNS(p DNSProvider) *Service {
	s.dns = p
	return s
}

func (s *Service) requireBroker() error {
	if s.broker == nil {
		return errx.New(errx.KindUnavailable, "broker_unavailable", "The broker is not available.")
	}
	return nil
}

// IssueSelfSigned generates and installs a self-signed certificate. This gives a
// site working HTTPS immediately (browsers warn until replaced by a real cert).
func (s *Service) IssueSelfSigned(ctx context.Context, ownerID int64, domain string) (*Cert, error) {
	domain = normalizeDomain(domain)
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	certPEM, keyPEM, notAfter, err := generateSelfSigned(domain)
	if err != nil {
		return nil, errx.Internal(err)
	}
	return s.installAndRecord(ctx, ownerID, domain, ProviderSelfSigned, certPEM, keyPEM, notAfter, "")
}

// UploadCustom installs a caller-provided certificate and key (validated).
func (s *Service) UploadCustom(ctx context.Context, ownerID int64, certPEM, keyPEM string) (*Cert, error) {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, errx.Validation("invalid_cert", "The certificate and key are invalid or do not match.")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, errx.Validation("invalid_cert", "Could not parse the certificate.")
	}
	domain := leaf.Subject.CommonName
	if domain == "" && len(leaf.DNSNames) > 0 {
		domain = leaf.DNSNames[0]
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	return s.installAndRecord(ctx, ownerID, normalizeDomain(domain), ProviderCustom, certPEM, keyPEM, leaf.NotAfter, "")
}

// Issue obtains a Let's Encrypt certificate via ACME HTTP-01, writing the
// challenge into the given site webroot.
func (s *Service) Issue(ctx context.Context, ownerID int64, domain, webroot string) (*Cert, error) {
	domain = normalizeDomain(domain)
	if s.acme == nil {
		return nil, errx.New(errx.KindUnavailable, "acme_unavailable", "Let's Encrypt is not configured.")
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	writeChallenge := func(token, keyAuth string) error {
		_, err := s.broker.Invoke(ctx, "cert.write_challenge", map[string]any{
			"webroot": webroot, "token": token, "key_auth": keyAuth,
		})
		return err
	}
	certPEM, keyPEM, notAfter, err := s.acme.Issue(ctx, domain, writeChallenge)
	if err != nil {
		return nil, errx.Upstream(err, "acme_failed", "Certificate issuance failed.")
	}
	return s.installAndRecord(ctx, ownerID, domain, ProviderLetsEncrypt, certPEM, keyPEM, notAfter, webroot)
}

// IssueDNS obtains a Let's Encrypt certificate via ACME DNS-01, publishing the
// challenge into a zone HeroPanel is authoritative for. This is the path for
// **wildcard** certificates ("*.example.com"), which HTTP-01 cannot issue.
func (s *Service) IssueDNS(ctx context.Context, ownerID int64, domain string) (*Cert, error) {
	domain = normalizeDomain(domain)
	if err := validateIssuableDomain(domain); err != nil {
		return nil, err
	}
	issuer, ok := s.acme.(ACMEDNS)
	if s.acme == nil || !ok {
		return nil, errx.New(errx.KindUnavailable, "acme_dns_unavailable",
			"DNS-01 issuance is not configured.")
	}
	if s.dns == nil {
		return nil, errx.New(errx.KindUnavailable, "dns_unavailable",
			"DNS-01 needs a zone HeroPanel is authoritative for; the DNS module is not configured.")
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}

	setTXT := func(fqdn, value string) error { return s.dns.SetTXT(ctx, fqdn, value) }
	deleteTXT := func(fqdn string) error { return s.dns.DeleteTXT(ctx, fqdn) }

	certPEM, keyPEM, notAfter, err := issuer.IssueDNS01(ctx, domain, setTXT, deleteTXT)
	if err != nil {
		return nil, errx.Upstream(err, "acme_failed", "Certificate issuance failed.")
	}
	// A DNS-01 cert has no webroot: renewal repeats the DNS-01 flow.
	return s.installAndRecord(ctx, ownerID, domain, ProviderLetsEncrypt, certPEM, keyPEM, notAfter, "")
}

// validateIssuableDomain accepts a hostname or a single-label wildcard.
func validateIssuableDomain(d string) error {
	base := strings.TrimPrefix(d, "*.")
	if base == "" || !strings.Contains(base, ".") || strings.ContainsAny(base, "*") ||
		strings.ContainsAny(d, " \t\r\n/") {
		return errx.Validation("invalid_domain", "A valid domain (or *.domain) is required.",
			errx.Field{Field: "domain", Code: "invalid", Message: "invalid domain"})
	}
	return nil
}

// List returns certificates (ownerID 0 = all).
func (s *Service) List(ctx context.Context, ownerID int64, limit, offset int) ([]Cert, error) {
	recs, err := s.repo.List(ctx, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]Cert, len(recs))
	for i := range recs {
		out[i] = *certView(&recs[i])
	}
	return out, nil
}

// installAndRecord installs the cert via the broker and persists the record.
// webroot records how the cert was obtained so the renewer can repeat it (empty
// => DNS-01). Insert upserts by common_name, so a renewal replaces the row.
func (s *Service) installAndRecord(ctx context.Context, ownerID int64, domain, provider, certPEM, keyPEM string, notAfter time.Time, webroot string) (*Cert, error) {
	// A wildcard is not a legal hostname, so it is installed under its base
	// domain's directory (the broker validates the name it is given).
	isWildcard := strings.HasPrefix(domain, "*.")
	installName := strings.TrimPrefix(domain, "*.")
	if _, err := s.broker.Invoke(ctx, "cert.install", map[string]any{
		"domain": installName, "cert_pem": certPEM, "key_pem": keyPEM,
	}); err != nil {
		return nil, err
	}
	sans, _ := json.Marshal([]string{domain})
	rec := &Record{
		OwnerID: ownerID, Provider: provider, CommonName: domain, SANs: sans,
		CertPEM: certPEM, PrivkeyEnc: []byte(keyPEM),
		IssuedAt: fmtTime(time.Now()), ExpiresAt: fmtTime(notAfter),
		AutoRenew: 1, Status: "valid", Webroot: webroot,
	}
	if isWildcard {
		rec.IsWildcard = 1
	}
	if err := s.repo.Insert(ctx, rec); err != nil {
		return nil, err
	}
	return certView(rec), nil
}

func certView(r *Record) *Cert {
	var sans []string
	_ = json.Unmarshal(r.SANs, &sans)
	return &Cert{
		UID: r.UID, Provider: r.Provider, CommonName: r.CommonName, SANs: sans,
		Status: r.Status, IssuedAt: r.IssuedAt, ExpiresAt: r.ExpiresAt,
		AutoRenew: r.AutoRenew == 1, CreatedAt: r.CreatedAt,
	}
}

func normalizeDomain(d string) string { return strings.ToLower(strings.TrimSpace(d)) }

func fmtTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05") }

// generateSelfSigned creates an ECDSA self-signed certificate for a domain.
func generateSelfSigned(domain string) (certPEM, keyPEM string, notAfter time.Time, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", time.Time{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", time.Time{}, err
	}
	notBefore := time.Now().Add(-time.Hour)
	notAfter = time.Now().Add(825 * 24 * time.Hour)
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: domain},
		DNSNames:              []string{domain},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", time.Time{}, err
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", time.Time{}, err
	}
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	return certPEM, keyPEM, notAfter, nil
}
