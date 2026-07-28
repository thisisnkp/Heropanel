package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// geoServer serves per-country zone files at /v4/<cc> and /v6/<cc>, so the
// import can be driven end to end without touching the real network.
func geoServer(t *testing.T, v4, v6 map[string]string) (*Firewall, *memFW, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var set map[string]string
		var cc string
		switch {
		case strings.HasPrefix(r.URL.Path, "/v4/"):
			set, cc = v4, strings.TrimPrefix(r.URL.Path, "/v4/")
		case strings.HasPrefix(r.URL.Path, "/v6/"):
			set, cc = v6, strings.TrimPrefix(r.URL.Path, "/v6/")
		}
		body, ok := set[cc]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	f, repo, _ := newFW(t)
	f = f.WithGeoSource(srv.URL+"/v4/%s", srv.URL+"/v6/%s")
	return f, repo, srv
}

func TestImportCountry(t *testing.T) {
	v4 := map[string]string{
		"np": "# Nepal\n1.0.0.0/24\n2.2.0.0/16\n1.0.0.0/24\ngarbage-line\n",
	}
	v6 := map[string]string{
		"np": "2400:abcd::/32\n",
	}
	f, repo, _ := geoServer(t, v4, v6)

	res, err := f.ImportCountry(context.Background(), CountryImportInput{Country: "np", Mode: "block"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	// 2 unique v4 (dupe dropped, garbage skipped, comment skipped) + 1 v6.
	if res.Count != 3 || res.Country != "NP" || res.Mode != "block" {
		t.Fatalf("summary = %+v, want 3 entries for NP/block", res)
	}
	entries, _ := repo.ListIPEntries(context.Background())
	if len(entries) != 3 {
		t.Fatalf("stored %d entries, want 3", len(entries))
	}
	for _, e := range entries {
		if e.Country != "NP" || e.Mode != "block" {
			t.Fatalf("entry not tagged: %+v", e)
		}
	}
}

func TestImportCountryReplaces(t *testing.T) {
	v4 := map[string]string{"np": "1.0.0.0/24\n2.2.0.0/16\n"}
	f, repo, _ := geoServer(t, v4, nil)

	if _, err := f.ImportCountry(context.Background(), CountryImportInput{Country: "NP", Mode: "block"}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	// Re-import with fewer ranges and a different mode: the old set must be gone.
	v4["np"] = "9.9.9.0/24\n"
	res, err := f.ImportCountry(context.Background(), CountryImportInput{Country: "np", Mode: "allow"})
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res.Count != 1 || res.Mode != "allow" {
		t.Fatalf("re-import summary = %+v", res)
	}
	entries, _ := repo.ListIPEntries(context.Background())
	if len(entries) != 1 || entries[0].CIDR != "9.9.9.0/24" || entries[0].Mode != "allow" {
		t.Fatalf("after re-import: %+v", entries)
	}
}

func TestImportCountryValidation(t *testing.T) {
	f, _, _ := geoServer(t, map[string]string{"np": "1.0.0.0/24\n"}, nil)

	if _, err := f.ImportCountry(context.Background(), CountryImportInput{Country: "USA"}); err == nil {
		t.Fatal("want error for 3-letter code")
	}
	if _, err := f.ImportCountry(context.Background(), CountryImportInput{Country: "np", Mode: "sideways"}); err == nil {
		t.Fatal("want error for invalid mode")
	}
	// An unknown country → the source 404s → fetch error surfaces.
	if _, err := f.ImportCountry(context.Background(), CountryImportInput{Country: "zz"}); err == nil {
		t.Fatal("want error for a country the source does not have")
	}
}

func TestListAndRemoveCountry(t *testing.T) {
	v4 := map[string]string{"np": "1.0.0.0/24\n", "in": "3.3.0.0/16\n4.4.0.0/16\n"}
	f, repo, _ := geoServer(t, v4, nil)

	_, _ = f.ImportCountry(context.Background(), CountryImportInput{Country: "np", Mode: "block"})
	_, _ = f.ImportCountry(context.Background(), CountryImportInput{Country: "in", Mode: "block"})
	// A manual entry must not show up as a country.
	_, _ = f.AddIPEntry(context.Background(), IPListInput{CIDR: "5.5.5.5", Mode: "allow"})

	countries, err := f.ListCountries(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(countries) != 2 {
		t.Fatalf("countries = %+v, want 2", countries)
	}
	byCC := map[string]int{}
	for _, c := range countries {
		byCC[c.Country] = c.Count
	}
	if byCC["NP"] != 1 || byCC["IN"] != 2 {
		t.Fatalf("counts wrong: %+v", byCC)
	}

	if err := f.RemoveCountry(context.Background(), "in"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	entries, _ := repo.ListIPEntries(context.Background())
	// NP's one entry + the manual entry remain.
	if len(entries) != 2 {
		t.Fatalf("after remove, entries = %+v, want 2", entries)
	}
	countries, _ = f.ListCountries(context.Background())
	if len(countries) != 1 || countries[0].Country != "NP" {
		t.Fatalf("after remove, countries = %+v", countries)
	}
}

func TestGeoUnavailableWhenNoSource(t *testing.T) {
	f, _, _ := newFW(t) // no WithGeoSource
	if f.GeoAvailable() {
		t.Fatal("GeoAvailable true with no source configured")
	}
	if _, err := f.ImportCountry(context.Background(), CountryImportInput{Country: "np"}); err == nil {
		t.Fatal("want geo_unavailable error")
	}
}
