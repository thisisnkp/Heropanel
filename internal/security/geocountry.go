package security

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// Country geo-import: bulk-load a country's published CIDR ranges as allow or
// block entries, enforced through the same nftables set + apply/confirm/revert
// machinery as a single entry. A country is thousands of ranges; storing them
// as ordinary entries (tagged with the country) keeps rendering and removal
// uniform, and one set-membership test still covers them all.
//
// The ranges come from a **configured** URL template rather than a compiled-in
// mirror, so an operator can point at their own copy — and so the panel's only
// outbound reach for this is one the operator chose. Nothing is fetched unless
// an import is explicitly requested.

const (
	// geoMaxBytes caps a single zone download. The largest national aggregate is
	// a few hundred KiB; this leaves generous headroom while refusing a source
	// that would try to stream gigabytes into memory.
	geoMaxBytes = 16 << 20
	// geoMaxEntries caps how many CIDRs one import may add across both families,
	// a backstop against a misconfigured source or a "country" that is really the
	// whole internet.
	geoMaxEntries = 50000
)

// CountryImportInput requests a country import.
type CountryImportInput struct {
	Country string `json:"country"` // ISO 3166-1 alpha-2, case-insensitive
	Mode    string `json:"mode"`    // block (default) | allow
}

// CountrySummary is one imported country in the list view.
type CountrySummary struct {
	Country string `json:"country"`
	Mode    string `json:"mode"`
	Count   int    `json:"count"`
}

// ImportResult reports what an import stored.
type ImportResult struct {
	Country string `json:"country"`
	Mode    string `json:"mode"`
	Count   int    `json:"count"`
}

// GeoAvailable reports whether country import is configured (a source URL is
// set) on top of the firewall being available at all.
func (f *Firewall) GeoAvailable() bool {
	return f.Available() && (f.geoV4URL != "" || f.geoV6URL != "") && f.geoFetch != nil
}

// ListCountries returns the imported countries, each with its entry count, so
// the UI can show and manage an import as a unit rather than as thousands of
// rows.
func (f *Firewall) ListCountries(ctx context.Context) ([]CountrySummary, error) {
	if err := f.requireAvailable(); err != nil {
		return nil, err
	}
	entries, err := f.repo.ListIPEntries(ctx)
	if err != nil {
		return nil, err
	}
	type key struct{ cc, mode string }
	seen := map[key]int{}
	var order []key
	for _, e := range entries {
		if e.Country == "" {
			continue
		}
		k := key{e.Country, e.Mode}
		if _, ok := seen[k]; !ok {
			order = append(order, k)
		}
		seen[k]++
	}
	out := make([]CountrySummary, 0, len(order))
	for _, k := range order {
		out = append(out, CountrySummary{Country: k.cc, Mode: k.mode, Count: seen[k]})
	}
	return out, nil
}

// ImportCountry fetches a country's CIDR ranges and stores them, replacing any
// previous import of the same country. Like every other list change it does NOT
// apply — the operator applies (and confirms) the firewall explicitly, so a
// geo-block is never a side effect that could lock the box.
func (f *Firewall) ImportCountry(ctx context.Context, in CountryImportInput) (*ImportResult, error) {
	if err := f.requireAvailable(); err != nil {
		return nil, err
	}
	if !f.GeoAvailable() {
		return nil, errx.New(errx.KindUnavailable, "geo_unavailable",
			"Country import is not configured. Set a geo CIDR source URL.")
	}
	cc, err := normalizeCountry(in.Country)
	if err != nil {
		return nil, err
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "block"
	}
	if mode != "block" && mode != "allow" {
		return nil, errx.Validation("invalid_mode", "Mode must be block or allow.")
	}

	cidrs, err := f.fetchCountryCIDRs(ctx, cc)
	if err != nil {
		return nil, err
	}
	if len(cidrs) == 0 {
		return nil, errx.Validation("empty_country",
			"No CIDR ranges were found for that country — check the code and the source URL.")
	}
	if len(cidrs) > geoMaxEntries {
		return nil, errx.Validation("too_many_entries",
			fmt.Sprintf("That import has %d ranges, over the %d limit.", len(cidrs), geoMaxEntries))
	}

	entries := make([]*IPListEntry, 0, len(cidrs))
	comment := "country:" + cc
	for _, c := range cidrs {
		entries = append(entries, &IPListEntry{
			UID: idgen.NewULID(), CIDR: c, Mode: mode, Comment: comment, Country: cc,
		})
	}

	// Replace any previous import of this country, then insert the fresh set. A
	// re-import can flip mode or refresh ranges without leaving stale rows.
	if err := f.repo.DeleteIPEntriesByCountry(ctx, cc); err != nil {
		return nil, err
	}
	if err := f.repo.InsertIPEntries(ctx, entries); err != nil {
		return nil, err
	}
	return &ImportResult{Country: cc, Mode: mode, Count: len(entries)}, nil
}

