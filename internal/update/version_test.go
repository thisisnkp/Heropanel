package update

import (
	"testing"

	"github.com/thisisnkp/nexpanel/internal/installer"
)

func TestValidChannel(t *testing.T) {
	for _, ok := range []string{ChannelStable, ChannelBeta, ChannelNightly} {
		if !ValidChannel(ok) {
			t.Errorf("%q should be a valid channel", ok)
		}
	}
	// An unknown channel must be refused, not silently treated as stable.
	for _, bad := range []string{"", "STABLE", "canary", "stable "} {
		if ValidChannel(bad) {
			t.Errorf("%q should not be a valid channel", bad)
		}
	}
}

// The installer writes these strings into the result file and this package
// reads them back. They are declared twice — internal/installer cannot import
// this package, because this package imports it to verify a release — so the
// contract between the two binaries is pinned here rather than assumed.
func TestStateConstantsMatchTheInstaller(t *testing.T) {
	cases := map[string]string{
		StateSucceeded:  installer.UpdateSucceeded,
		StateRolledBack: installer.UpdateRolledBack,
		StateFailed:     installer.UpdateFailed,
	}
	for mine, theirs := range cases {
		if mine != theirs {
			t.Errorf("state mismatch: update has %q, installer writes %q", mine, theirs)
		}
	}
}
