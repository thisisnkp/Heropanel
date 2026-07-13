package ssl_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/ssl"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

type mockGateway struct{ calls []call }
type call struct {
	capability string
	input      map[string]any
}

func (m *mockGateway) Invoke(_ context.Context, c string, input any) (map[string]any, error) {
	in, _ := input.(map[string]any)
	m.calls = append(m.calls, call{c, in})
	return map[string]any{"ok": true}, nil
}
func (m *mockGateway) Health(context.Context) error { return nil }

// fakeACME returns a static (invalid) cert/key so orchestration can be tested.
type fakeACME struct {
	wroteChallenge bool
	fail           bool
}

func (f *fakeACME) Issue(_ context.Context, domain string, writeChallenge func(token, keyAuth string) error) (string, string, time.Time, error) {
	if f.fail {
		return "", "", time.Time{}, errors.New("acme boom")
	}
	if err := writeChallenge("tok", "keyauth"); err != nil {
		return "", "", time.Time{}, err
	}
	f.wroteChallenge = true
	return "CERTPEM-" + domain, "KEYPEM", time.Now().Add(90 * 24 * time.Hour), nil
}

func newSvc(t *testing.T, acme ssl.ACME) (*ssl.Service, *mockGateway) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "ssl.db")
	dbh, err := repository.Open(config.Database{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if _, err := repository.Migrate(context.Background(), dbh); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = repository.NewUserRepository(dbh).Create(context.Background(),
		&repository.User{Email: "o@x.com", Username: "o"})
	gw := &mockGateway{}
	return ssl.NewService(repository.NewCertStore(dbh), gw, acme), gw
}

func TestIssueSelfSignedInstallsAndRecords(t *testing.T) {
	svc, gw := newSvc(t, nil)
	ctx := context.Background()

	cert, err := svc.IssueSelfSigned(ctx, 1, "acme.example.com")
	if err != nil {
		t.Fatalf("self-signed: %v", err)
	}
	if cert.Provider != ssl.ProviderSelfSigned || cert.CommonName != "acme.example.com" {
		t.Fatalf("unexpected cert: %+v", cert)
	}
	if cert.ExpiresAt == "" {
		t.Fatal("expiry should be set")
	}
	// Broker was asked to install real PEM material.
	if len(gw.calls) != 1 || gw.calls[0].capability != "cert.install" {
		t.Fatalf("unexpected calls: %+v", gw.calls)
	}
	certPEM, _ := gw.calls[0].input["cert_pem"].(string)
	if len(certPEM) < 100 || certPEM[:27] != "-----BEGIN CERTIFICATE-----" {
		t.Fatalf("cert_pem not a real certificate: %q", certPEM[:40])
	}

	list, _ := svc.List(ctx, 0, 50, 0)
	if len(list) != 1 {
		t.Fatalf("list = %d, want 1", len(list))
	}
}

func TestUploadCustomValidatesPair(t *testing.T) {
	svc, _ := newSvc(t, nil)
	// Mismatched/garbage PEM is rejected.
	if _, err := svc.UploadCustom(context.Background(), 1, "not-a-cert", "not-a-key"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation, got %v", err)
	}
}

func TestIssueACMEOrchestration(t *testing.T) {
	fa := &fakeACME{}
	svc, gw := newSvc(t, fa)

	cert, err := svc.Issue(context.Background(), 1, "le.example.com", "/srv/heropanel/sites/1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if cert.Provider != ssl.ProviderLetsEncrypt {
		t.Fatalf("provider = %q", cert.Provider)
	}
	if !fa.wroteChallenge {
		t.Fatal("ACME should have written a challenge")
	}
	// The challenge was written via the broker, and the cert installed.
	var wroteChallenge, installed bool
	for _, c := range gw.calls {
		if c.capability == "cert.write_challenge" {
			wroteChallenge = true
		}
		if c.capability == "cert.install" {
			installed = true
		}
	}
	if !wroteChallenge || !installed {
		t.Fatalf("expected write_challenge + install, got %+v", gw.calls)
	}
}

func TestIssueACMEUnavailable(t *testing.T) {
	svc, _ := newSvc(t, nil) // no ACME provider
	if _, err := svc.Issue(context.Background(), 1, "x.example.com", "/srv/heropanel/sites/1"); !errx.IsKind(err, errx.KindUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
}

func TestIssueACMEFailurePropagates(t *testing.T) {
	svc, _ := newSvc(t, &fakeACME{fail: true})
	if _, err := svc.Issue(context.Background(), 1, "x.example.com", "/srv/heropanel/sites/1"); !errx.IsKind(err, errx.KindUpstream) {
		t.Fatalf("want upstream, got %v", err)
	}
}