// RemoveCountry drops a previously-imported country's entries (unapplied until
// the next firewall Apply).
func (f *Firewall) RemoveCountry(ctx context.Context, country string) error {
	if err := f.requireAvailable(); err != nil {
		return err
	}
	cc, err := normalizeCountry(country)
	if err != nil {
		return err
	}
	return f.repo.DeleteIPEntriesByCountry(ctx, cc)
}

// fetchCountryCIDRs pulls the IPv4 and IPv6 zone files for the country and
// returns the valid, de-duplicated CIDRs. A configured-but-missing family URL
// is simply skipped; both missing was already ruled out by GeoAvailable.
func (f *Firewall) fetchCountryCIDRs(ctx context.Context, cc string) ([]string, error) {
	lower := strings.ToLower(cc)
	seen := map[string]struct{}{}
	var out []string
	for _, tmpl := range []string{f.geoV4URL, f.geoV6URL} {
		if tmpl == "" {
			continue
		}
		url := fmt.Sprintf(tmpl, lower)
		body, err := f.geoFetch(ctx, url)
		if err != nil {
			return nil, err
		}
		for _, c := range parseZoneCIDRs(body) {
			if _, dup := seen[c]; dup {
				continue
			}
			seen[c] = struct{}{}
			out = append(out, c)
		}
	}
	return out, nil
}

// parseZoneCIDRs reads a one-CIDR-per-line zone file, skipping blanks, comments
// (# or ;) and anything that is not a parseable CIDR. The canonical network
// form is stored so a set never carries a host-bits-set duplicate of a range.
func parseZoneCIDRs(body []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		// Tolerate a trailing comment on the line.
		if i := strings.IndexAny(line, " \t#;"); i >= 0 {
			line = line[:i]
		}
		if _, ipnet, err := net.ParseCIDR(line); err == nil {
			out = append(out, ipnet.String())
		}
	}
	return out
}

// httpFetch is the default network fetcher: a bounded GET. Tests inject their
// own to avoid the network entirely.
func (f *Firewall) httpFetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errx.Validation("invalid_source_url", "The geo source URL is not valid.")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errx.New(errx.KindUnavailable, "geo_fetch_failed",
			"Could not fetch the country ranges from the configured source.")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		// A country may publish ranges for only one address family; the missing
		// family's file legitimately 404s. Treat it as "no ranges here" rather
		// than failing the whole import — an entirely-unknown country then falls
		// out as an empty result the caller reports.
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errx.New(errx.KindUnavailable, "geo_fetch_failed",
			fmt.Sprintf("The geo source returned HTTP %d.", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, geoMaxBytes))
	if err != nil {
		return nil, errx.New(errx.KindUnavailable, "geo_fetch_failed",
			"Could not read the country ranges from the source.")
	}
	return body, nil
}

// normalizeCountry validates and uppercases an ISO 3166-1 alpha-2 code.
func normalizeCountry(cc string) (string, error) {
	cc = strings.ToUpper(strings.TrimSpace(cc))
	if len(cc) != 2 || cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z' {
		return "", errx.Validation("invalid_country", "Enter a two-letter ISO country code.")
	}
	return cc, nil
}
