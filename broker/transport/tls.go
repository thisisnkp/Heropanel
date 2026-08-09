package transport

import (
	"crypto/tls"
	"errors"
	"net"
)

// This file is the multi-node half of the broker's transport. The framing is
// unchanged from ADR-0007 — the same length-prefixed JSON, the same request and
// response types. Only what proves *who is calling* changes, because that proof
// is the one thing a Unix socket supplies for free and a network cannot.
//
// On a socket the kernel answers it: SO_PEERCRED reports the peer's uid and no
// process can lie about its own. Across hosts there is no such authority, so the
// peer's certificate becomes the identity — and it has to be verified as
// rigorously as the kernel would have.
//
// The tls.Config builders themselves live in [pkg/nodepki], because npd needs
// the client half and must not link this package to get it.

// peerNode returns the verified identity of a TLS peer.
//
// It reads only VerifiedChains — the chains the TLS stack itself built and
// checked against ClientCAs. PeerCertificates alone would be whatever the peer
// sent, which is attacker-chosen input; trusting the Subject out of it is how
// mTLS gets implemented wrongly.
func peerNode(conn net.Conn) (string, error) {
	tc, ok := conn.(*tls.Conn)
	if !ok {
		return "", errors.New("connection is not TLS")
	}
	st := tc.ConnectionState()
	if !st.HandshakeComplete {
		return "", errors.New("TLS handshake not complete")
	}
	if len(st.VerifiedChains) == 0 || len(st.VerifiedChains[0]) == 0 {
		return "", errors.New("no verified client certificate")
	}
	name := st.VerifiedChains[0][0].Subject.CommonName
	if name == "" {
		return "", errors.New("client certificate has no common name")
	}
	return name, nil
}
