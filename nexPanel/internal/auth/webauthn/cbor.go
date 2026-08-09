package webauthn

import (
	"encoding/binary"
	"errors"
	"math"
)

// A deliberately tiny CBOR (RFC 8949) reader — only the subset WebAuthn uses:
// unsigned/negative integers, byte and text strings, arrays, maps, and the
// three simple values. No floats, no streaming, no tags beyond skip. Keeping
// it to exactly what the attestation object and COSE keys contain means the
// parser this security path depends on is a couple of hundred lines that can
// be read in full, not a general-purpose library's whole surface.
//
// Decoded shapes:
//   - unsigned int   -> uint64
//   - negative int   -> int64
//   - byte string    -> []byte
//   - text string    -> string
//   - array          -> []any
//   - map            -> map[any]any   (keys are int64 or string)
//   - false/true/null-> bool / nil

var errCBOR = errors.New("webauthn: malformed CBOR")

// cborDecode decodes one item from b, returning the value and the unread rest.
func cborDecode(b []byte) (any, []byte, error) {
	if len(b) == 0 {
		return nil, nil, errCBOR
	}
	major := b[0] >> 5
	minor := b[0] & 0x1f
	b = b[1:]

	switch major {
	case 0: // unsigned int
		n, rest, err := cborUint(minor, b)
		return n, rest, err
	case 1: // negative int: -1 - n
		n, rest, err := cborUint(minor, b)
		if err != nil {
			return nil, nil, err
		}
		if n > math.MaxInt64 {
			return nil, nil, errCBOR
		}
		return -1 - int64(n), rest, nil
	case 2: // byte string
		n, rest, err := cborUint(minor, b)
		if err != nil {
			return nil, nil, err
		}
		if uint64(len(rest)) < n {
			return nil, nil, errCBOR
		}
		return append([]byte(nil), rest[:n]...), rest[n:], nil
	case 3: // text string
		n, rest, err := cborUint(minor, b)
		if err != nil {
			return nil, nil, err
		}
		if uint64(len(rest)) < n {
			return nil, nil, errCBOR
		}
		return string(rest[:n]), rest[n:], nil
	case 4: // array
		n, rest, err := cborUint(minor, b)
		if err != nil {
			return nil, nil, err
		}
		out := make([]any, 0, n)
		for i := uint64(0); i < n; i++ {
			var v any
			v, rest, err = cborDecode(rest)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, v)
		}
		return out, rest, nil
	case 5: // map
		n, rest, err := cborUint(minor, b)
		if err != nil {
			return nil, nil, err
		}
		out := make(map[any]any, n)
		for i := uint64(0); i < n; i++ {
			var k, v any
			k, rest, err = cborDecode(rest)
			if err != nil {
				return nil, nil, err
			}
			v, rest, err = cborDecode(rest)
			if err != nil {
				return nil, nil, err
			}
			out[normalizeKey(k)] = v
		}
		return out, rest, nil
	case 7: // simple values
		switch minor {
		case 20:
			return false, b, nil
		case 21:
			return true, b, nil
		case 22, 23: // null / undefined
			return nil, b, nil
		}
	}
	return nil, nil, errCBOR
}

// cborUint reads the argument encoded by the additional-info bits.
func cborUint(minor byte, b []byte) (uint64, []byte, error) {
	switch {
	case minor < 24:
		return uint64(minor), b, nil
	case minor == 24:
		if len(b) < 1 {
			return 0, nil, errCBOR
		}
		return uint64(b[0]), b[1:], nil
	case minor == 25:
		if len(b) < 2 {
			return 0, nil, errCBOR
		}
		return uint64(binary.BigEndian.Uint16(b)), b[2:], nil
	case minor == 26:
		if len(b) < 4 {
			return 0, nil, errCBOR
		}
		return uint64(binary.BigEndian.Uint32(b)), b[4:], nil
	case minor == 27:
		if len(b) < 8 {
			return 0, nil, errCBOR
		}
		return binary.BigEndian.Uint64(b), b[8:], nil
	}
	return 0, nil, errCBOR
}

// normalizeKey folds unsigned map keys to int64 so COSE's mix of positive and
// negative integer labels can be looked up with one key type.
func normalizeKey(k any) any {
	if u, ok := k.(uint64); ok && u <= math.MaxInt64 {
		return int64(u)
	}
	return k
}
