package broker_test

import (
	"context"
	"testing"

	brokerd "github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func TestCertInstallWritesFiles(t *testing.T) {
	b, fs := newBrokerWithFS(t, &exec.FakeRunner{})

	if _, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "cert.install",
		Input: mustJSON(t, map[string]string{
			"domain": "acme.example.com", "cert_pem": "CERT", "key_pem": "KEY",
		}),
	}); err != nil {
		t.Fatalf("cert.install: %v", err)
	}
	if got, ok := fs.Written("/etc/heropanel/ssl/acme.example.com/fullchain.pem"); !ok || got != "CERT" {
		t.Fatalf("cert not written: %q", got)
	}
	if got, ok := fs.Written("/etc/heropanel/ssl/acme.example.com/privkey.pem"); !ok || got != "KEY" {
		t.Fatalf("key not written: %q", got)
	}
}

func TestCertInstallRejectsBadDomain(t *testing.T) {
	b, _ := newBrokerWithFS(t, &exec.FakeRunner{})
	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "cert.install",
		Input:      mustJSON(t, map[string]string{"domain": "../../etc/x", "cert_pem": "c", "key_pem": "k"}),
	})
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation for bad domain, got %v", err)
	}
}

func TestCertWriteChallenge(t *testing.T) {
	b, fs := newBrokerWithFS(t, &exec.FakeRunner{})
	if _, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "cert.write_challenge",
		Input: mustJSON(t, map[string]string{
			"webroot": "/srv/heropanel/sites/1", "token": "tok_ABC-123", "key_auth": "tok.thumb",
		}),
	}); err != nil {
		t.Fatalf("write_challenge: %v", err)
	}
	if got, ok := fs.Written("/srv/heropanel/sites/1/.well-known/acme-challenge/tok_ABC-123"); !ok || got != "tok.thumb" {
		t.Fatalf("challenge not written: %q", got)
	}
}

func TestCertWriteChallengeRejectsBadWebroot(t *testing.T) {
	b, _ := newBrokerWithFS(t, &exec.FakeRunner{})
	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "cert.write_challenge",
		Input:      mustJSON(t, map[string]string{"webroot": "/etc", "token": "t", "key_auth": "k"}),
	})
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("want forbidden for out-of-policy webroot, got %v", err)
	}
}
