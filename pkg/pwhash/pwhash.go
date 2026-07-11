// Package pwhash hashes and verifies passwords with Argon2id (docs/05 §4).
//
// Hashes are encoded in the standard PHC string format so parameters travel
// with the hash, allowing cost upgrades over time without breaking old hashes:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<b64 salt>$<b64 key>
package pwhash

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are the Argon2id cost parameters.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32 // iterations
	Threads uint8  // parallelism
	SaltLen uint32
	KeyLen  uint32
}

// Default is a sensible interactive-login baseline (~64 MiB, 3 passes).
var Default = Params{Memory: 64 * 1024, Time: 3, Threads: 2, SaltLen: 16, KeyLen: 32}

var b64 = base64.RawStdEncoding

// ErrInvalidHash is returned when an encoded hash cannot be parsed.
var ErrInvalidHash = errors.New("pwhash: invalid encoded hash")

// Hash hashes password with the default parameters.
func Hash(password string) (string, error) { return HashWith(password, Default) }

// HashWith hashes password with explicit parameters.
func HashWith(password string, p Params) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("pwhash: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads,
		b64.EncodeToString(salt), b64.EncodeToString(key),
	), nil
}

// Verify reports whether password matches the encoded Argon2id hash. The
// comparison is constant-time.
func Verify(password, encoded string) (bool, error) {
	p, salt, key, err := decode(encoded)
	if err != nil {
		return false, err
	}
	other := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(key, other) == 1, nil
}

// NeedsRehash reports whether an encoded hash used weaker parameters than want,
// so callers can transparently upgrade on the next successful login.
func NeedsRehash(encoded string, want Params) bool {
	p, _, _, err := decode(encoded)
	if err != nil {
		return true
	}
	return p.Memory < want.Memory || p.Time < want.Time || p.Threads < want.Threads
}

func decode(encoded string) (p Params, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, key]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err = fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return p, nil, nil, ErrInvalidHash
	}
	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if salt, err = b64.DecodeString(parts[4]); err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if key, err = b64.DecodeString(parts[5]); err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	p.SaltLen = uint32(len(salt))
	p.KeyLen = uint32(len(key))
	return p, salt, key, nil
}
