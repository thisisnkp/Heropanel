package setup

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// tempSlugBytes is how much randomness goes into a temporary hostname's label.
// Three bytes is six hex characters — short enough to read back over a phone
// and to fit in a browser tab, and 16.7M values is far more than enough for a
// name whose uniqueness is enforced anyway: the site-create path rejects a
// duplicate primary domain, so a collision costs one retry rather than data.
const tempSlugBytes = 3

// TempDomainPrefix labels a hostname the panel minted rather than one the
// operator owns. It is a visible marker on purpose — an address that reads
// "site-" is obviously disposable, which is what stops one being treated as
// permanent and then relied on.
const TempDomainPrefix = "site-"

// SuggestTempDomain mints a throwaway hostname under base, e.g.
// "site-k3f9a2.panel.example.com". It only *suggests*: nothing is reserved and
// no DNS is written, so two callers can hold the same suggestion and the loser
// is rejected at create time. That is deliberate — reserving names would need a
// table and an expiry sweep to avoid leaking every abandoned form.
//
// base must already be normalized and validated (Selection.Validate does this);
// an empty base yields an empty result, which callers treat as "not configured".
func SuggestTempDomain(base string) (string, error) {
	if base == "" {
		return "", nil
	}
	b := make([]byte, tempSlugBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return TempDomainPrefix + hex.EncodeToString(b) + "." + base, nil
}

// WildcardFor returns the DNS name that must resolve to this host for any
// temporary address under base to work. Every minted hostname is a fresh label,
// so a wildcard is the only record that covers them all — the alternative is a
// per-site A record written before the site exists.
func WildcardFor(base string) string {
	if base == "" {
		return ""
	}
	return "*." + base
}

// IsTempDomain reports whether fqdn looks like one of this panel's minted
// addresses under base.
func IsTempDomain(fqdn, base string) bool {
	if base == "" {
		return false
	}
	suffix := "." + base
	if !strings.HasSuffix(fqdn, suffix) {
		return false
	}
	return strings.HasPrefix(strings.TrimSuffix(fqdn, suffix), TempDomainPrefix)
}
