package httpapi

import (
	"crypto/ed25519"
	"encoding/base64"
	"io"
	"log/slog"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/license"
)

// testLicenseService builds a licence client over a throwaway directory and a
// throwaway key, so the router tests mount every licence route and the OpenAPI
// walk sees them. It never talks to a network: nothing here activates.
func testLicenseService(t *testing.T) *license.Service {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := license.New(license.Options{
		Dir:       t.TempDir(),
		ServerURL: "http://127.0.0.1:1",
		ExtraKeys: map[string]string{"lk-test": base64.StdEncoding.EncodeToString(pub)},
		Version:   "0.0.0-test",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}
