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

// Rotating data keys — the envelope docs/05 §6 reserved room for.
//
// Instead of sealing every `*_enc` column directly under a key derived from the
// master (the `np1` format), a panel can hold a **keyring**: a set of random data
// keys, each *wrapped* (sealed) by the master key and stored in the data_keys
// table. One generation is active; new values seal under it as `np2.<gen>.<blob>`
// and record which generation they used, so any older generation's values still
// open. This buys two rotations:
//
//   - **Master rotation** is cheap: re-wrap the handful of data keys under a new
//     master (Rewrap) — the many row blobs are untouched, because they are sealed
//     under data keys, not the master.
//   - **Data-key rotation** adds a new active generation (Rotate); new writes use
//     it immediately, and old rows migrate lazily (any update reseals under the
//     active key) or via a re-encrypt sweep.
//
// A panel that never rotates has no keyring and keeps producing `np1` blobs —
// fully backward compatible.

const (
	versionKeyed = "np2"                 // np2.<gen>.<base64> — sealed under data key <gen>
	keyWrapInfo  = "nexpanel/keywrap/v1" // HKDF info for the master's key-wrapping subkey
	dataKeyLen   = 32                    // AES-256 data keys
)

// WrappedKey is a data key sealed under the master, as persisted in data_keys.
type WrappedKey struct {
	Generation int    `json:"generation"`
	Wrapped    string `json:"wrapped"` // base64url(nonce||ciphertext||tag)
}

// dataKey is an unwrapped data key held in memory: raw bytes (to re-wrap on
// master rotation) plus its ready AEAD.
type dataKey struct {
	raw  []byte
	aead cipher.AEAD
}

// keyWrapAEAD derives the master's key-wrapping AEAD (distinct from the np1
// column-sealing key, so the two purposes never share key material).
func keyWrapAEAD(master []byte) (cipher.AEAD, error) {
	k, err := hkdf.Key(sha256.New, master, nil, keyWrapInfo, MasterKeyLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func newAEADFromKey(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// LoadKeyring unwraps the given data keys with the master and installs them,
// making the highest generation the active one for new seals. Called at startup
// with the rows from the data_keys table. Passing no keys leaves the Cipher in
// legacy (np1) mode.
func (c *Cipher) LoadKeyring(wrapped []WrappedKey) error {
	if !c.Configured() {
		if len(wrapped) == 0 {
			return nil
		}
		return ErrNoCipher
	}
	if c.wrap == nil {
		return errors.New("secrets: cipher has no key-wrapping key (constructed without a master)")
	}
	keys := map[int]dataKey{}
	active := 0
	for _, w := range wrapped {
		raw, err := c.unwrapKey(w.Wrapped, w.Generation)
		if err != nil {
			return fmt.Errorf("secrets: unwrap data key gen %d: %w", w.Generation, err)
		}
		aead, err := newAEADFromKey(raw)
		if err != nil {
			return err
		}
		keys[w.Generation] = dataKey{raw: raw, aead: aead}
		if w.Generation > active {
			active = w.Generation
		}
	}
	c.dataKeys = keys
	c.active = active
	return nil
}

// keyWrapAAD binds a wrapped data key to its generation, so a wrapped key lifted
// to a different generation slot fails to unwrap.
func keyWrapAAD(gen int) []byte { return []byte("nexpanel/datakey/" + strconv.Itoa(gen)) }

func (c *Cipher) unwrapKey(wrapped string, gen int) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, errors.New("wrapped key is not valid base64")
	}
	ns := c.wrap.NonceSize()
	if len(raw) < ns+c.wrap.Overhead() {
		return nil, errors.New("wrapped key too short")
	}
	return c.wrap.Open(nil, raw[:ns], raw[ns:], keyWrapAAD(gen))
}

func (c *Cipher) wrapKey(raw []byte, gen int) (string, error) {
	nonce := make([]byte, c.wrap.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.wrap.Seal(nonce, nonce, raw, keyWrapAAD(gen))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Rotate mints a fresh data key as a new generation (active+1), installs it as
// the active key, and returns it wrapped for the caller to persist. New seals use
// it immediately; existing blobs keep opening under their own generation.
func (c *Cipher) Rotate() (WrappedKey, error) {
	if !c.Configured() || c.wrap == nil {
		return WrappedKey{}, ErrNoCipher
	}
	raw := make([]byte, dataKeyLen)
	if _, err := rand.Read(raw); err != nil {
		return WrappedKey{}, err
	}
	gen := c.active + 1
	wrapped, err := c.wrapKey(raw, gen)
	if err != nil {
		return WrappedKey{}, err
	}
	aead, err := newAEADFromKey(raw)
	if err != nil {
		return WrappedKey{}, err
	}
	if c.dataKeys == nil {
		c.dataKeys = map[int]dataKey{}
	}
	c.dataKeys[gen] = dataKey{raw: raw, aead: aead}
	c.active = gen
	return WrappedKey{Generation: gen, Wrapped: wrapped}, nil
}

// Rewrap re-wraps every data key under a new master, returning the new wrapped
// set to persist. This is master rotation: the row blobs are untouched (they are
// sealed under data keys), so only these few keys change. The Cipher itself is
// not switched to the new master here — the caller rebuilds it from the new
// master + returned keys after persisting.
func (c *Cipher) Rewrap(newMaster []byte) ([]WrappedKey, error) {
	if !c.Configured() {
		return nil, ErrNoCipher
	}
	if len(newMaster) != MasterKeyLen {
		return nil, fmt.Errorf("secrets: new master key must be %d bytes, got %d", MasterKeyLen, len(newMaster))
	}
	nw, err := keyWrapAEAD(newMaster)
	if err != nil {
		return nil, err
	}
	out := make([]WrappedKey, 0, len(c.dataKeys))
	for gen, dk := range c.dataKeys {
		nonce := make([]byte, nw.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return nil, err
		}
		sealed := nw.Seal(nonce, nonce, dk.raw, keyWrapAAD(gen))
		out = append(out, WrappedKey{Generation: gen, Wrapped: base64.RawURLEncoding.EncodeToString(sealed)})
	}
	return out, nil
}

// ActiveGeneration reports the active data-key generation (0 => legacy np1).
func (c *Cipher) ActiveGeneration() int {
	if c == nil {
		return 0
	}
	return c.active
}

// Reseal opens a blob and re-seals it under the active key, returning the new
// blob and whether it changed. Used by a re-encrypt sweep to migrate old-
// generation (or legacy np1) blobs onto the current data key.
func (c *Cipher) Reseal(blob, aad string) (string, bool, error) {
	pt, err := c.Open(blob, aad)
	if err != nil {
		return "", false, err
	}
	// Already on the active generation? Leave it.
	if gen := blobGeneration(blob); gen == c.active {
		return blob, false, nil
	}
	nb, err := c.Seal(pt, aad)
	if err != nil {
		return "", false, err
	}
	return nb, true, nil
}

// blobGeneration returns the generation a stored blob was sealed under: 0 for the
// legacy np1 format, or the embedded generation for np2.
func blobGeneration(blob string) int {
	if strings.HasPrefix(blob, versionKeyed+".") {
		rest := blob[len(versionKeyed)+1:]
		if i := strings.IndexByte(rest, '.'); i > 0 {
			if n, err := strconv.Atoi(rest[:i]); err == nil {
				return n
			}
		}
	}
	return 0
}
