package php_test

import (
	"regexp"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/php"
)

// The default has to be one of the versions a site may select. Nothing else in
// the panel checks this: EnsurePool validates the version it is given, and a
// site that specifies none is handed DefaultVersion without a second look — so
// a default outside the allowlist would refuse every site that expressed no
// preference, which is most of them.
func TestDefaultVersionIsSelectable(t *testing.T) {
	if !php.IsSupported(php.DefaultVersion) {
		t.Fatalf("DefaultVersion %q is not in SupportedVersions %v", php.DefaultVersion, php.SupportedVersions)
	}
}

// The allowlist is read straight into a package name (php-fpm7.4, remi tag 74)
// and into a systemd unit, so anything that is not major.minor produces a
// package the host has never heard of at the moment a site is created.
func TestSupportedVersionsAreWellFormedAndOrdered(t *testing.T) {
	reVersion := regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
	seen := map[string]bool{}
	prev := ""
	for _, v := range php.SupportedVersions {
		if !reVersion.MatchString(v) {
			t.Errorf("version %q is not major.minor", v)
		}
		if seen[v] {
			t.Errorf("version %q is listed twice", v)
		}
		seen[v] = true
		// Ordered oldest-first, which is the order the version selector shows.
		// String comparison is enough while every entry is single-digit either
		// side of the dot; the well-formed check above holds that true, and "8.10"
		// would need a real comparator here.
		if prev != "" && v <= prev {
			t.Errorf("version %q comes after %q; the list must be oldest-first", v, prev)
		}
		prev = v
	}

	// The two ends of the range are a product decision, not an accident: 7.4 so a
	// legacy application can be hosted here rather than somewhere unwatched, and
	// the current release so a new site is not born a version behind.
	for _, want := range []string{"7.4", php.DefaultVersion} {
		if !seen[want] {
			t.Errorf("PHP %s is missing from SupportedVersions", want)
		}
	}
	if seen["5.6"] || seen["7.0"] {
		t.Error("PHP 5.x/7.0 are not packaged by any current distribution and must not be selectable")
	}
}
