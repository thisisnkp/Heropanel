package secrets

import (
	"crypto/rand"
	"strings"
	"testing"
)

func newMaster(t *testing.T) []byte {
	t.Helper()
	m := make([]byte, MasterKeyLen)
	if _, err := rand.Read(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestKeyringSealsUnderActiveGenerationAndOpens(t *testing.T) {
	m := newMaster(t)
	c, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	// Before any rotation: legacy hp1 format, active generation 0.
	if c.ActiveGeneration() != 0 {
		t.Fatalf("active = %d, want 0", c.ActiveGeneration())
	}
	legacy, _ := c.Seal([]byte("secret"), "t:1:c")
	if !strings.HasPrefix(legacy, "hp1.") {
		t.Fatalf("pre-rotation seal should be hp1, got %q", legacy)
	}

	// Rotate: new seals become hp2.1.…, and the legacy blob still opens.
	wk, err := c.Rotate()
	if err != nil {
		t.Fatal(err)
	}
	if wk.Generation != 1 || c.ActiveGeneration() != 1 {
		t.Fatalf("rotate gen = %d active = %d, want 1/1", wk.Generation, c.ActiveGeneration())
	}
	keyed, _ := c.Seal([]byte("secret2"), "t:2:c")
	if !strings.HasPrefix(keyed, "hp2.1.") {
		t.Fatalf("post-rotation seal should be hp2.1, got %q", keyed)
	}
	if pt, err := c.Open(legacy, "t:1:c"); err != nil || string(pt) != "secret" {
		t.Fatalf("legacy blob must still open after rotation: %v / %q", err, pt)
	}
	if pt, err := c.Open(keyed, "t:2:c"); err != nil || string(pt) != "secret2" {
		t.Fatalf("keyed blob open: %v / %q", err, pt)
	}
}

func TestKeyringPersistAndReload(t *testing.T) {
	m := newMaster(t)
	c, _ := New(m)
	wk, _ := c.Rotate()
	blob, _ := c.Seal([]byte("payload"), "t:5:c")

	// A fresh Cipher (restart) with the same master + persisted wrapped key opens it.
	c2, _ := New(m)
	if err := c2.LoadKeyring([]WrappedKey{wk}); err != nil {
		t.Fatalf("load keyring: %v", err)
	}
	if pt, err := c2.Open(blob, "t:5:c"); err != nil || string(pt) != "payload" {
		t.Fatalf("reloaded keyring must open the blob: %v / %q", err, pt)
	}
}

func TestKeyringMultiGenerationOldStillOpens(t *testing.T) {
	m := newMaster(t)
	c, _ := New(m)
	c.Rotate() // gen 1
	g1, _ := c.Seal([]byte("v1"), "t:1:c")
	c.Rotate() // gen 2
	g2, _ := c.Seal([]byte("v2"), "t:1:c")
	if blobGeneration(g1) != 1 || blobGeneration(g2) != 2 {
		t.Fatalf("generations = %d,%d want 1,2", blobGeneration(g1), blobGeneration(g2))
	}
	if pt, _ := c.Open(g1, "t:1:c"); string(pt) != "v1" {
		t.Error("gen-1 blob must still open under gen-2 active cipher")
	}
	if pt, _ := c.Open(g2, "t:1:c"); string(pt) != "v2" {
		t.Error("gen-2 blob open")
	}
}

func TestRewrapUnderNewMasterKeepsBlobsReadable(t *testing.T) {
	oldM := newMaster(t)
	newM := newMaster(t)
	c, _ := New(oldM)
	wk, _ := c.Rotate()
	blob, _ := c.Seal([]byte("survives"), "t:9:c")

	// Master rotation: re-wrap the data keys under the new master. The row blob is
	// untouched.
	rewrapped, err := c.Rewrap(newM)
	if err != nil {
		t.Fatal(err)
	}
	if len(rewrapped) != 1 || rewrapped[0].Generation != wk.Generation {
		t.Fatalf("rewrap = %+v", rewrapped)
	}
	// The old wrapped key must NOT unwrap under the new master (it really changed).
	cNewOldKeys, _ := New(newM)
	if err := cNewOldKeys.LoadKeyring([]WrappedKey{wk}); err == nil {
		t.Fatal("the pre-rewrap wrapped key must not unwrap under the new master")
	}
	// The rewrapped key under the new master opens the untouched blob.
	cNew, _ := New(newM)
	if err := cNew.LoadKeyring(rewrapped); err != nil {
		t.Fatalf("load rewrapped: %v", err)
	}
	if pt, err := cNew.Open(blob, "t:9:c"); err != nil || string(pt) != "survives" {
		t.Fatalf("blob must open under the new master after rewrap: %v / %q", err, pt)
	}
}

func TestResealMigratesToActiveGeneration(t *testing.T) {
	m := newMaster(t)
	c, _ := New(m)
	legacy, _ := c.Seal([]byte("old"), "t:1:c") // hp1
	c.Rotate()                                  // active = 1

	nb, changed, err := c.Reseal(legacy, "t:1:c")
	if err != nil || !changed {
		t.Fatalf("reseal: changed=%v err=%v", changed, err)
	}
	if blobGeneration(nb) != 1 {
		t.Fatalf("resealed blob gen = %d, want 1", blobGeneration(nb))
	}
	if pt, _ := c.Open(nb, "t:1:c"); string(pt) != "old" {
		t.Error("resealed blob must still open to the same plaintext")
	}
	// Reseal of an already-active blob is a no-op.
	if _, changed, _ := c.Reseal(nb, "t:1:c"); changed {
		t.Error("reseal of an active-generation blob should not change it")
	}
}

func TestKeyedBlobFailsWithoutKeyring(t *testing.T) {
	m := newMaster(t)
	c, _ := New(m)
	c.Rotate()
	blob, _ := c.Seal([]byte("x"), "t:1:c")
	// A fresh cipher without the keyring loaded cannot open an hp2 blob.
	c2, _ := New(m)
	if _, err := c2.Open(blob, "t:1:c"); err == nil {
		t.Fatal("an hp2 blob must not open without its data key loaded")
	}
}

func TestAADStillEnforcedUnderKeyring(t *testing.T) {
	m := newMaster(t)
	c, _ := New(m)
	c.Rotate()
	blob, _ := c.Seal([]byte("x"), "t:1:c")
	if _, err := c.Open(blob, "t:2:c"); err == nil {
		t.Fatal("wrong AAD must still fail under the keyring")
	}
}
