package capability

import (
	"regexp"

	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Validation patterns shared by capabilities. These are strict allowlists — the
// broker rejects anything that does not match exactly.
var (
	// reUsername: lowercase Linux username, 1-32 chars, starts with a letter.
	reUsername = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	// reLabel: a single DNS label (used to validate FQDNs).
	reLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// ValidateUsername returns a validation error if u is not a valid, safe Linux
// username.
func ValidateUsername(u string) error {
	if !reUsername.MatchString(u) {
		return errx.Validation("invalid_username",
			"Username must be 1-32 chars, lowercase, starting with a letter.",
			errx.Field{Field: "username", Code: "invalid_username", Message: "invalid format"})
	}
	return nil
}

// ValidateFQDN returns a validation error if d is not a syntactically valid
// fully-qualified domain name.
func ValidateFQDN(d string) error {
	invalid := errx.Validation("invalid_domain", "Not a valid domain name.",
		errx.Field{Field: "domain", Code: "invalid_domain", Message: "invalid format"})
	if len(d) == 0 || len(d) > 253 {
		return invalid
	}
	// Trim a single trailing dot (root) before splitting labels.
	if d[len(d)-1] == '.' {
		d = d[:len(d)-1]
	}
	labels := splitDots(d)
	if len(labels) < 2 {
		return invalid
	}
	for _, l := range labels {
		if !reLabel.MatchString(l) {
			return invalid
		}
	}
	return nil
}

// ValidateServiceName checks name against the policy's service allowlist.
func ValidateServiceName(name string, p policy.Policy) error {
	if !p.ServiceAllowed(name) {
		return errx.Forbidden("service_not_allowed",
			"This service is not permitted by policy.")
	}
	return nil
}

// ValidatePath checks that pth is confined to a policy-allowed root.
func ValidatePath(pth string, p policy.Policy) error {
	if !p.PathAllowed(pth) {
		return errx.Forbidden("path_not_allowed",
			"Path is outside the permitted roots.")
	}
	return nil
}

// splitDots splits s on '.' without importing strings for a single use pattern
// elsewhere; kept explicit for clarity.
func splitDots(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
