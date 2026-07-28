package webauthn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"testing"
)

// A virtual authenticator: an ES256 key that produces real attestation and
// assertion material, so the verifier is exercised against genuine signatures
// (and genuinely-wrong ones) with no browser or hardware token. This is the
// "live proof" for WebAuthn — it is pure crypto, so a Go integration test is
// the honest end-to-end, not a container.
type vauth struct {
	priv   *ecdsa.PrivateKey
	credID []byte
	rpID   string
	origin string
}

func newVauth(t *testing.T, rpID, origin string) *vauth {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &vauth{priv: priv, credID: id, rpID: rpID, origin: origin}
}

func (a *vauth) coseKey() []byte {
	x := pad32(a.priv.PublicKey.X.Bytes())
	y := pad32(a.priv.PublicKey.Y.Bytes())
	// COSE_Key: {1:2(EC2), 3:-7(ES256), -1:1(P256), -2:x, -3:y}
	m := append(encHead(5, 5), encUint(1)...)
	m = append(m, encUint(2)...)
	m = append(m, encUint(3)...)
	m = append(m, encNint(-7)...)
	m = append(m, encNint(-1)...)
	m = append(m, encUint(1)...)
	m = append(m, encNint(-2)...)
	m = append(m, encBytes(x)...)
	m = append(m, encNint(-3)...)
	m = append(m, encBytes(y)...)
	return m
}

func (a *vauth) authData(flags byte, signCount uint32, withCred bool) []byte {
	h := sha256.Sum256([]byte(a.rpID))
	b := append([]byte(nil), h[:]...)
	b = append(b, flags)
	var sc [4]byte
	binary.BigEndian.PutUint32(sc[:], signCount)
	b = append(b, sc[:]...)
	if withCred {
		b = append(b, make([]byte, 16)...) // aaguid
		var cl [2]byte
		binary.BigEndian.PutUint16(cl[:], uint16(len(a.credID)))
		b = append(b, cl[:]...)
		b = append(b, a.credID...)
		b = append(b, a.coseKey()...)
	}
	return b
}

func (a *vauth) attestationObject(signCount uint32) []byte {
	authData := a.authData(flagUP|flagAT, signCount, true)
	// {"fmt":"none","attStmt":{},"authData": authData}
	m := encHead(5, 3)
	m = append(m, encText("fmt")...)
	m = append(m, encText("none")...)
	m = append(m, encText("attStmt")...)
	m = append(m, encHead(5, 0)...)
	m = append(m, encText("authData")...)
	m = append(m, encBytes(authData)...)
	return m
}

func (a *vauth) clientData(t *testing.T, typ string, challenge []byte) []byte {
	t.Helper()
	b, _ := json.Marshal(clientData{Type: typ, Challenge: b64.EncodeToString(challenge), Origin: a.origin})
	return b
}

