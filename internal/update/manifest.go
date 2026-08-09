// Package update moves a running installation to another release.
//
// The panel could already be *installed* — np-installer verifies binaries
// against a signed SHA256SUMS manifest, writes units, health-checks and rolls
// back — but there was no way to move an installed panel forward. This package
// is the npd half of that: it discovers what a channel currently offers,
// downloads and verifies it, and hands the verified artifacts to the privileged
// half.
//
// # Why npd does not do the swap
//
// A self-update has to replace np-broker, the root component, and the HTTP
// request that triggers it is served through the very processes being replaced.
// npd therefore never touches /opt/nexpanel/bin. It stages verified artifacts
// under its own data directory and asks the broker for one narrow capability
// (panel.update), which starts np-installer as a **detached systemd transient
// unit**. That unit is owned by systemd, not by the broker, so it outlives both
// restarts — the only arrangement in which the thing performing the swap is not
// also the thing being swapped. See docs/26-self-update.md.
//
// # Trust
//
// Two signed documents, one key. `channels.json` says *which* version a channel
// is on; `SHA256SUMS` says *what bytes* that version consists of. Both are
// ed25519-signed by the release key the operator already pins as
// NP_RELEASE_PUBKEY, and the second chain is verified with the exact code
// np-installer uses at install time (installer.VerifyDetachedSig /
// VerifyChecksum) rather than a second implementation of it. With no key
// configured the feature is off — an update source with no anchor would let
// whoever serves that URL replace the root component on this host.
package update

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/thisisnkp/nexpanel/internal/installer"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Channel names. A panel follows exactly one.
const (
	ChannelStable  = "stable"
	ChannelBeta    = "beta"
	ChannelNightly = "nightly"
)

// ValidChannel reports whether name is a channel this panel understands. An
// unknown channel is refused rather than silently treated as stable: quietly
// putting a host on a different release line than the operator asked for is
// worse than telling them the name was wrong.
func ValidChannel(name string) bool {
	switch name {
	case ChannelStable, ChannelBeta, ChannelNightly:
		return true
	}
	return false
}

// ChannelRelease is what one channel currently offers.
type ChannelRelease struct {
	Version string `json:"version"`
	// Notes is an optional short human summary shown beside the update button.
	Notes string `json:"notes,omitempty"`
	// ReleasedAt is informational (RFC 3339); nothing keys off it.
	ReleasedAt string `json:"released_at,omitempty"`
}

// Manifest is the parsed `channels.json`: which version each channel is on.
// Deliberately tiny. It answers one question — "what should I be running?" —
// and leaves "what is that made of" to the release's own SHA256SUMS, so the
// two concerns keep separate signatures and neither has to be re-cut when the
// other changes.
type Manifest struct {
	Channels map[string]ChannelRelease `json:"channels"`
}

// Release returns the release a channel currently offers.
func (m Manifest) Release(channel string) (ChannelRelease, error) {
	r, ok := m.Channels[channel]
	if !ok {
		return ChannelRelease{}, errx.New(errx.KindNotFound, "unknown_channel",
			fmt.Sprintf("The release manifest has no %q channel.", channel))
	}
	if strings.TrimSpace(r.Version) == "" {
		return ChannelRelease{}, errx.New(errx.KindNotFound, "empty_channel",
			fmt.Sprintf("The %q channel names no version.", channel))
	}
	return r, nil
}

// Names lists the channels the manifest offers, sorted, for an error message
// that tells the operator what they could have asked for.
func (m Manifest) Names() []string {
	out := make([]string, 0, len(m.Channels))
	for k := range m.Channels {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ParseManifest verifies a detached signature over the raw channels.json bytes
// and then parses them.
//
// Order matters and is the whole point: the signature is checked against the
// bytes as received, *before* they are interpreted. Parsing first would run a
// JSON decoder over attacker-controlled input and then verify a re-encoding of
// it, which is how signature-bypass bugs are built.
func ParseManifest(raw []byte, sigB64 string, pub ed25519.PublicKey) (Manifest, error) {
	if err := installer.VerifyDetachedSig(raw, sigB64, pub); err != nil {
		return Manifest{}, errx.New(errx.KindValidation, "manifest_untrusted",
			"The release manifest is not signed by the pinned release key: "+err.Error())
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, errx.New(errx.KindValidation, "manifest_malformed",
			"The release manifest is signed but is not valid JSON: "+err.Error())
	}
	if len(m.Channels) == 0 {
		return Manifest{}, errx.New(errx.KindValidation, "manifest_empty",
			"The release manifest names no channels.")
	}
	return m, nil
}
