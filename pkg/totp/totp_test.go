package totp_test

import (
	"testing"

	"github.com/thisisnkp/heropanel/pkg/totp"
)

func TestGenerateCodeValidate(t *testing.T) {
	secret, err := totp.GenerateSecret()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	code, err := totp.Code(secret)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("code = %q, want 6 digits", code)
	}
	if !totp.Validate(secret, code) {
		t.Fatal("current code should validate")
	}
	if totp.Validate(secret, "000000") && code != "000000" {
		t.Fatal("a wrong code should not validate")
	}
	if totp.Validate(secret, "12345") {
		t.Fatal("wrong-length code must not validate")
	}
}

func TestProvisioningURI(t *testing.T) {
	uri := totp.ProvisioningURI("ABCDEF", "user@example.com", "HeroPanel")
	if uri == "" || uri[:15] != "otpauth://totp/" {
		t.Fatalf("bad uri: %q", uri)
	}
}
