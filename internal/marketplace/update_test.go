package marketplace_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/marketplace"
	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/proto"
)

// Updating an installed module re-runs the whole install gate and adds two
// checks install does not need: the publisher may not change, and the version
// may not go backwards. Both are ways an attacker who reaches the catalog turns
// "update" into something else.

type publisher struct {
	pub  string
	priv ed25519.PrivateKey
}

func newPublisher(t *testing.T) publisher {
	t.Helper()
	pub, priv, err := marketplace.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sk, err := base64.StdEncoding.DecodeString(priv)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	return publisher{pub: pub, priv: ed25519.PrivateKey(sk)}
}

// moduleAt returns the sample manifest at a version, signed by p.
func (p publisher) moduleAt(t *testing.T, version string) proto.Manifest {
	t.Helper()
	m := sampleManifest()
	m.Metadata.Version = version
	return signWith(t, m, p.priv)
}

// serviceFor wires a service over the given trusted keys, catalog and store.
func serviceFor(t *testing.T, store *memStore, mods []proto.Manifest, keys ...string) *marketplace.Service {
	t.Helper()
	kr, err := marketplace.NewKeyring(keys...)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	return marketplace.NewService(kr, &marketplace.Catalog{Modules: mods}, store, nil)
}

// installedAt installs the module at a version and returns the store it used.
func installedAt(t *testing.T, p publisher, version string) *memStore {
	t.Helper()
	store := newMemStore()
	svc := serviceFor(t, store, []proto.Manifest{p.moduleAt(t, version)}, p.pub)
	if _, err := svc.Install(context.Background(), "backups-pro"); err != nil {
		t.Fatalf("install: %v", err)
	}
	return store
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error %q, got success", code)
	}
	e, ok := errx.As(err)
	if !ok {
		t.Fatalf("want a typed error %q, got %v", code, err)
	}
	if e.Code != code {
		t.Fatalf("error code = %q, want %q", e.Code, code)
	}
}

func TestUpdateMovesToTheNewerVersion(t *testing.T) {
	p := newPublisher(t)
	store := installedAt(t, p, "2.1.0")
	svc := serviceFor(t, store, []proto.Manifest{p.moduleAt(t, "2.2.0")}, p.pub)

	got, from, err := svc.Update(context.Background(), "backups-pro")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Version != "2.2.0" {
		t.Errorf("version = %q, want 2.2.0", got.Version)
	}
	// The audit trail's "which version was running before this" depends on it.
	if from != "2.1.0" {
		t.Errorf("replaced version = %q, want 2.1.0", from)
	}
}

