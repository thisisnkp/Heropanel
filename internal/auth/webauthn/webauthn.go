// Package webauthn is a minimal, dependency-free WebAuthn (FIDO2) verifier:
// enough of the ceremony to register a passkey and verify an assertion, using
// only the standard library's crypto and a small CBOR reader (cbor.go).
//
// Scope, stated honestly: we verify the *assertion signature* — the thing that
// actually authenticates a login — and the registration data that yields the
// public key, the RP-ID binding, user presence, and signature-counter clone
// detection. We do NOT verify attestation statements (the authenticator's
// vendor provenance): the panel needs a key it can trust for future
// assertions, not proof of which manufacturer made the token, and skipping
// attestation is a common, deliberate posture (it also means any passkey works,
// including platform authenticators that self-attest). ES256 and RS256 cover
// essentially every authenticator in use.
package webauthn

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
)

// Errors are intentionally coarse — a caller turns them into a generic
// "authentication failed", never leaking which check tripped.
var (
	ErrVerify = errors.New("webauthn: verification failed")
	ErrParse  = errors.New("webauthn: malformed authenticator data")
)

// COSE algorithm identifiers we accept.
const (
	algES256 = -7
	algRS256 = -257
)

// authenticatorData flag bits.
const (
	flagUP = 1 << 0 // user present
	flagAT = 1 << 6 // attested credential data included
)

// Config binds a verifier to a relying party.
type Config struct {
	RPID   string // e.g. "panel.example.com"
	RPName string // display name
	Origin string // e.g. "https://panel.example.com"
}

// Verifier runs the ceremonies for one relying party.
type Verifier struct{ cfg Config }

// New constructs a Verifier.
func New(cfg Config) *Verifier { return &Verifier{cfg: cfg} }

// Credential is a registered passkey the caller persists.
type Credential struct {
	ID        []byte // raw credential id
	COSEKey   []byte // the credentialPublicKey, as stored in authData (COSE)
	SignCount uint32
}

// clientData is the parsed clientDataJSON.
type clientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"` // base64url, no padding
	Origin    string `json:"origin"`
}

var b64 = base64.RawURLEncoding

// FinishRegistration verifies a create() response against the issued challenge
// and returns the credential to store. clientDataJSON and attestationObject are
// the raw (base64url-decoded) bytes from the browser.
func (v *Verifier) FinishRegistration(challenge, clientDataJSON, attestationObject []byte) (*Credential, error) {
	if err := v.checkClientData(clientDataJSON, "webauthn.create", challenge); err != nil {
		return nil, err
	}
	// attestationObject = CBOR map { fmt, attStmt, authData }. We read authData
	// and ignore attStmt (attestation is not verified — see the package doc).
	obj, _, err := cborDecode(attestationObject)
	if err != nil {
		return nil, ErrParse
	}
	m, ok := obj.(map[any]any)
	if !ok {
		return nil, ErrParse
	}
	authData, ok := m["authData"].([]byte)
	if !ok {
		return nil, ErrParse
	}
	ad, err := parseAuthData(authData, true)
	if err != nil {
		return nil, err
	}
	if !v.rpMatches(ad.rpIDHash) || ad.flags&flagUP == 0 || ad.flags&flagAT == 0 {
		return nil, ErrVerify
	}
	if _, err := parseCOSEKey(ad.coseKey); err != nil {
		return nil, err // reject a key we could never verify assertions against
	}
	return &Credential{ID: ad.credID, COSEKey: ad.coseKey, SignCount: ad.signCount}, nil
}

// FinishAssertion verifies a get() response against a stored credential and the
// issued challenge, returning the new signature counter to persist.
func (v *Verifier) FinishAssertion(cred *Credential, challenge, clientDataJSON, authenticatorData, signature []byte) (uint32, error) {
	if err := v.checkClientData(clientDataJSON, "webauthn.get", challenge); err != nil {
		return 0, err
	}
	ad, err := parseAuthData(authenticatorData, false)
	if err != nil {
		return 0, err
	}
	if !v.rpMatches(ad.rpIDHash) || ad.flags&flagUP == 0 {
		return 0, ErrVerify
	}
	pub, err := parseCOSEKey(cred.COSEKey)
	if err != nil {
		return 0, err
	}
	// The signed message is authenticatorData || SHA256(clientDataJSON).
	cdh := sha256.Sum256(clientDataJSON)
	signed := append(append([]byte(nil), authenticatorData...), cdh[:]...)
	digest := sha256.Sum256(signed)
	if !verifySignature(pub, digest[:], signature) {
		return 0, ErrVerify
	}
	// Clone detection: a counter that goes backwards (when either side is
	// non-zero) means two authenticators share a credential. Authenticators
	// that never count report 0 on both sides, which is allowed.
	if !(ad.signCount == 0 && cred.SignCount == 0) && ad.signCount <= cred.SignCount {
		return 0, ErrVerify
	}
	return ad.signCount, nil
}

