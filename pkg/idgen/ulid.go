// Package idgen generates HeroPanel identifiers. External IDs are ULIDs
// (lexicographically sortable, time-ordered, non-enumerable) rendered as 26
// Crockford base32 characters, optionally with a short type prefix such as
// "sit_" for sites or "job_" for jobs (see docs/03-database-schema.md).
//
// This is a dependency-free implementation (crypto/rand only) so pkg/ stays
// lightweight and builds offline.
package idgen

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// crockford is the ULID base32 alphabet (excludes I, L, O, U).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewULID returns a new 26-character ULID string: a 48-bit millisecond
// timestamp followed by 80 bits of cryptographic randomness.
func NewULID() string {
	var id [16]byte

	ms := uint64(time.Now().UnixMilli())
	// 48-bit big-endian timestamp in id[0:6].
	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)

	// 80 bits of randomness in id[6:16].
	if _, err := rand.Read(id[6:]); err != nil {
		// crypto/rand should never fail; fall back to time-derived entropy so
		// callers still receive a unique-ish, valid ULID rather than a panic.
		binary.BigEndian.PutUint64(id[6:14], uint64(time.Now().UnixNano()))
	}

	return encode(id)
}

// NewPrefixed returns a prefixed ULID, e.g. NewPrefixed("sit") -> "sit_01J...".
func NewPrefixed(prefix string) string {
	return prefix + "_" + NewULID()
}

// encode renders 16 bytes as 26 Crockford base32 characters using the canonical
// ULID bit layout.
func encode(id [16]byte) string {
	var d [26]byte
	d[0] = crockford[(id[0]&224)>>5]
	d[1] = crockford[id[0]&31]
	d[2] = crockford[(id[1]&248)>>3]
	d[3] = crockford[((id[1]&7)<<2)|((id[2]&192)>>6)]
	d[4] = crockford[(id[2]&62)>>1]
	d[5] = crockford[((id[2]&1)<<4)|((id[3]&240)>>4)]
	d[6] = crockford[((id[3]&15)<<1)|((id[4]&128)>>7)]
	d[7] = crockford[(id[4]&124)>>2]
	d[8] = crockford[((id[4]&3)<<3)|((id[5]&224)>>5)]
	d[9] = crockford[id[5]&31]
	d[10] = crockford[(id[6]&248)>>3]
	d[11] = crockford[((id[6]&7)<<2)|((id[7]&192)>>6)]
	d[12] = crockford[(id[7]&62)>>1]
	d[13] = crockford[((id[7]&1)<<4)|((id[8]&240)>>4)]
	d[14] = crockford[((id[8]&15)<<1)|((id[9]&128)>>7)]
	d[15] = crockford[(id[9]&124)>>2]
	d[16] = crockford[((id[9]&3)<<3)|((id[10]&224)>>5)]
	d[17] = crockford[id[10]&31]
	d[18] = crockford[(id[11]&248)>>3]
	d[19] = crockford[((id[11]&7)<<2)|((id[12]&192)>>6)]
	d[20] = crockford[(id[12]&62)>>1]
	d[21] = crockford[((id[12]&1)<<4)|((id[13]&240)>>4)]
	d[22] = crockford[((id[13]&15)<<1)|((id[14]&128)>>7)]
	d[23] = crockford[(id[14]&124)>>2]
	d[24] = crockford[((id[14]&3)<<3)|((id[15]&224)>>5)]
	d[25] = crockford[id[15]&31]
	return string(d[:])
}
