// Package totp implements RFC 6238 time-based one-time passwords (SHA-1, 6
// digits, 30-second period) using only the standard library. Used for MFA.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	period = 30
	digits = 6
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret returns a new base32-encoded secret (160 bits).
func GenerateSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

// Code returns the current TOTP code for a secret.
func Code(secret string) (string, error) {
	return codeAt(secret, time.Now())
}

func codeAt(secret string, t time.Time) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	return hotp(key, uint64(t.Unix()/period)), nil
}

// Validate reports whether code matches the secret within +/- one time step
// (tolerating minor clock skew). The comparison is constant-time.
func Validate(secret, code string) bool {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(code) != digits {
		return false
	}
	counter := uint64(time.Now().Unix() / period)
	for _, w := range []int64{-1, 0, 1} {
		want := hotp(key, uint64(int64(counter)+w))
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// ProvisioningURI returns an otpauth:// URI for QR provisioning.
func ProvisioningURI(secret, account, issuer string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", digits))
	q.Set("period", fmt.Sprintf("%d", period))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// hotp is the HMAC-based OTP (RFC 4226) with dynamic truncation.
func hotp(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	val := (int(sum[offset]&0x7f) << 24) |
		(int(sum[offset+1]) << 16) |
		(int(sum[offset+2]) << 8) |
		int(sum[offset+3])
	return fmt.Sprintf("%0*d", digits, val%1_000_000)
}