// checkClientData validates the type, challenge and origin of a clientDataJSON.
func (v *Verifier) checkClientData(clientDataJSON []byte, wantType string, challenge []byte) error {
	var cd clientData
	if err := json.Unmarshal(clientDataJSON, &cd); err != nil {
		return ErrParse
	}
	if cd.Type != wantType {
		return ErrVerify
	}
	if subtle.ConstantTimeCompare([]byte(cd.Challenge), []byte(b64.EncodeToString(challenge))) != 1 {
		return ErrVerify
	}
	if cd.Origin != v.cfg.Origin {
		return ErrVerify
	}
	return nil
}

func (v *Verifier) rpMatches(rpIDHash []byte) bool {
	want := sha256.Sum256([]byte(v.cfg.RPID))
	return subtle.ConstantTimeCompare(rpIDHash, want[:]) == 1
}

// parsedAuthData is the decoded authenticator data.
type parsedAuthData struct {
	rpIDHash  []byte
	flags     byte
	signCount uint32
	credID    []byte
	coseKey   []byte
}

// parseAuthData decodes authenticator data. withCred requires the attested
// credential data block (registration); assertions carry only the header.
func parseAuthData(b []byte, withCred bool) (*parsedAuthData, error) {
	if len(b) < 37 {
		return nil, ErrParse
	}
	ad := &parsedAuthData{
		rpIDHash:  b[:32],
		flags:     b[32],
		signCount: binary.BigEndian.Uint32(b[33:37]),
	}
	if !withCred {
		return ad, nil
	}
	if ad.flags&flagAT == 0 || len(b) < 55 {
		return nil, ErrParse
	}
	// attestedCredentialData: aaguid(16) credIdLen(2) credId(L) coseKey(rest).
	rest := b[37:]
	credLen := int(binary.BigEndian.Uint16(rest[16:18]))
	rest = rest[18:]
	if len(rest) < credLen {
		return nil, ErrParse
	}
	ad.credID = rest[:credLen]
	// The COSE key is the first CBOR item of the remainder; decode-and-reslice so
	// any trailing extensions do not become part of the stored key.
	coseBytes := rest[credLen:]
	if _, remain, err := cborDecode(coseBytes); err != nil {
		return nil, ErrParse
	} else {
		ad.coseKey = coseBytes[:len(coseBytes)-len(remain)]
	}
	return ad, nil
}

// parseCOSEKey turns a COSE_Key into a crypto.PublicKey (EC2/ES256 or RSA/RS256).
func parseCOSEKey(b []byte) (crypto.PublicKey, error) {
	v, _, err := cborDecode(b)
	if err != nil {
		return nil, ErrParse
	}
	m, ok := v.(map[any]any)
	if !ok {
		return nil, ErrParse
	}
	kty, _ := m[int64(1)].(uint64)
	alg, _ := m[int64(3)].(int64)
	switch kty {
	case 2: // EC2
		if alg != algES256 {
			return nil, ErrVerify
		}
		x, xok := m[int64(-2)].([]byte)
		y, yok := m[int64(-3)].([]byte)
		if !xok || !yok || len(x) != 32 || len(y) != 32 {
			return nil, ErrParse
		}
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
			return nil, ErrVerify
		}
		return pub, nil
	case 3: // RSA
		if alg != algRS256 {
			return nil, ErrVerify
		}
		n, nok := m[int64(-1)].([]byte)
		e, eok := m[int64(-2)].([]byte)
		if !nok || !eok || len(n) == 0 || len(e) == 0 {
			return nil, ErrParse
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}, nil
	}
	return nil, ErrVerify
}

// verifySignature checks sig over digest with an ES256 or RS256 public key.
func verifySignature(pub crypto.PublicKey, digest, sig []byte) bool {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		return ecdsa.VerifyASN1(k, digest, sig)
	case *rsa.PublicKey:
		return rsa.VerifyPKCS1v15(k, crypto.SHA256, digest, sig) == nil
	}
	return false
}
