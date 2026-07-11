package pwhash_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/pkg/pwhash"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	// Use cheap params so the test is fast.
	cheap := pwhash.Params{Memory: 8 * 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}
	enc, err := pwhash.HashWith("correct horse battery staple", cheap)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(enc, "$argon2id$") {
		t.Fatalf("unexpected encoding: %q", enc)
	}

	ok, err := pwhash.Verify("correct horse battery staple", enc)
	if err != nil || !ok {
		t.Fatalf("verify correct = (%v,%v), want (true,nil)", ok, err)
	}

	ok, err = pwhash.Verify("wrong password", enc)
	if err != nil || ok {
		t.Fatalf("verify wrong = (%v,%v), want (false,nil)", ok, err)
	}
}

func TestHashIsSaltedAndUnique(t *testing.T) {
	cheap := pwhash.Params{Memory: 8 * 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}
	a, _ := pwhash.HashWith("same", cheap)
	b, _ := pwhash.HashWith("same", cheap)
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "not-a-hash", "$argon2id$v=19$bad", "$argon2i$v=19$m=8,t=1,p=1$AAAA$AAAA"} {
		if _, err := pwhash.Verify("x", bad); err == nil {
			t.Errorf("expected error for malformed hash %q", bad)
		}
	}
}

func TestNeedsRehash(t *testing.T) {
	weak := pwhash.Params{Memory: 8 * 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}
	enc, _ := pwhash.HashWith("x", weak)
	if !pwhash.NeedsRehash(enc, pwhash.Default) {
		t.Fatal("a weak hash should need rehashing against Default")
	}
	strong, _ := pwhash.HashWith("x", pwhash.Default)
	if pwhash.NeedsRehash(strong, pwhash.Default) {
		t.Fatal("a Default-strength hash should not need rehashing")
	}
}
