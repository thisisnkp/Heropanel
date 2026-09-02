package httpapi

import (
	"context"
)

// The licence gates the feature handlers call.
//
// Three near-identical functions rather than one parameterised helper, and that
// is deliberate. A single `checkLicense(kind)` would be one function to patch
// out and one branch to invert, and the whole product would be unlocked at a
// stroke. Three separate ones, called from three separate handlers, each
// reaching a different method on the licence service, have to be found and
// defeated three times — and a miss leaves that feature still gated.
//
// The counting is the other reason they are separate: each asks a different
// service how many of its own thing exist. There is no shared notion of "how
// many things does this panel have".
//
// Every one of them refuses only *creation*. Nothing reachable from here can
// stop a site serving, a database answering, or a user signing in.

// licenseAllowsNewSite gates POST /sites.
func licenseAllowsNewSite(ctx context.Context, d Deps) error {
	if d.License == nil {
		return nil
	}
	// The count is a closure, not a number: it is only taken if the ladder
	// allows the action at all. A locked panel must not query every site on the
	// box on its way to refusing.
	return d.License.CanCreateSite(func() int {
		if d.Sites == nil {
			return 0
		}
		rows, err := d.Sites.List(ctx, 0, licenseCountCap, 0)
		if err != nil {
			return 0
		}
		return len(rows)
	})
}

// licenseAllowsNewDatabase gates POST /databases.
func licenseAllowsNewDatabase(ctx context.Context, d Deps) error {
	if d.License == nil {
		return nil
	}
	// The count is a closure, not a number: it is only taken if the ladder
	// allows the action at all. A locked panel must not query every database on the
	// box on its way to refusing.
	return d.License.CanCreateDatabase(func() int {
		if d.Databases == nil {
			return 0
		}
		rows, err := d.Databases.ListDatabases(ctx, 0, licenseCountCap, 0)
		if err != nil {
			return 0
		}
		return len(rows)
	})
}

// licenseAllowsNewUser gates POST /users.
func licenseAllowsNewUser(ctx context.Context, d Deps) error {
	if d.License == nil {
		return nil
	}
	// The count is a closure, not a number: it is only taken if the ladder
	// allows the action at all. A locked panel must not query every user on the
	// box on its way to refusing.
	return d.License.CanCreateUser(func() int {
		if d.Users == nil {
			return 0
		}
		rows, err := d.Users.List(ctx, licenseCountCap, 0)
		if err != nil {
			return 0
		}
		return len(rows)
	})
}

// licenseCountCap bounds the listing each gate does. Above it the count is
// under-reported, which errs toward letting the customer create the thing —
// the right direction to be wrong in when the alternative is blocking a paying
// customer over an arithmetic detail.
const licenseCountCap = 5000
