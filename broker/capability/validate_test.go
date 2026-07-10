package capability_test

import (
	"testing"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func TestValidateUsername(t *testing.T) {
	valid := []string{"site1", "a", "a_b-c", "www-data", "u0123456789012345678901234567890"} // 32 chars
	for _, u := range valid {
		if err := capability.ValidateUsername(u); err != nil {
			t.Errorf("expected %q valid, got %v", u, err)
		}
	}
	invalid := []string{"", "1abc", "Abc", "ab c", "-abc", "toolongusername_0123456789012345678"}
	for _, u := range invalid {
		if err := capability.ValidateUsername(u); err == nil {
			t.Errorf("expected %q invalid", u)
		} else if !errx.IsKind(err, errx.KindValidation) {
			t.Errorf("expected validation kind for %q, got %v", u, errx.KindOf(err))
		}
	}
}

func TestValidateFQDN(t *testing.T) {
	valid := []string{"example.com", "a.b.co", "sub.example.co.uk", "example.com.", "x1.y2.zz"}
	for _, d := range valid {
		if err := capability.ValidateFQDN(d); err != nil {
			t.Errorf("expected %q valid, got %v", d, err)
		}
	}
	invalid := []string{"", "example", "-bad.com", "a..b", "bad_underscore.com",
		"toolonglabel-0123456789012345678901234567890123456789012345678901234.com"}
	for _, d := range invalid {
		if err := capability.ValidateFQDN(d); err == nil {
			t.Errorf("expected %q invalid", d)
		}
	}
}
