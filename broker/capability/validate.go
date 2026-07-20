package capability

import (
	"path"
	"regexp"
	"strings"

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
	// reVhost: an internal virtual-host identifier (used to build config paths).
	reVhost = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	// rePHPVersion: a major.minor PHP version like "8.2".
	rePHPVersion = regexp.MustCompile(`^\d+\.\d+$`)
)

// ValidatePHPVersion returns a validation error if v is not a major.minor PHP
// version (it is used to construct filesystem paths and service names).
func ValidatePHPVersion(v string) error {
	if !rePHPVersion.MatchString(v) {
		return errx.Validation("invalid_php_version", "Invalid PHP version.",
			errx.Field{Field: "version", Code: "invalid_php_version", Message: "expected e.g. 8.2"})
	}
	return nil
}

// reDBIdent: a safe SQL identifier (database or username). Restricted so it can
// be embedded in SQL with backtick/quote delimiters with no injection risk.
var reDBIdent = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)

// reDBHost: a MySQL account host (localhost, %, or hostname/ip-ish).
var reDBHost = regexp.MustCompile(`^[a-zA-Z0-9_.%-]{1,255}$`)

// ValidateDBIdentifier validates a database or user name.
func ValidateDBIdentifier(s string) error {
	if !reDBIdent.MatchString(s) {
		return errx.Validation("invalid_identifier",
			"Only letters, digits and underscore are allowed (max 64 chars).",
			errx.Field{Field: "name", Code: "invalid_identifier", Message: "invalid"})
	}
	return nil
}

// ValidateDBHost validates a MySQL account host.
func ValidateDBHost(s string) error {
	if !reDBHost.MatchString(s) {
		return errx.Validation("invalid_host", "Invalid database host.")
	}
	return nil
}

// ValidateVhostName returns a validation error if name is not a safe virtual
// host identifier (it is used to construct filesystem paths).
func ValidateVhostName(name string) error {
	if !reVhost.MatchString(name) || name == "." || name == ".." {
		return errx.Validation("invalid_vhost", "Invalid virtual host name.",
			errx.Field{Field: "name", Code: "invalid_vhost", Message: "invalid format"})
	}
	return nil
}

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

// ConfinedPath joins a caller-supplied relative path onto a policy-confined
// root, clamping any traversal so the result can never leave the root.
//
// This is the single implementation of that clamp: the File Manager's file
// operations and the interactive terminal's working directory both go through
// it, so there is one place to review and one place a mistake could live.
//
// `path.Clean` on an absolute path cannot ascend above "/", so prefixing "/"
// before cleaning neutralises "../" sequences and absolute inputs alike; the
// joined result is then re-validated against the policy roots.
func ConfinedPath(root, rel string, p policy.Policy) (string, error) {
	if err := ValidatePath(root, p); err != nil {
		return "", err
	}
	abs := path.Clean(root + "/" + path.Clean("/"+rel))
	if abs != path.Clean(root) && !strings.HasPrefix(abs, path.Clean(root)+"/") {
		return "", errx.Forbidden("path_escape", "Path escapes the permitted root.")
	}
	if err := ValidatePath(abs, p); err != nil {
		return "", err
	}
	return abs, nil
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
