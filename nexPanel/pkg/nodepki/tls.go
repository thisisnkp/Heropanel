package nodepki

import (
	"crypto/tls"
	"errors"
	"fmt"
)

// The TLS configurations live here rather than beside the broker's transport for
// a structural reason: npd needs the client half, and npd must not link the root
// broker's capability registry to get it. ADR-0007 separated those two binaries
// on purpose, and a shared helper is not worth blurring the boundary — so the
// configs sit in the package that already owns the key material, and depend on
// nothing but the standard library.

// ServerTLS builds the listener configuration for a remote broker endpoint.
//
// Client certificates are required and verified — not requested, and not
// verified only when offered. tls.RequireAndVerifyClientCert is the difference
// between an endpoint that authenticates its callers and one that merely
// encrypts to anybody who connects.
func ServerTLS(certPEM, keyPEM, clientCAPEM []byte) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("nodepki: server keypair: %w", err)
	}
	pool, err := CertPool(clientCAPEM)
	if err != nil {
		return nil, fmt.Errorf("nodepki: client CA: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		// Both ends of this connection are built from this repository, so there
		// is no legacy peer to accommodate. TLS 1.3 removes the downgrade and
		// renegotiation surface that a lower floor would keep reachable on the
		// one component running as root.
		MinVersion: tls.VersionTLS13,
	}, nil
}

// ClientTLS builds the dialer configuration npd uses to reach a remote broker.
//
// serverName must match a SAN on the broker's certificate. It is not cosmetic:
// without it Go cannot verify it reached the broker it meant to reach, and
// mutual TLS that authenticates only one direction is not mutual.
func ClientTLS(certPEM, keyPEM, caPEM []byte, serverName string) (*tls.Config, error) {
	if serverName == "" {
		return nil, errors.New("nodepki: server name is required to verify the broker's certificate")
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("nodepki: client keypair: %w", err)
	}
	pool, err := CertPool(caPEM)
	if err != nil {
		return nil, fmt.Errorf("nodepki: server CA: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
	}, nil
}
