package nodepki_test

import (
	"crypto/x509"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/thisisnkp/nexpanel/pkg/nodepki"
)

func TestIssuedLeafChainsToItsCA(t *testing.T) {
	ca, err := nodepki.NewCA("test ca", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	pair, err := ca.Issue(nodepki.Options{CommonName: "node-b", Client: true})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	leaf, err := nodepki.ParseCertificate(pair.CertPEM)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	pool, err := nodepki.CertPool(ca.CertPEM)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("leaf did not verify against its own CA: %v", err)
	}
	if leaf.Subject.CommonName != "node-b" {
		t.Errorf("common name = %q, want node-b", leaf.Subject.CommonName)
	}
}

// The identity a node presents is only meaningful if a leaf from a different
// root cannot satisfy this one.
func TestLeafDoesNotVerifyAgainstAnotherCA(t *testing.T) {
	a, err := nodepki.NewCA("ca a", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	b, err := nodepki.NewCA("ca b", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	pair, err := a.Issue(nodepki.Options{CommonName: "node-b", Client: true})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	leaf, err := nodepki.ParseCertificate(pair.CertPEM)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pool, err := nodepki.CertPool(b.CertPEM)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool}); err == nil {
		t.Fatal("a leaf verified against a CA that did not sign it")
	}
}

// A leaf must not be able to sign further certificates: one tier is all the
// transport authorizes, and a leaf that could mint identities would let any
// compromised node manufacture every other node.
func TestLeafIsNotACA(t *testing.T) {
	ca, err := nodepki.NewCA("test ca", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	pair, err := ca.Issue(nodepki.Options{
		CommonName: "np-broker", Server: true, DNSNames: []string{"np-broker"},
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	leaf, err := nodepki.ParseCertificate(pair.CertPEM)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if leaf.IsCA {
		t.Error("leaf certificate is marked as a CA")
	}
	if leaf.KeyUsage&x509.KeyUsageCertSign != 0 {
		t.Error("leaf certificate may sign certificates")
	}
}

// Go verifies a server by its SANs, so issuing a server certificate without one
// produces something no client can ever accept. Failing at issue time is the
// only place that mistake is cheap to see.
func TestServerCertificateRequiresSAN(t *testing.T) {
	ca, err := nodepki.NewCA("test ca", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	if _, err := ca.Issue(nodepki.Options{CommonName: "np-broker", Server: true}); err == nil {
		t.Fatal("issued a server certificate with no DNS name or IP")
	}
}

func TestIssueRejectsIncompleteOptions(t *testing.T) {
	ca, err := nodepki.NewCA("test ca", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	for name, o := range map[string]nodepki.Options{
		"no common name": {Client: true},
		"no usage":       {CommonName: "node-b"},
	} {
		if _, err := ca.Issue(o); err == nil {
			t.Errorf("%s: Issue succeeded, want refusal", name)
		}
	}
}

func TestNewCARequiresCommonName(t *testing.T) {
	if _, err := nodepki.NewCA("", 0); err == nil {
		t.Fatal("NewCA accepted an empty common name")
	}
}

// A round trip through PEM is how the CA reaches the next process; losing the
// key or the CA bit there would only show up as a handshake failure much later.
func TestLoadCARoundTrip(t *testing.T) {
	ca, err := nodepki.NewCA("test ca", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	loaded, err := nodepki.LoadCA(ca.CertPEM, ca.KeyPEM)
	if err != nil {
		t.Fatalf("load ca: %v", err)
	}
	pair, err := loaded.Issue(nodepki.Options{CommonName: "node-b", Client: true})
	if err != nil {
		t.Fatalf("issue from loaded ca: %v", err)
	}
	leaf, err := nodepki.ParseCertificate(pair.CertPEM)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pool, err := nodepki.CertPool(ca.CertPEM)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("leaf from reloaded CA did not verify: %v", err)
	}
}

func TestLoadCARejectsALeaf(t *testing.T) {
	ca, err := nodepki.NewCA("test ca", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	pair, err := ca.Issue(nodepki.Options{CommonName: "node-b", Client: true})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := nodepki.LoadCA(pair.CertPEM, pair.KeyPEM); err == nil {
		t.Fatal("LoadCA accepted a leaf certificate as a CA")
	}
}

// NotBefore is backdated so a node whose clock trails the issuer's does not
// reject a certificate minted seconds ago.
func TestLeafToleratesSmallClockSkew(t *testing.T) {
	ca, err := nodepki.NewCA("test ca", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	pair, err := ca.Issue(nodepki.Options{
		CommonName: "np-broker", Server: true, IPs: []net.IP{net.ParseIP("127.0.0.1")},
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	leaf, err := nodepki.ParseCertificate(pair.CertPEM)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !leaf.NotBefore.Before(time.Now()) {
		t.Errorf("NotBefore = %v, want it backdated before now", leaf.NotBefore)
	}
}

func TestParseCertificateRejectsGarbage(t *testing.T) {
	if _, err := nodepki.ParseCertificate([]byte("definitely not pem")); err == nil {
		t.Fatal("ParseCertificate accepted non-PEM input")
	}
	if _, err := nodepki.CertPool([]byte("definitely not pem")); err == nil {
		t.Fatal("CertPool accepted non-PEM input")
	}
}

func TestDefaultTTLApplies(t *testing.T) {
	ca, err := nodepki.NewCA("test ca", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	if !ca.Cert.NotAfter.After(time.Now().Add(nodepki.DefaultCATTL - 24*time.Hour)) {
		t.Errorf("CA NotAfter = %v, want ~DefaultCATTL out", ca.Cert.NotAfter)
	}
	pair, err := ca.Issue(nodepki.Options{CommonName: "node-b", Client: true})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	leaf, err := nodepki.ParseCertificate(pair.CertPEM)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !leaf.NotAfter.After(time.Now().Add(nodepki.DefaultLeafTTL - 24*time.Hour)) {
		t.Errorf("leaf NotAfter = %v, want ~DefaultLeafTTL out", leaf.NotAfter)
	}
	if strings.TrimSpace(string(pair.KeyPEM)) == "" {
		t.Error("issued pair has an empty key")
	}
}
