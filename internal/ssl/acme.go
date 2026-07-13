package ssl

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"time"

	"golang.org/x/crypto/acme"
)

// LetsEncryptDirectory is the production ACME directory. Use the staging
// directory (https://acme-staging-v02.api.letsencrypt.org/directory) while
// testing to avoid rate limits.
const LetsEncryptDirectory = "https://acme-v02.api.letsencrypt.org/directory"

// LetsEncrypt is a real ACME (RFC 8555) issuer using HTTP-01 challenges.
//
// NOTE: issuance requires a reachable ACME server and a publicly-resolvable
// domain served by this host, so it cannot run in unit tests; the Service is
// tested against a fake ACME. The account key is generated per instance for now
// — persisting it in acme_accounts (for rate-limit-friendly reuse) is a planned
// follow-up.
type LetsEncrypt struct {
	dirURL     string
	email      string
	accountKey crypto.Signer
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

// Issue implements ssl.ACME.
func (le *LetsEncrypt) Issue(ctx context.Context, domain string, writeChallenge func(token, keyAuth string) error) (string, string, time.Time, error) {
	client := &acme.Client{Key: le.accountKey, DirectoryURL: le.dirURL}

	// Register the account (idempotent: an existing account is fine).
	acct := &acme.Account{}
	if le.email != "" {
		acct.Contact = []string{"mailto:" + le.email}
	}
	if _, err := client.Register(ctx, acct, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return "", "", time.Time{}, err
	}

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return "", "", time.Time{}, err
	}

	// Satisfy each authorization via HTTP-01.
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
			if ch.Type == "http-01" {
				chal = ch
				break
			}
		}
		if chal == nil {
			return "", "", time.Time{}, errors.New("acme: no http-01 challenge offered")
		}
		keyAuth, err := client.HTTP01ChallengeResponse(chal.Token)
		if err != nil {
			return "", "", time.Time{}, err
		}
		if err := writeChallenge(chal.Token, keyAuth); err != nil {
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
