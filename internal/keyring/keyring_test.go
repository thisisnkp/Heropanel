package keyring_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/internal/keyring"
	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/pkg/secrets"
)

func newCipher(t *testing.T) (*secrets.Cipher, string) {
	t.Helper()
	raw := make([]byte, secrets.MasterKeyLen)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	enc := base64.StdEncoding.EncodeToString(raw)
	c, err := secrets.FromBase64(enc)
	if err != nil {
		t.Fatal(err)
	}
	return c, enc
}

func newStore(t *testing.T) *repository.KeyringStore {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "kr.db")
	dbh, err := repository.Open(config.Database{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if _, err := repository.Migrate(context.Background(), dbh); err != nil {
		t.Fatal(err)
	}
	return repository.NewKeyringStore(dbh)
}

func TestRotatePersistsAndReloads(t *testing.T) {
	ctx := context.Background()
	c, enc := newCipher(t)
	store := newStore(t)
	svc := keyring.NewService(c, store)

	// Before rotation: legacy mode, generation 0.
	st, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Available || st.ActiveGeneration != 0 || !st.LegacyKeyInUse {
		t.Fatalf("initial status = %+v", st)
	}
	// Seal a value under the legacy key.
	legacy, _ := c.Seal([]byte("before"), "t:1:c")

	// Rotate: generation becomes 1 and it is persisted.
	st, err = svc.Rotate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.ActiveGeneration != 1 || st.KeyCount != 1 || st.LegacyKeyInUse {
		t.Fatalf("post-rotate status = %+v", st)
	}
	keyed, _ := c.Seal([]byte("after"), "t:2:c")

	// A fresh cipher (a restart) that loads the persisted keyring opens both the
	// legacy and the keyed blob — proving the wrapped key round-trips through the DB.
	c2, _ := secrets.FromBase64(enc)
	wrapped, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c2.LoadKeyring(wrapped); err != nil {
		t.Fatalf("reload keyring: %v", err)
	}
	if pt, err := c2.Open(legacy, "t:1:c"); err != nil || string(pt) != "before" {
		t.Fatalf("legacy blob after reload: %v / %q", err, pt)
	}
	if pt, err := c2.Open(keyed, "t:2:c"); err != nil || string(pt) != "after" {
		t.Fatalf("keyed blob after reload: %v / %q", err, pt)
	}
}

func TestRotateUnavailableWithoutMaster(t *testing.T) {
	// No master key => not configured => rotation refused.
	svc := keyring.NewService(nil, newStore(t))
	if _, err := svc.Rotate(context.Background()); err == nil {
		t.Fatal("rotation without a master key must be refused")
	}
}
