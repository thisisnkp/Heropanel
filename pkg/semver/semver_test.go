package semver_test

import (
	"testing"

	"github.com/thisisnkp/nexpanel/pkg/semver"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.4", "1.2.3", 1},
		{"1.3.0", "1.2.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.2.3", "1.2.4", -1},

		// A leading v is cosmetic; tags carry it, manifests often do not.
		{"v1.2.3", "1.2.3", 0},
		{"v1.2.4", "v1.2.3", 1},

		// Missing components are zero.
		{"1.2", "1.2.0", 0},
		{"1", "1.0.0", 0},

		// Build metadata is not part of precedence (semver 2.0.0 §10).
		{"1.2.3+build.5", "1.2.3", 0},
		{"1.2.3+a", "1.2.3+b", 0},

		// A pre-release is older than its release — this is what stops a beta
		// from looking newer than the stable it was cut from.
		{"1.2.3-rc.1", "1.2.3", -1},
		{"1.2.3", "1.2.3-rc.1", 1},
		{"1.2.3-rc.1", "1.2.3-rc.2", -1},

		// Numeric identifiers compare as numbers, not strings: the string order
		// would put rc.10 before rc.9.
		{"1.2.3-rc.10", "1.2.3-rc.9", 1},

		// A longer pre-release wins when the shared identifiers are equal.
		{"1.2.3-rc.1.1", "1.2.3-rc.1", 1},

		// Numeric identifiers rank below alphanumeric ones.
		{"1.2.3-1", "1.2.3-alpha", -1},

		// The development build must lose to every real release, or a dev panel
		// would report itself up to date forever.
		{semver.DevVersion, "1.0.0", -1},
		{semver.DevVersion, "0.0.1", -1},
		{semver.DevVersion, "0.0.0", -1},
		{"1.0.0", semver.DevVersion, 1},

		// Garbage must not panic; unparseable components read as zero.
		{"not-a-version", "1.0.0", -1},
		{"", "0.0.0", 0},
	}
	for _, tc := range cases {
		if got := semver.Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("semver.Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// Compare must be antisymmetric, or "is there an update" and "am I ahead"
// could both answer yes.
func TestCompareIsAntisymmetric(t *testing.T) {
	versions := []string{semver.DevVersion, "0.0.0", "1.0.0", "1.2.3-rc.1", "1.2.3", "1.2.4", "2.0.0"}
	for _, a := range versions {
		for _, b := range versions {
			if got, rev := semver.Compare(a, b), semver.Compare(b, a); got != -rev {
				t.Errorf("semver.Compare(%q,%q)=%d but semver.Compare(%q,%q)=%d", a, b, got, b, a, rev)
			}
		}
	}
}

func TestNewer(t *testing.T) {
	if !semver.Newer("1.2.3", "1.2.4") {
		t.Error("1.2.4 should be newer than 1.2.3")
	}
	if semver.Newer("1.2.3", "1.2.3") {
		t.Error("a version is not newer than itself")
	}
	if semver.Newer("1.2.3", "1.2.2") {
		t.Error("an older version must not report as newer")
	}
	if !semver.Newer(semver.DevVersion, "0.1.0") {
		t.Error("any release should be newer than the dev build")
	}
}
