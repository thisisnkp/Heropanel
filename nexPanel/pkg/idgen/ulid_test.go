package idgen

import (
	"strings"
	"testing"
	"time"
)

func TestNewULIDShapeAndCharset(t *testing.T) {
	id := NewULID()
	if len(id) != 26 {
		t.Fatalf("ULID length = %d, want 26 (%q)", len(id), id)
	}
	for _, r := range id {
		if !strings.ContainsRune(crockford, r) {
			t.Fatalf("ULID contains non-Crockford char %q in %q", r, id)
		}
	}
}

func TestNewULIDUnique(t *testing.T) {
	seen := make(map[string]struct{}, 10000)
	for i := 0; i < 10000; i++ {
		id := NewULID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ULID generated: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestULIDSortsByTime(t *testing.T) {
	a := NewULID()
	time.Sleep(2 * time.Millisecond)
	b := NewULID()
	if !(a < b) {
		t.Fatalf("later ULID should sort after earlier: a=%q b=%q", a, b)
	}
}

func TestNewPrefixed(t *testing.T) {
	id := NewPrefixed("sit")
	if !strings.HasPrefix(id, "sit_") {
		t.Fatalf("missing prefix: %q", id)
	}
	if got := strings.TrimPrefix(id, "sit_"); len(got) != 26 {
		t.Fatalf("prefixed ULID body length = %d, want 26 (%q)", len(got), id)
	}
}
