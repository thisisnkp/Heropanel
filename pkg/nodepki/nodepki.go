// Package nodepki issues the certificates that identify NexPanel nodes to each
// other. It is the multi-node counterpart to the Unix socket's SO_PEERCRED
// check: on one host the kernel attests who is calling, and across hosts nothing
// does, so a certificate has to.
//
// Scope is deliberately small. This is not a general CA — there is no CRL, no
// OCSP, no intermediate tier and no renewal daemon. It mints a self-signed root
// for one panel installation and issues short-lived leaf certificates from it,
// which is the whole of what the transport in [broker/transport] verifies. A
// deployment that already runs a real PKI can ignore this package entirely and
// hand the transport its own CA and leaves; nothing here is on that path.
//
// Keys are Ed25519, matching the release-signing and module-signing chains
// (pkg/edkey) so the installation has one key algorithm rather than three. Go's
// TLS stack supports Ed25519 leaves from TLS 1.3, and both ends of this
// transport are built from this repository, so there is no interop tier to
// accommodate.
//
// Everything here is standard library. Adding a PKI dependency to reach the root
// broker would undo the reason ADR-0007 hand-rolled the framing in the first
// place.
package nodepki

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"
)

// Pair is a PEM-encoded certificate and its private key.
type Pair struct {
	CertPEM []byte
	KeyPEM  []byte
}

// CA is a self-signed root that issues node certificates.
type CA struct {
	Cert    *x509.Certificate
	Key     ed25519.PrivateKey
	CertPEM []byte
	KeyPEM  []byte
}

// Options describes a leaf certificate to issue.
type Options struct {
	// CommonName is the node's identity. The transport authorizes on this exact
	// string, so it is the name an operator puts in the allowlist.
	CommonName string
	// TTL bounds the certificate's life. Short is the point: this package has no
	// revocation, so expiry is the only way a certificate stops being valid.
	TTL time.Duration
	// Server marks the leaf usable by a listener, Client by a dialer. A node that
	// both serves and dials sets both.
	Server bool
	Client bool
	// DNSNames and IPs are the server-side SANs. Go verifies a server's identity
	// against SANs and has ignored CommonName for that purpose since 1.15, so a
	// server certificate without one of these cannot be verified by any client.
	DNSNames []string
	IPs      []net.IP
}

// DefaultCATTL is how long a generated root lives. Long enough that rotating it
// is a planned action rather than an outage, short enough that it is not
// forever.
const DefaultCATTL = 10 * 365 * 24 * time.Hour

// DefaultLeafTTL is the default life of a node certificate.
const DefaultLeafTTL = 90 * 24 * time.Hour

// NewCA generates a self-signed Ed25519 root.
func NewCA(commonName string, ttl time.Duration) (*CA, error) {
	if commonName == "" {
		return nil, errors.New("nodepki: CA common name is required")
	}
	if ttl <= 0 {
		ttl = DefaultCATTL
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("nodepki: generate CA key: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(ttl),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		// A root that can sign intermediates could sign a second CA that this
		// installation never authorized. One tier is all the transport needs.
		MaxPathLen:     0,
		MaxPathLenZero: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
	if err != nil {
		return nil, fmt.Errorf("nodepki: self-sign CA: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("nodepki: parse CA: %w", err)
	}
	keyPEM, err := marshalKey(key)
	if err != nil {
		return nil, err
	}
	return &CA{
		Cert:    cert,
		Key:     key,
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  keyPEM,
	}, nil
}

// LoadCA reconstructs a CA from its PEM certificate and key.
func LoadCA(certPEM, keyPEM []byte) (*CA, error) {
	cert, err := ParseCertificate(certPEM)
	if err != nil {
		return nil, err
	}
	if !cert.IsCA {
		return nil, errors.New("nodepki: certificate is not a CA")
	}
	blk, _ := pem.Decode(keyPEM)
	if blk == nil {
		return nil, errors.New("nodepki: key is not PEM")
	}
	any, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("nodepki: parse CA key: %w", err)
	}
	key, ok := any.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("nodepki: CA key is not Ed25519")
	}
	return &CA{Cert: cert, Key: key, CertPEM: certPEM, KeyPEM: keyPEM}, nil
}

// Issue mints a leaf certificate signed by the CA.
func (c *CA) Issue(o Options) (Pair, error) {
	if o.CommonName == "" {
		return Pair{}, errors.New("nodepki: common name is required")
	}
	if !o.Server && !o.Client {
		return Pair{}, errors.New("nodepki: certificate must be usable for server, client, or both")
	}
	if o.Server && len(o.DNSNames) == 0 && len(o.IPs) == 0 {
		// Failing here beats issuing a certificate that every client will reject
		// with an opaque handshake error at 3am.
		return Pair{}, errors.New("nodepki: a server certificate needs at least one DNS name or IP")
	}
	if o.TTL <= 0 {
		o.TTL = DefaultLeafTTL
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Pair{}, fmt.Errorf("nodepki: generate leaf key: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return Pair{}, err
	}
	var eku []x509.ExtKeyUsage
	if o.Server {
		eku = append(eku, x509.ExtKeyUsageServerAuth)
	}
	if o.Client {
		eku = append(eku, x509.ExtKeyUsageClientAuth)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: o.CommonName},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(o.TTL),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           eku,
		BasicConstraintsValid: true,
		DNSNames:              o.DNSNames,
		IPAddresses:           o.IPs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.Cert, pub, c.Key)
	if err != nil {
		return Pair{}, fmt.Errorf("nodepki: sign leaf: %w", err)
	}
	keyPEM, err := marshalKey(key)
	if err != nil {
		return Pair{}, err
	}
	return Pair{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  keyPEM,
	}, nil
}

// ParseCertificate decodes a single PEM certificate.
func ParseCertificate(certPEM []byte) (*x509.Certificate, error) {
	blk, _ := pem.Decode(certPEM)
	if blk == nil || blk.Type != "CERTIFICATE" {
		return nil, errors.New("nodepki: not a PEM certificate")
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("nodepki: parse certificate: %w", err)
	}
	return cert, nil
}

// CertPool builds a verification pool from one or more PEM certificates.
func CertPool(pemBytes []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, errors.New("nodepki: no certificates found in PEM")
	}
	return pool, nil
}

// clockSkew backdates NotBefore so a node whose clock is a little behind the
// issuer does not reject a certificate that was just minted for it.
const clockSkew = 5 * time.Minute

func newSerial() (*big.Int, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("nodepki: serial: %w", err)
	}
	return n, nil
}

func marshalKey(key ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("nodepki: marshal key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}
