package arch_test

import (
	"testing"

	"github.com/thisisnkp/heropanel/pkg/arch"
)

// The aliases that actually turn up: uname and package managers speak one
// spelling, Go speaks another, and they must all land on the same Arch.
func TestNormalizeMapsTheAliasesThatOccur(t *testing.T) {
	cases := map[string]arch.Arch{
		"amd64": arch.AMD64, "x86_64": arch.AMD64, "x64": arch.AMD64,
		"arm64": arch.ARM64, "aarch64": arch.ARM64,
		"386": arch.I386, "i386": arch.I386, "i686": arch.I386, "x86": arch.I386,
	}
	for in, want := range cases {
		if got := arch.Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// An unknown arch must survive as a visible value, not fall back to amd64 —
// which would "work" until the binary faulted.
func TestNormalizeKeepsUnknownArchVisible(t *testing.T) {
	got := arch.Normalize("riscv64")
	if got != arch.Arch("riscv64") {
		t.Errorf("Normalize(riscv64) = %q, want it preserved unchanged", got)
	}
	if got.Supported() {
		t.Error("an unknown arch reported itself as supported")
	}
}

func TestSupportedSet(t *testing.T) {
	if !arch.AMD64.Supported() || !arch.ARM64.Supported() || !arch.I386.Supported() {
		t.Error("one of the three built architectures is not marked supported")
	}
	got := arch.Supported()
	if len(got) != 3 {
		t.Fatalf("Supported() returned %d architectures, want 3", len(got))
	}
	// Stable order so installer output and the release matrix agree.
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("Supported() is not sorted: %v", got)
		}
	}
}

// The artifact name is the one string the bootstrap script, module installer,
// and release build must all agree on. Pin it.
func TestArtifactName(t *testing.T) {
	if got := arch.ArtifactName("hp-installer", arch.Linux, arch.ARM64); got != "hp-installer-linux-arm64" {
		t.Errorf("ArtifactName = %q", got)
	}
	if got := arch.ArtifactName("hp-mod-docker", arch.Linux, arch.AMD64); got != "hp-mod-docker-linux-amd64" {
		t.Errorf("ArtifactName = %q", got)
	}
}

func TestCurrentIsSupportedOnThisHost(t *testing.T) {
	// The test binary runs on a build target, so Current must be one we support —
	// if this fails, the release matrix and pkg/arch disagree.
	if !arch.Current().Supported() {
		t.Skipf("running on an unsupported arch %q; not a failure of the package", arch.Current())
	}
}
