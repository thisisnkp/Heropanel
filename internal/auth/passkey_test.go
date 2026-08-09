package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/auth"
	"github.com/thisisnkp/nexpanel/internal/auth/webauthn"
	"github.com/thisisnkp/nexpanel/internal/repository"
)

const (
	pkRPID   = "panel.example.com"
	pkOrigin = "https://panel.example.com"
)

var pkB64 = base64.RawURLEncoding

// A full passkey round trip THROUGH THE SERVICE: register while "signed in",
// then log in passwordlessly — proving the challenge storage, credential
// persistence, assertion verification and session issue all fit together, with
// a virtual authenticator standing in for the browser + hardware token.
func TestPasskeyRegisterAndLogin(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig()).WithWebAuthn(
		repository.NewWebAuthnRepository(db),
		webauthn.New(webauthn.Config{RPID: pkRPID, RPName: "NexPanel", Origin: pkOrigin}),
	)
	seedUser(t, db, "op@example.com", "op", "supersecret1", "admin")
	if !svc.PasskeysEnabled() {
		t.Fatal("passkeys should be enabled")
	}
	ctx := context.Background()

	// Discover the seeded user's id via a password login.
	lr, err := svc.Login(ctx, "op@example.com", "supersecret1", "1.2.3.4", "test")
	if err != nil || lr.Principal == nil {
		t.Fatalf("password login: %v", err)
	}
	userID := lr.Principal.UserID

	a := newVirtAuth(t)

	// Register.
	regOpts, err := svc.BeginPasskeyRegistration(ctx, userID)
	if err != nil {
		t.Fatalf("begin register: %v", err)
	}
	regChal := mustDecode(t, regOpts.Challenge)
	att, cdj := a.attestation(t, regChal)
	if _, err := svc.FinishPasskeyRegistration(ctx, userID, "My Key", a.credID, cdj, att); err != nil {
		t.Fatalf("finish register: %v", err)
	}
	keys, _ := svc.ListPasskeys(ctx, userID)
	if len(keys) != 1 || keys[0].Name != "My Key" {
		t.Fatalf("passkey not stored: %+v", keys)
	}

	// Log in with it.
	loginOpts, token, err := svc.BeginPasskeyLogin(ctx, "op@example.com")
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	loginChal := mustDecode(t, loginOpts.Challenge)
	authData, lcdj, sig := a.assertion(t, loginChal, 1)
	res, err := svc.FinishPasskeyLogin(ctx, token, a.credID, lcdj, authData, sig, "1.2.3.4", "test")
	if err != nil {
		t.Fatalf("finish login: %v", err)
	}
	if res.Principal == nil || res.Principal.UserID != userID || res.SessionToken == "" {
		t.Fatalf("passkey login did not issue a session: %+v", res)
	}
}

// An assertion for an account that never registered a passkey fails.
func TestPasskeyLoginWithoutCredential(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig()).WithWebAuthn(
		repository.NewWebAuthnRepository(db),
		webauthn.New(webauthn.Config{RPID: pkRPID, RPName: "NexPanel", Origin: pkOrigin}),
	)
	seedUser(t, db, "op@example.com", "op", "supersecret1", "admin")
	ctx := context.Background()

	_, token, err := svc.BeginPasskeyLogin(ctx, "op@example.com")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	a := newVirtAuth(t)
	_, cdj, sig := a.assertion(t, []byte("whatever0000000000000000000000000"), 1)
	authData := a.authData(0x01, 1, false)
	if _, err := svc.FinishPasskeyLogin(ctx, token, a.credID, cdj, authData, sig, "1.2.3.4", "test"); err == nil {
		t.Error("login succeeded with no registered passkey")
	}
}

// ── minimal ES256 virtual authenticator ──────────────────────────────────────

type virtAuth struct {
	priv   *ecdsa.PrivateKey
	credID []byte
}

func newVirtAuth(t *testing.T) *virtAuth {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &virtAuth{priv: priv, credID: id}
}

func (a *virtAuth) authData(flags byte, count uint32, withCred bool) []byte {
	h := sha256.Sum256([]byte(pkRPID))
	b := append([]byte(nil), h[:]...)
	b = append(b, flags)
	var sc [4]byte
	binary.BigEndian.PutUint32(sc[:], count)
	b = append(b, sc[:]...)
	if withCred {
		b = append(b, make([]byte, 16)...)
		var cl [2]byte
		binary.BigEndian.PutUint16(cl[:], uint16(len(a.credID)))
		b = append(b, cl[:]...)
		b = append(b, a.credID...)
		b = append(b, a.cose()...)
	}
	return b
}

func (a *virtAuth) cose() []byte {
	x := pad(a.priv.PublicKey.X.Bytes())
	y := pad(a.priv.PublicKey.Y.Bytes())
	m := head(5, 5)
	m = append(m, head(0, 1)...) // 1 (kty)
	m = append(m, head(0, 2)...) // EC2
	m = append(m, head(0, 3)...) // 3 (alg)
	m = append(m, head(1, 6)...) // -7
	m = append(m, head(1, 0)...) // -1 (crv)
	m = append(m, head(0, 1)...) // P256
	m = append(m, head(1, 1)...) // -2 (x)
	m = append(m, hbytes(x)...)
	m = append(m, head(1, 2)...) // -3 (y)
	m = append(m, hbytes(y)...)
	return m
}

func (a *virtAuth) attestation(t *testing.T, challenge []byte) (att, cdj []byte) {
	t.Helper()
	authData := a.authData(0x41, 0, true) // UP|AT
	m := head(5, 3)
	m = append(m, htext("fmt")...)
	m = append(m, htext("none")...)
	m = append(m, htext("attStmt")...)
	m = append(m, head(5, 0)...)
	m = append(m, htext("authData")...)
	m = append(m, hbytes(authData)...)
	cdj, _ = json.Marshal(map[string]string{"type": "webauthn.create", "challenge": pkB64.EncodeToString(challenge), "origin": pkOrigin})
	return m, cdj
}

func (a *virtAuth) assertion(t *testing.T, challenge []byte, count uint32) (authData, cdj, sig []byte) {
	t.Helper()
	authData = a.authData(0x01, count, false) // UP
	cdj, _ = json.Marshal(map[string]string{"type": "webauthn.get", "challenge": pkB64.EncodeToString(challenge), "origin": pkOrigin})
	cdh := sha256.Sum256(cdj)
	signed := append(append([]byte(nil), authData...), cdh[:]...)
	digest := sha256.Sum256(signed)
	sig, _ = ecdsa.SignASN1(rand.Reader, a.priv, digest[:])
	return authData, cdj, sig
}

func head(major byte, n uint64) []byte {
	if n < 24 {
		return []byte{major<<5 | byte(n)}
	}
	return []byte{major<<5 | 24, byte(n)}
}
func hbytes(b []byte) []byte { return append(head(2, uint64(len(b))), b...) }
func htext(s string) []byte  { return append(head(3, uint64(len(s))), []byte(s)...) }
func pad(b []byte) []byte {
	if len(b) >= 32 {
		return b[len(b)-32:]
	}
	return append(make([]byte, 32-len(b)), b...)
}
func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := pkB64.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
