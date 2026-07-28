package ssl

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
)

// LetsEncryptDirectory is the production ACME directory. Use the staging
// directory (https://acme-staging-v02.api.letsencrypt.org/directory) while
// testing to avoid rate limits.
const LetsEncryptDirectory = "https://acme-v02.api.letsencrypt.org/directory"

// ZeroSSLDirectory is ZeroSSL's ACME directory. ZeroSSL is a standard RFC 8555
// CA that additionally requires **External Account Binding** — the operator gets
// an EAB key id + HMAC key from their ZeroSSL dashboard and the account is bound
// to it at registration. Everything after that is identical ACME, so the same
// order/challenge flow issues its certificates.
const ZeroSSLDirectory = "https://acme.zerossl.com/v2/DV90"

// LetsEncrypt is a real ACME (RFC 8555) issuer using HTTP-01 challenges.
//
// NOTE: issuance requires a reachable ACME server and a publicly-resolvable
// domain served by this host, so it cannot run in unit tests; the Service is
// tested against a fake ACME. The account key is generated per instance for now
// — persisting it in acme_accounts (for rate-limit-friendly reuse) is a planned
// follow-up.
type LetsEncrypt struct {
	dirURL      string
	email       string
	accountKey  crypto.Signer
	propagation time.Duration // DNS-01 wait; 0 => DefaultPropagationDelay
	// eab, when set, binds account registration to an external account (ZeroSSL
	// and other EAB-requiring CAs). nil for Let's Encrypt.
	eab *acme.ExternalAccountBinding
}

// NewLetsEncrypt constructs a Let's Encrypt issuer for the given directory URL
// (empty => production) and contact email.
func NewLetsEncrypt(dirURL, email string) (*LetsEncrypt, error) {
	if dirURL == "" {
		dirURL = LetsEncryptDirectory
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &LetsEncrypt{dirURL: dirURL, email: email, accountKey: key}, nil
}

// NewACMEIssuer constructs a generic RFC 8555 issuer, optionally with External
// Account Binding. The HMAC key is the base64url (or standard base64) ZeroSSL
// gives on its dashboard. When eabKID is empty it behaves like a plain ACME CA.
func NewACMEIssuer(dirURL, email, eabKID, eabHMAC string) (*LetsEncrypt, error) {
	if dirURL == "" {
		return nil, errors.New("acme: a directory URL is required")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	le := &LetsEncrypt{dirURL: dirURL, email: email, accountKey: key}
	if eabKID != "" {
		hmacKey, err := decodeEABKey(eabHMAC)
		if err != nil {
			return nil, err
		}
		le.eab = &acme.ExternalAccountBinding{KID: eabKID, Key: hmacKey}
	}
	return le, nil
}

// decodeEABKey accepts the EAB HMAC in base64url (ZeroSSL's form) or standard
// base64, returning the raw key bytes.
func decodeEABKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return nil, errors.New("acme: EAB HMAC key is not valid base64")
}

// DefaultPropagationDelay is how long to wait after publishing a DNS-01 record
// before telling the CA to check it, giving the authoritative server time to
// serve the new TXT.
const DefaultPropagationDelay = 10 * time.Second

// PropagationDelay overrides the DNS-01 wait (tests set it to zero).
func (le *LetsEncrypt) PropagationDelay(d time.Duration) { le.propagation = d }

// Issue implements ssl.ACME using an HTTP-01 challenge.
func (le *LetsEncrypt) Issue(ctx context.Context, domain string, writeChallenge func(token, keyAuth string) error) (string, string, time.Time, error) {
	solve := func(client *acme.Client, chal *acme.Challenge, _ string) error {
		keyAuth, err := client.HTTP01ChallengeResponse(chal.Token)
		if err != nil {
			return err
		}
		return writeChallenge(chal.Token, keyAuth)
	}
	return le.issue(ctx, domain, "http-01", solve, nil)
}

// IssueDNS01 implements ssl.ACMEDNS. It publishes the challenge as a TXT record
// at _acme-challenge.<domain> through setTXT, and removes it afterwards. This is
// the only challenge type that can issue a **wildcard** certificate.
func (le *LetsEncrypt) IssueDNS01(ctx context.Context, domain string,
	setTXT func(fqdn, value string) error, deleteTXT func(fqdn string) error) (string, string, time.Time, error) {
	var published []string
	solve := func(client *acme.Client, chal *acme.Challenge, authzDomain string) error {
		val, err := client.DNS01ChallengeRecord(chal.Token)
		if err != nil {
			return err
		}
		// For a wildcard order the CA reports the base domain, so the record name
		// is the same for "example.com" and "*.example.com".
		fqdn := "_acme-challenge." + strings.TrimPrefix(authzDomain, "*.")
		if err := setTXT(fqdn, val); err != nil {
			return err
		}
		published = append(published, fqdn)
		delay := le.propagation
		if delay == 0 {
			delay = DefaultPropagationDelay
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}
	cleanup := func() {
		if deleteTXT == nil {
			return
		}
		for _, f := range published {
			_ = deleteTXT(f)
		}
	}
	return le.issue(ctx, domain, "dns-01", solve, cleanup)
}

// issue runs the ACME v2 order flow, satisfying every authorization with the
// given challenge type and solver. The order flow (rather than the legacy
// pre-authorization) is what makes wildcard issuance possible.
func (le *LetsEncrypt) issue(ctx context.Context, domain, chalType string,
	solve func(client *acme.Client, chal *acme.Challenge, authzDomain string) error,
	cleanup func()) (string, string, time.Time, error) {
	client := &acme.Client{Key: le.accountKey, DirectoryURL: le.dirURL}
	if cleanup != nil {
		defer cleanup()
	}

	// Register the account (idempotent: an existing account is fine).
	acct := &acme.Account{}
	if le.email != "" {
		acct.Contact = []string{"mailto:" + le.email}
	}
	// ZeroSSL and other EAB CAs bind the new account to an external account at
	// registration; Let's Encrypt leaves this nil.
	if le.eab != nil {
		acct.ExternalAccountBinding = le.eab
	}
	if _, err := client.Register(ctx, acct, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return "", "", time.Time{}, err
	}

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return "", "", time.Time{}, err
	}

	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return "", "", time.Time{}, err
		}
		if authz.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, ch := range authz.Challenges {
			if ch.Type == chalType {
				chal = ch
				break
			}
		}
		if chal == nil {
			return "", "", time.Time{}, errors.New("acme: no " + chalType + " challenge offered")
		}
		if err := solve(client, chal, authz.Identifier.Value); err != nil {
			return "", "", time.Time{}, err
		}
		if _, err := client.Accept(ctx, chal); err != nil {
			return "", "", time.Time{}, err
		}
		if _, err := client.WaitAuthorization(ctx, authz.URI); err != nil {
			return "", "", time.Time{}, err
		}
	}

	// Generate the certificate key + CSR and finalize the order.
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", time.Time{}, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: []string{domain}}, certKey)
	if err != nil {
		return "", "", time.Time{}, err
	}
	der, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return "", "", time.Time{}, err
	}

	var certPEM []byte
	for _, b := range der {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: b})...)
	}
	keyDER, err := x509.MarshalECPrivateKey(certKey)
	if err != nil {
		return "", "", time.Time{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	notAfter := time.Now().Add(90 * 24 * time.Hour)
	if leaf, err := x509.ParseCertificate(der[0]); err == nil {
		notAfter = leaf.NotAfter
	}
	return string(certPEM), string(keyPEM), notAfter, nil
}
