package semver

import (
	"strconv"
	"strings"
)

// DevVersion is the version a binary built without -ldflags reports. It is
// treated as older than every real release, so a developer build always sees an
// update available rather than being told it is somehow current.
const DevVersion = "0.0.0-dev"

// Compare orders two semantic versions: -1 if a < b, 0 if equal, +1 if a > b.
//
// Hand-rolled rather than pulled in, matching the SigV4 signer, the SFTP client
// and the WebAuthn verifier — and this is a smaller problem than any of those.
// It implements the part of semver 2.0.0 that release ordering actually needs:
//
//   - a leading "v" is ignored, so "v1.2.3" and "1.2.3" are the same release;
//   - build metadata ("+abc") is ignored entirely, per the spec — two versions
//     differing only in build metadata are the same release;
//   - a pre-release ("1.2.3-rc.1") sorts *before* its release ("1.2.3"), which
//     is what stops a beta channel from looking newer than the stable it came
//     from;
//   - pre-release identifiers compare numerically when both are numeric and
//     lexically otherwise, so rc.9 < rc.10 rather than the string order.
//
// Missing numeric components are zero, so "1.2" == "1.2.0".
func Compare(a, b string) int {
	aCore, aPre := splitVersion(a)
	bCore, bPre := splitVersion(b)

	if c := compareCore(aCore, bCore); c != 0 {
		return c
	}
	// Equal cores: absence of a pre-release wins, since 1.2.3 > 1.2.3-rc.1.
	switch {
	case aPre == "" && bPre == "":
		return 0
	case aPre == "":
		return 1
	case bPre == "":
		return -1
	}
	return comparePreRelease(aPre, bPre)
}

// Newer reports whether candidate is a strictly newer release than current.
func Newer(current, candidate string) bool { return Compare(candidate, current) > 0 }

// splitVersion strips a leading "v" and build metadata, then separates the
// numeric core from the pre-release tail.
func splitVersion(v string) (core, pre string) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i] // build metadata is not part of precedence
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

func compareCore(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		if c := compareNum(at(as, i), at(bs, i)); c != 0 {
			return c
		}
	}
	return 0
}

// comparePreRelease walks dot-separated identifiers. A longer pre-release is
// greater when all shared identifiers are equal (1.2.3-rc.1 < 1.2.3-rc.1.1).
func comparePreRelease(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai, aok := idx(as, i)
		bi, bok := idx(bs, i)
		switch {
		case !aok:
			return -1
		case !bok:
			return 1
		}
		an, aErr := strconv.Atoi(ai)
		bn, bErr := strconv.Atoi(bi)
		aNumeric, bNumeric := aErr == nil, bErr == nil
		switch {
		case aNumeric && bNumeric:
			// Both numeric: compare as numbers so rc.9 < rc.10.
			if an != bn {
				return sign(an - bn)
			}
		case aNumeric:
			return -1 // numeric identifiers rank below alphanumeric ones
		case bNumeric:
			return 1
		default:
			if c := strings.Compare(ai, bi); c != 0 {
				return c
			}
		}
	}
	return 0
}

// compareNum compares two numeric components, treating anything unparseable as
// zero rather than failing — a malformed version must not crash a check.
func compareNum(a, b string) int {
	ai, _ := strconv.Atoi(a)
	bi, _ := strconv.Atoi(b)
	return sign(ai - bi)
}

func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "0"
}

func idx(s []string, i int) (string, bool) {
	if i < len(s) {
		return s[i], true
	}
	return "", false
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
