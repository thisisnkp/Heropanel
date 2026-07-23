// Package secrets seals small secret values — API tokens, deploy keys — for
// storage in the `*_enc` columns described in docs/05 §6.
//
// Each value is encrypted with AES-256-GCM under a key derived from the panel's
// master key, with a fresh random nonce per record and the record's identity
// bound in as additional authenticated data (AAD). The AAD binding is what stops
// a swap attack: a ciphertext lifted out of one row and pasted into another —
// or into a different column of the same row — fails to open rather than
// silently decrypting into the wrong record's secret.
//
// The stored form is `hp1.<base64url(nonce||ciphertext||tag)>`. The version
// prefix is deliberate: it reserves room for the rotating data-key envelope from
// docs/05 §6, which is not implemented yet (see the package's README note in
// docs/11 §1). Today a single key is derived from the master key via HKDF, so
// rotation means re-encrypting rows rather than re-wrapping data keys.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// version is the stored-blob format tag.
const version = "hp1"

// hkdfInfo separates this key from any other key derived from the same master.
const hkdfInfo = "heropanel/secrets/v1"

// MasterKeyLen is the required master key length in bytes (AES-256).
const MasterKeyLen = 32

// ErrNoCipher reports that encryption was attempted without a configured master
// key. Callers surface this as "unavailable", never as a reason to store the
// value in the clear.
var ErrNoCipher = errors.New("secrets: no master key configured")

// Cipher seals and opens secret values. The zero value is unusable; construct
// with New or FromBase64. A nil *Cipher is valid to hold and reports ErrNoCipher
// from Seal/Open, so callers can wire an unconfigured panel without nil checks
// at every use site.
type Cipher struct {
	aead cipher.AEAD
}

// New derives a Cipher from a master key, which must be exactly MasterKeyLen
// bytes of cryptographically random data.
func New(masterKey []byte) (*Cipher, error) {
	if len(masterKey) != MasterKeyLen {
		return nil, fmt.Errorf("secrets: master key must be %d bytes, got %d", MasterKeyLen, len(masterKey))
	}
	key, err := hkdf.Key(sha256.New, masterKey, nil, hkdfInfo, MasterKeyLen)
	if err != nil {
		return nil, fmt.Errorf("secrets: derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// FromBase64 derives a Cipher from a base64-encoded (standard or raw, padded or
// not) 32-byte master key — the form carried in secrets.env / HP_SECRET_KEY.
// An empty string returns (nil, nil): secrets are simply not configured, which
// is a supported state for a panel that stores none.
func FromBase64(encoded string) (*Cipher, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil
	}
	raw, err := decodeBase64(encoded)
	if err != nil {
		return nil, fmt.Errorf("secrets: master key is not valid base64: %w", err)
	}
	return New(raw)
}

// GenerateMasterKey returns a new base64-encoded master key, for the installer
// to write into secrets.env.
func GenerateMasterKey() (string, error) {
	b := make([]byte, MasterKeyLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("secrets: generate master key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// Configured reports whether c can actually seal and open values.
func (c *Cipher) Configured() bool { return c != nil && c.aead != nil }

// DeriveKeyBase64 derives a purpose-specific 32-byte subkey from the encoded
// master key (HKDF with the purpose as info). Different purposes yield unrelated
// keys, so the backup stream key and the column-sealing key can never be
// confused for one another. An empty master key returns (nil, nil) — the
// feature is then unavailable, same contract as FromBase64.
func DeriveKeyBase64(encoded, purpose string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil
	}
	raw, err := decodeBase64(encoded)
	if err != nil {
		return nil, fmt.Errorf("secrets: master key is not valid base64: %w", err)
	}
	if len(raw) != MasterKeyLen {
		return nil, fmt.Errorf("secrets: master key must be %d bytes, got %d", MasterKeyLen, len(raw))
	}
	key, err := hkdf.Key(sha256.New, raw, nil, "heropanel/"+purpose, MasterKeyLen)
	if err != nil {
		return nil, fmt.Errorf("secrets: derive key: %w", err)
	}
	return key, nil
}

// Seal encrypts plaintext and returns its stored form. aad must identify the
// record the value belongs to — build it with AAD.
func (c *Cipher) Seal(plaintext []byte, aad string) (string, error) {
	if !c.Configured() {
		return "", ErrNoCipher
	}
	if aad == "" {
		return "", errors.New("secrets: aad is required")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secrets: nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, []byte(aad))
	return version + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open decrypts a stored blob. It fails if the blob was tampered with, was
// sealed under a different key, or belongs to a different record than aad names.
func (c *Cipher) Open(blob, aad string) ([]byte, error) {
	if !c.Configured() {
		return nil, ErrNoCipher
	}
	tag, body, ok := strings.Cut(blob, ".")
	if !ok || tag != version {
		return nil, errors.New("secrets: unrecognized ciphertext format")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, errors.New("secrets: ciphertext is not valid base64")
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns+c.aead.Overhead() {
		return nil, errors.New("secrets: ciphertext is too short")
	}
	plaintext, err := c.aead.Open(nil, raw[:ns], raw[ns:], []byte(aad))
	if err != nil {
		// Deliberately opaque: never leak whether the key, the AAD, or the bytes
		// were wrong.
		return nil, errors.New("secrets: could not decrypt")
	}
	return plaintext, nil
}

// AAD builds the additional-authenticated-data string binding a ciphertext to
// one column of one row, e.g. AAD("git_sources", 7, "credential_enc").
func AAD(table string, id int64, column string) string {
	return table + ":" + strconv.FormatInt(id, 10) + ":" + column
}

// decodeBase64 accepts the four common spellings so an operator pasting a key
// out of any tool gets a working panel rather than a startup error.
func decodeBase64(s string) ([]byte, error) {
	encs := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	}
	var err error
	for _, enc := range encs {
		var b []byte
		if b, err = enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, err
}