// sign produces an assertion (authenticatorData + signature) over a challenge.
func (a *vauth) sign(t *testing.T, challenge []byte, signCount uint32) (authData, clientDataJSON, sig []byte) {
	t.Helper()
	authData = a.authData(flagUP, signCount, false)
	clientDataJSON = a.clientData(t, "webauthn.get", challenge)
	cdh := sha256.Sum256(clientDataJSON)
	signed := append(append([]byte(nil), authData...), cdh[:]...)
	digest := sha256.Sum256(signed)
	s, err := ecdsa.SignASN1(rand.Reader, a.priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return authData, clientDataJSON, s
}

const (
	testRPID   = "panel.example.com"
	testOrigin = "https://panel.example.com"
)

func randChallenge() []byte {
	c := make([]byte, 32)
	_, _ = rand.Read(c)
	return c
}

// A full round trip: register a passkey, then authenticate with it.
func TestRegisterThenAssert(t *testing.T) {
	v := New(Config{RPID: testRPID, RPName: "HeroPanel", Origin: testOrigin})
	a := newVauth(t, testRPID, testOrigin)

	regChal := randChallenge()
	cred, err := v.FinishRegistration(regChal, a.clientData(t, "webauthn.create", regChal), a.attestationObject(0))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(cred.ID) != 16 || len(cred.COSEKey) == 0 {
		t.Fatalf("credential not extracted: %+v", cred)
	}

	loginChal := randChallenge()
	authData, cdj, sig := a.sign(t, loginChal, 1)
	newCount, err := v.FinishAssertion(cred, loginChal, cdj, authData, sig)
	if err != nil {
		t.Fatalf("assert: %v", err)
	}
	if newCount != 1 {
		t.Errorf("sign count = %d, want 1", newCount)
	}
}

// Every tamper the verifier must catch.
func TestAssertionRejections(t *testing.T) {
	v := New(Config{RPID: testRPID, RPName: "HeroPanel", Origin: testOrigin})
	a := newVauth(t, testRPID, testOrigin)
	regChal := randChallenge()
	cred, _ := v.FinishRegistration(regChal, a.clientData(t, "webauthn.create", regChal), a.attestationObject(0))

	loginChal := randChallenge()

	// A flipped signature byte.
	authData, cdj, sig := a.sign(t, loginChal, 1)
	sig[len(sig)-1] ^= 0xff
	if _, err := v.FinishAssertion(cred, loginChal, cdj, authData, sig); err == nil {
		t.Error("a tampered signature was accepted")
	}

	// The wrong challenge (a replay of a different login).
	authData, cdj, sig = a.sign(t, loginChal, 1)
	if _, err := v.FinishAssertion(cred, randChallenge(), cdj, authData, sig); err == nil {
		t.Error("a signature for a different challenge was accepted")
	}

	// The wrong origin (a phishing site).
	evil := newVauth(t, testRPID, "https://evil.example.com")
	evil.priv = a.priv
	evil.credID = a.credID
	ea, ecdj, esig := evil.sign(t, loginChal, 1)
	if _, err := v.FinishAssertion(cred, loginChal, ecdj, ea, esig); err == nil {
		t.Error("an assertion from the wrong origin was accepted")
	}

	// A counter rollback (a cloned authenticator) — first accept count 5, then
	// reject a later assertion that reports 3.
	authData, cdj, sig = a.sign(t, loginChal, 5)
	if _, err := v.FinishAssertion(cred, loginChal, cdj, authData, sig); err != nil {
		t.Fatalf("count 5: %v", err)
	}
	cred.SignCount = 5
	chal2 := randChallenge()
	authData, cdj, sig = a.sign(t, chal2, 3)
	if _, err := v.FinishAssertion(cred, chal2, cdj, authData, sig); err == nil {
		t.Error("a rolled-back signature counter (cloned key) was accepted")
	}

	// A different key entirely cannot assert for this credential.
	other := newVauth(t, testRPID, testOrigin)
	other.credID = a.credID
	oa, ocdj, osig := other.sign(t, loginChal, 9)
	if _, err := v.FinishAssertion(cred, loginChal, ocdj, oa, osig); err == nil {
		t.Error("an assertion signed by a different key was accepted")
	}
}

// Registration binds to the relying party: a create() for another RP-ID is
// refused (its rpIdHash will not match).
func TestRegistrationRPBinding(t *testing.T) {
	v := New(Config{RPID: testRPID, RPName: "HeroPanel", Origin: testOrigin})
	a := newVauth(t, "attacker.example.com", testOrigin)
	regChal := randChallenge()
	if _, err := v.FinishRegistration(regChal, a.clientData(t, "webauthn.create", regChal), a.attestationObject(0)); err == nil {
		t.Error("a registration for a different RP-ID was accepted")
	}
}

// ── tiny CBOR encoder (test only) ────────────────────────────────────────────

func encHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	case n < 65536:
		return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
	default:
		return []byte{major<<5 | 26, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	}
}
func encUint(n uint64) []byte  { return encHead(0, n) }
func encNint(i int64) []byte   { return encHead(1, uint64(-1-i)) }
func encBytes(b []byte) []byte { return append(encHead(2, uint64(len(b))), b...) }
func encText(s string) []byte  { return append(encHead(3, uint64(len(s))), []byte(s)...) }

func pad32(b []byte) []byte {
	if len(b) >= 32 {
		return b[len(b)-32:]
	}
	return append(make([]byte, 32-len(b)), b...)
}
