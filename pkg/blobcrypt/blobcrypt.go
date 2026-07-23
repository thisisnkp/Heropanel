// Package blobcrypt seals large files (backups) with chunked AES-256-GCM.
//
// Why not one big GCM seal: GCM authenticates on Open, which for a single blob
// means buffering the entire file in memory before a single byte can be trusted
// — unusable for a multi-gigabyte backup. And why not a raw CTR stream: without
// authentication a flipped ciphertext bit silently flips a restored byte.
//
// So the file is sealed as a sequence of bounded chunks, each individually
// AEAD-authenticated (the STREAM construction): a random nonce prefix is fixed
// per file, each chunk's nonce appends a counter and a last-chunk flag. The
// counter makes reordering and replay fail authentication; the flag makes
// truncation fail — a file cut off after a valid chunk is detected, because the
// final chunk was not marked final.
//
// Format:
//
//	magic "HPB1" | nonce prefix (7 bytes) | chunks…
//	chunk: ciphertext length (uint32 BE) | GCM(nonce=prefix||counter||flag, plaintext)
package blobcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
)

const (
	magic = "HPB1"
	// chunkSize bounds plaintext per chunk. 64 KiB keeps memory flat regardless
	// of file size while amortising the 16-byte GCM tag to ~0.02 % overhead.
	chunkSize   = 64 * 1024
	prefixLen   = 7
	counterLen  = 4
	flagLen     = 1
	nonceLen    = prefixLen + counterLen + flagLen // 12, GCM's standard size
	lastFlag    = 0x01
	notLastFlag = 0x00
)

// ErrCorrupt is returned when authentication fails anywhere: a tampered chunk, a
// reordered chunk, or a truncated file. Deliberately one error — which byte went
// wrong is not something an attacker should learn.
var ErrCorrupt = errors.New("blobcrypt: data is corrupt or was tampered with")

// aeadFor builds the AEAD from a 32-byte key.
func aeadFor(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("blobcrypt: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunkNonce(prefix []byte, counter uint32, last bool) []byte {
	nonce := make([]byte, nonceLen)
	copy(nonce, prefix)
	binary.BigEndian.PutUint32(nonce[prefixLen:], counter)
	if last {
		nonce[nonceLen-1] = lastFlag
	} else {
		nonce[nonceLen-1] = notLastFlag
	}
	return nonce
}

// Seal encrypts r to w. The caller supplies the 32-byte key (derive it from the
// panel's master key; this package does no key management).
func Seal(w io.Writer, r io.Reader, key []byte) error {
	aead, err := aeadFor(key)
	if err != nil {
		return err
	}
	prefix := make([]byte, prefixLen)
	if _, err := rand.Read(prefix); err != nil {
		return err
	}
	if _, err := w.Write([]byte(magic)); err != nil {
		return err
	}
	if _, err := w.Write(prefix); err != nil {
		return err
	}

	buf := make([]byte, chunkSize)
	var counter uint32
	var pending []byte // one chunk of lookahead, so the LAST chunk can be flagged
	pendingSet := false

	flush := func(chunk []byte, last bool) error {
		ct := aead.Seal(nil, chunkNonce(prefix, counter, last), chunk, nil)
		counter++
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(ct)))
		if _, err := w.Write(lenBuf[:]); err != nil {
			return err
		}
		_, err := w.Write(ct)
		return err
	}

	for {
		n, rerr := io.ReadFull(r, buf)
		if n > 0 {
			if pendingSet {
				if err := flush(pending, false); err != nil {
					return err
				}
			}
			pending = append(pending[:0], buf[:n]...)
			pendingSet = true
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if !pendingSet {
		pending = []byte{} // an empty file still gets one (empty, final) chunk
	}
	return flush(pending, true)
}

// Open decrypts r to w, verifying every chunk. Any tampering, reordering or
// truncation yields ErrCorrupt; bytes are only written after their chunk
// authenticates, so a partially-written output is still all-authentic.
func Open(w io.Writer, r io.Reader, key []byte) error {
	aead, err := aeadFor(key)
	if err != nil {
		return err
	}
	head := make([]byte, len(magic)+prefixLen)
	if _, err := io.ReadFull(r, head); err != nil {
		return ErrCorrupt
	}
	if string(head[:len(magic)]) != magic {
		return ErrCorrupt
	}
	prefix := head[len(magic):]

	var counter uint32
	var lenBuf [4]byte
	for {
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			// EOF here means the previous chunk claimed not to be last, or the
			// stream ended mid-header: truncation either way.
			return ErrCorrupt
		}
		n := binary.BigEndian.Uint32(lenBuf[:])
		if n > chunkSize+uint32(aead.Overhead()) {
			return ErrCorrupt
		}
		ct := make([]byte, n)
		if _, err := io.ReadFull(r, ct); err != nil {
			return ErrCorrupt
		}

		// Try as a non-final chunk first, then as the final one.
		pt, err := aead.Open(nil, chunkNonce(prefix, counter, false), ct, nil)
		last := false
		if err != nil {
			pt, err = aead.Open(nil, chunkNonce(prefix, counter, true), ct, nil)
			if err != nil {
				return ErrCorrupt
			}
			last = true
		}
		counter++
		if _, err := w.Write(pt); err != nil {
			return err
		}
		if last {
			// Anything after the final chunk is garbage appended to the file.
			var one [1]byte
			if _, err := r.Read(one[:]); err != io.EOF {
				return ErrCorrupt
			}
			return nil
		}
	}
}