// An operator who updates a running module did not ask for it to be switched
// off, and a module that silently stops running after an update is an outage.
func TestUpdatePreservesEnableState(t *testing.T) {
	p := newPublisher(t)
	store := installedAt(t, p, "2.1.0")
	svc := serviceFor(t, store, []proto.Manifest{p.moduleAt(t, "2.2.0")}, p.pub)

	if _, err := svc.SetEnabled(context.Background(), "backups-pro", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	got, _, err := svc.Update(context.Background(), "backups-pro")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.State != marketplace.StateEnabled {
		t.Errorf("state = %q, want it to stay %q", got.State, marketplace.StateEnabled)
	}

	// And a disabled module must not be quietly switched on either.
	if _, err := svc.SetEnabled(context.Background(), "backups-pro", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	svc2 := serviceFor(t, store, []proto.Manifest{p.moduleAt(t, "2.3.0")}, p.pub)
	got, _, err = svc2.Update(context.Background(), "backups-pro")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.State != marketplace.StateDisabled {
		t.Errorf("state = %q, want it to stay %q", got.State, marketplace.StateDisabled)
	}
}

// A catalog that regressed — rolled back, rebuilt from an older tag, or
// tampered with — must not walk an operator backwards into a known-vulnerable
// release under the name "update".
func TestUpdateRefusesAVersionThatIsNotNewer(t *testing.T) {
	p := newPublisher(t)
	for name, offered := range map[string]string{
		"same version":                         "2.1.0",
		"older version":                        "2.0.9",
		"pre-release of the installed version": "2.1.0-rc.1",
	} {
		store := installedAt(t, p, "2.1.0")
		svc := serviceFor(t, store, []proto.Manifest{p.moduleAt(t, offered)}, p.pub)

		_, _, err := svc.Update(context.Background(), "backups-pro")
		wantCode(t, err, "module_not_newer")

		cur, _ := store.Get(context.Background(), "backups-pro")
		if cur.Version != "2.1.0" {
			t.Errorf("%s: installed version changed to %q", name, cur.Version)
		}
	}
}

// The operator pinned a set of publishers, not a promise that any of them may
// take over any other's module. Both keys being trusted is not enough.
func TestUpdateRefusesAPublisherChange(t *testing.T) {
	original := newPublisher(t)
	attacker := newPublisher(t)

	store := installedAt(t, original, "2.1.0")
	// Both keys trusted; only the signer differs.
	svc := serviceFor(t, store,
		[]proto.Manifest{attacker.moduleAt(t, "2.2.0")}, original.pub, attacker.pub)

	_, _, err := svc.Update(context.Background(), "backups-pro")
	wantCode(t, err, "module_publisher_changed")

	cur, _ := store.Get(context.Background(), "backups-pro")
	if cur.Version != "2.1.0" {
		t.Errorf("installed version changed to %q after a refused update", cur.Version)
	}
}

// A signature verified once is not verified forever: the bytes on offer now are
// not the bytes that were on offer at install time.
func TestUpdateRefusesAnUnverifiedManifest(t *testing.T) {
	p := newPublisher(t)
	store := installedAt(t, p, "2.1.0")

	unsigned := sampleManifest()
	unsigned.Metadata.Version = "2.2.0" // newer, but nobody vouched for it
	svc := serviceFor(t, store, []proto.Manifest{unsigned}, p.pub)

	_, _, err := svc.Update(context.Background(), "backups-pro")
	wantCode(t, err, "module_unverified")
}

func TestUpdateRefusesAModuleThatIsNotInstalled(t *testing.T) {
	p := newPublisher(t)
	svc := serviceFor(t, newMemStore(), []proto.Manifest{p.moduleAt(t, "2.2.0")}, p.pub)

	_, _, err := svc.Update(context.Background(), "backups-pro")
	wantCode(t, err, "module_not_found")
}

// Installed but withdrawn from the catalog: uninstall still works, there is
// simply nothing to move to, and saying so beats a generic failure.
func TestUpdateRefusesWhenTheCatalogNoLongerOffersIt(t *testing.T) {
	p := newPublisher(t)
	store := installedAt(t, p, "2.1.0")
	svc := serviceFor(t, store, nil, p.pub)

	_, _, err := svc.Update(context.Background(), "backups-pro")
	wantCode(t, err, "module_not_in_catalog")
}

// Browse must only advertise an update the operator can actually apply.
func TestBrowseFlagsUpdateAvailableOnlyWhenUpdateWouldSucceed(t *testing.T) {
	p := newPublisher(t)
	attacker := newPublisher(t)

	cases := map[string]struct {
		offered proto.Manifest
		keys    []string
		want    bool
	}{
		"newer from the same publisher": {p.moduleAt(t, "2.2.0"), []string{p.pub}, true},
		"same version":                  {p.moduleAt(t, "2.1.0"), []string{p.pub}, false},
		"newer from another publisher":  {attacker.moduleAt(t, "2.2.0"), []string{p.pub, attacker.pub}, false},
	}
	for name, tc := range cases {
		store := installedAt(t, p, "2.1.0")
		svc := serviceFor(t, store, []proto.Manifest{tc.offered}, tc.keys...)

		entries, err := svc.Browse(context.Background())
		if err != nil {
			t.Fatalf("%s: browse: %v", name, err)
		}
		var found bool
		for _, e := range entries {
			if e.Slug != "backups-pro" {
				continue
			}
			found = true
			if e.UpdateAvailable != tc.want {
				t.Errorf("%s: UpdateAvailable = %v, want %v", name, e.UpdateAvailable, tc.want)
			}
			if e.InstalledVersion != "2.1.0" {
				t.Errorf("%s: InstalledVersion = %q, want 2.1.0", name, e.InstalledVersion)
			}
			// The advertised flag must agree with what Update actually does.
			_, _, err := svc.Update(context.Background(), "backups-pro")
			if tc.want && err != nil {
				t.Errorf("%s: advertised an update that failed: %v", name, err)
			}
			if !tc.want && err == nil {
				t.Errorf("%s: hid an update that would have succeeded", name)
			}
		}
		if !found {
			t.Errorf("%s: module missing from browse output", name)
		}
	}
}
