package blobcrypt

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func roundTrip(t *testing.T, plain []byte, key []byte) []byte {
	t.Helper()
	var sealed bytes.Buffer
	if err := Seal(&sealed, bytes.NewReader(plain), key); err != nil {
		t.Fatalf("seal: %v", err)
	}
	var opened bytes.Buffer
	if err := Open(&opened, bytes.NewReader(sealed.Bytes()), key); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(opened.Bytes(), plain) {
		t.Fatalf("round trip lost data: got %d bytes, want %d", opened.Len(), len(plain))
	}
	return sealed.Bytes()
}

func TestRoundTripsAcrossChunkBoundaries(t *testing.T) {
	key := testKey(t)
	for _, size := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3*chunkSize + 17} {
		plain := make([]byte, size)
		_, _ = rand.Read(plain)
		roundTrip(t, plain, key)
	}
}

func TestTamperedChunkIsRejected(t *testing.T) {
	key := testKey(t)
	sealed := roundTrip(t, bytes.Repeat([]byte("x"), 2*chunkSize), key)
	// Flip one ciphertext bit past the header.
	sealed[len(magic)+prefixLen+10] ^= 0x01
	if err := Open(&bytes.Buffer{}, bytes.NewReader(sealed), key); err != ErrCorrupt {
		t.Fatalf("tampered file opened: %v", err)
	}
}

// Truncation is the failure mode a naive chunked format misses: cutting a file
// after any complete chunk still authenticates chunk-by-chunk. The final-chunk
// flag is what catches it.
func TestTruncationIsRejected(t *testing.T) {
	key := testKey(t)
	sealed := roundTrip(t, bytes.Repeat([]byte("y"), 3*chunkSize), key)
	// Cut the file so it ends exactly after the SECOND complete chunk.
	// Header + 2 * (4-byte len + chunk ct).
	ctLen := chunkSize + 16
	cut := len(magic) + prefixLen + 2*(4+ctLen)
	if err := Open(&bytes.Buffer{}, bytes.NewReader(sealed[:cut]), key); err != ErrCorrupt {
		t.Fatalf("truncated file opened: %v", err)
	}
}

func TestWrongKeyIsRejected(t *testing.T) {
	sealed := roundTrip(t, []byte("secret backup"), testKey(t))
	if err := Open(&bytes.Buffer{}, bytes.NewReader(sealed), testKey(t)); err != ErrCorrupt {
		t.Fatalf("wrong key opened the file: %v", err)
	}
}

func TestTrailingGarbageIsRejected(t *testing.T) {
	key := testKey(t)
	sealed := roundTrip(t, []byte("data"), key)
	if err := Open(&bytes.Buffer{}, bytes.NewReader(append(sealed, 0xFF)), key); err != ErrCorrupt {
		t.Fatalf("appended garbage was accepted: %v", err)
	}
}
