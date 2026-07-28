package ssl

import (
	"encoding/base64"
	"testing"
)

func TestDecodeEABKeyAcceptsBase64Variants(t *testing.T) {
	raw := []byte("a-32-byte-hmac-key-for-zerossl!!")
	forms := []string{
		base64.RawURLEncoding.EncodeToString(raw), // ZeroSSL's form
		base64.URLEncoding.EncodeToString(raw),
		base64.StdEncoding.EncodeToString(raw),
	}
	for _, f := range forms {
		got, err := decodeEABKey(f)
		if err != nil {
			t.Fatalf("decode %q: %v", f, err)
		}
		if string(got) != string(raw) {
			t.Errorf("decode %q = %q, want the raw key", f, got)
		}
	}
	if _, err := decodeEABKey("!!!not base64!!!"); err == nil {
		t.Error("garbage must be rejected")
	}
}

func TestNewACMEIssuerSetsEAB(t *testing.T) {
	hmac := base64.RawURLEncoding.EncodeToString([]byte("secret-hmac-key"))
	iss, err := NewACMEIssuer(ZeroSSLDirectory, "a@b.com", "kid-123", hmac)
	if err != nil {
		t.Fatalf("new zerossl issuer: %v", err)
	}
	if iss.eab == nil || iss.eab.KID != "kid-123" {
		t.Fatalf("EAB not set: %+v", iss.eab)
	}
	// No KID => a plain ACME issuer with no EAB (behaves like Let's Encrypt).
	plain, err := NewACMEIssuer(LetsEncryptDirectory, "a@b.com", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if plain.eab != nil {
		t.Error("no KID must mean no EAB")
	}
	// An empty directory is refused.
	if _, err := NewACMEIssuer("", "a@b.com", "", ""); err == nil {
		t.Error("empty directory must be rejected")
	}
}
