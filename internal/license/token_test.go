package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// mint builds a compact JWS the way the licence server does, so the verifier is
// tested against the real wire format rather than against its own encoder.
func mint(t *testing.T, priv ed25519.PrivateKey, kid string, claims Claims) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": Alg, "typ": "JWT", "kid": kid})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	signing := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	sig := ed25519.Sign(priv, []byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func testKeyring(t *testing.T) (Keyring, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return Keyring{"lk1": pub}, priv
}

func validClaims(now time.Time) Claims {
	return Claims{
		LID:   "lic_8f2b91",
		Acct:  "acct_1234",
		Plan:  "pro",
		Feat:  []string{"docker", "mail", "ai", "backup", "dns"},
		Lim:   Limits{Sites: 50, DBs: 100, Users: 10},
		FP:    "sha256:9ac1",
		IP:    "103.21.44.7",
		IAT:   now.Unix(),
		Exp:   now.Add(7 * 24 * time.Hour).Unix(),
		Grace: 1209600,
		State: "active",
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	ring, priv := testKeyring(t)
	want := validClaims(epoch)

	got, err := ring.Verify(mint(t, priv, "lk1", want))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.LID != want.LID || got.Plan != want.Plan || got.Lim != want.Lim {
		t.Fatalf("claims did not round-trip: %+v", got)
	}
	if got.GraceFor() != 14*24*time.Hour {
		t.Fatalf("grace = %s, want 336h", got.GraceFor())
	}
	if !got.Has("docker") || got.Has("terminal") {
		t.Fatal("feature lookup is wrong")
	}
}

// The test the whole design rests on: a token signed by a key this binary does
// not pin must not verify. Without it, pointing an install at a lookalike
// licence server would be a free licence.
func TestATokenFromAnotherKeyIsRefused(t *testing.T) {
	ring, _ := testKeyring(t)
	_, theirs, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Same key id, different key — the impersonation that actually gets tried.
	_, err = ring.Verify(mint(t, theirs, "lk1", validClaims(epoch)))
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
}

func TestAnUnknownKeyIDIsRefusedDistinctly(t *testing.T) {
	ring, priv := testKeyring(t)
	// Distinct from a bad signature because it means something different: an
	// old binary during a key rotation, which is fixed by upgrading, not by
	// suspecting the machine.
	if _, err := ring.Verify(mint(t, priv, "lk9", validClaims(epoch))); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("err = %v, want ErrUnknownKey", err)
	}
}

// Editing the payload — raising the site limit is the edit somebody would
// actually make — must break the signature.
func TestEditingTheClaimsBreaksTheSignature(t *testing.T) {
	ring, priv := testKeyring(t)
	token := mint(t, priv, "lk1", validClaims(epoch))
	parts := strings.Split(token, ".")

	var claims Claims
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	claims.Lim.Sites = 9999
	claims.Plan = "enterprise"
	edited, _ := json.Marshal(claims)

	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString(edited) + "." + parts[2]
	if _, err := ring.Verify(forged); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
}

// The header is inside the signature too. If it were not, `kid` could be
// rewritten to point the verifier at a key the attacker controls.
func TestEditingTheHeaderBreaksTheSignature(t *testing.T) {
	pubA, privA, _ := ed25519.GenerateKey(nil)
	pubB, _, _ := ed25519.GenerateKey(nil)
	ring := Keyring{"lk1": pubA, "lk2": pubB}

	parts := strings.Split(mint(t, privA, "lk1", validClaims(epoch)), ".")
	header, _ := json.Marshal(map[string]string{"alg": Alg, "typ": "JWT", "kid": "lk2"})
	forged := base64.RawURLEncoding.EncodeToString(header) + "." + parts[1] + "." + parts[2]

	if _, err := ring.Verify(forged); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
}

// The best-known JWT bug in one line: a token that names its own algorithm and
// a verifier that believes it.
func TestAlgNoneIsRefused(t *testing.T) {
	ring, priv := testKeyring(t)
	parts := strings.Split(mint(t, priv, "lk1", validClaims(epoch)), ".")

	for _, alg := range []string{"none", "HS256", "RS256", ""} {
		header, _ := json.Marshal(map[string]string{"alg": alg, "kid": "lk1"})
		forged := base64.RawURLEncoding.EncodeToString(header) + "." + parts[1] + "." + parts[2]
		if _, err := ring.Verify(forged); err == nil {
			t.Fatalf("alg %q was accepted", alg)
		}
	}
}

func TestMalformedTokensAreRefusedNotParsed(t *testing.T) {
	ring, _ := testKeyring(t)
	for _, bad := range []string{
		"", "not-a-token", "a.b", "a.b.c.d", ".b.c", "a..c", "a.b.",
		"a.b.c", strings.Repeat("x", 4096),
	} {
		if _, err := ring.Verify(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

// Expiry is not the verifier's business. The panel walks a ladder from it, and
// a verifier that folded the two together would leave the caller unable to tell
// an expired token from a forged one — opposite situations.
func TestVerifyDoesNotCheckExpiry(t *testing.T) {
	ring, priv := testKeyring(t)
	old := validClaims(epoch.Add(-365 * 24 * time.Hour))

	got, err := ring.Verify(mint(t, priv, "lk1", old))
	if err != nil {
		t.Fatalf("a long-expired token must still verify: %v", err)
	}
	if got.ExpiresAt().After(epoch) {
		t.Fatal("the expiry did not survive the round trip")
	}
}

// A server that adds a claim must not break every panel in the field.
func TestUnknownClaimsAreIgnoredNotRejected(t *testing.T) {
	ring, priv := testKeyring(t)
	header, _ := json.Marshal(map[string]string{"alg": Alg, "typ": "JWT", "kid": "lk1"})
	payload := []byte(`{"lid":"lic_x","plan":"pro","exp":9999999999,"iat":1,"grace":1209600,"region":"ap-south-1"}`)
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	token := signing + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, []byte(signing)))

	got, err := ring.Verify(token)
	if err != nil {
		t.Fatalf("a token with an unknown claim was refused: %v", err)
	}
	if got.LID != "lic_x" {
		t.Fatalf("lid = %q", got.LID)
	}
}

// Configuration may add a key only when the binary pins none. Otherwise anyone
// who can write the panel's config file could mint themselves a licence — and
// on a customer's own VPS, that is the customer.
func TestConfiguredKeysAreIgnoredWhenAKeyIsPinned(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	other := base64.StdEncoding.EncodeToString(pub)

	t.Run("unpinned build accepts configuration", func(t *testing.T) {
		ring, pinned, err := LoadKeyring(map[string]string{"lkX": other})
		if err != nil {
			t.Fatal(err)
		}
		if pinned {
			t.Fatal("reported as pinned with no compiled-in key")
		}
		if _, ok := ring["lkX"]; !ok {
			t.Fatal("the configured key was not trusted")
		}
	})

	t.Run("pinned build ignores configuration", func(t *testing.T) {
		compiled, _, _ := ed25519.GenerateKey(nil)
		defer func(prev string) { pinnedPubKey = prev }(pinnedPubKey)
		pinnedPubKey = base64.StdEncoding.EncodeToString(compiled)

		ring, pinned, err := LoadKeyring(map[string]string{"lkX": other})
		if err != nil {
			t.Fatal(err)
		}
		if !pinned {
			t.Fatal("a compiled-in key was not reported as pinned")
		}
		if _, ok := ring["lkX"]; ok {
			t.Fatal("a configured key was trusted by a build that pins its own")
		}
	})
}
