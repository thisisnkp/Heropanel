package auth_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/totp"
)

func TestMFASetupEnableLoginComplete(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())
	ctx := context.Background()
	seedUser(t, db, "user@example.com", "user", "password123", "admin") // user id 1

	// 1. Setup returns a secret; MFA is not yet enabled, so login is normal.
	secret, uri, err := svc.SetupMFA(ctx, 1)
	if err != nil || secret == "" || uri == "" {
		t.Fatalf("setup: secret=%q uri=%q err=%v", secret, uri, err)
	}
	if res, _ := svc.Login(ctx, "user@example.com", "password123", "1.1.1.1", "ua"); res.MFARequired {
		t.Fatal("MFA should not be required before enabling")
	}

	// 2. Enabling requires a valid current code.
	if err := svc.EnableMFA(ctx, 1, "000000"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("wrong code should fail enable, got %v", err)
	}
	code, _ := totp.Code(secret)
	if err := svc.EnableMFA(ctx, 1, code); err != nil {
		t.Fatalf("enable: %v", err)
	}

	// 3. Login now returns an MFA challenge (no session yet).
	res, err := svc.Login(ctx, "user@example.com", "password123", "1.1.1.1", "ua")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !res.MFARequired || res.MFAToken == "" || res.SessionToken != "" {
		t.Fatalf("expected MFA challenge, got %+v", res)
	}

	// 4. A wrong code fails; the correct code completes the login.
	if _, err := svc.CompleteMFA(ctx, res.MFAToken, "000000", "1.1.1.1", "ua"); !errx.IsKind(err, errx.KindUnauthorized) {
		t.Fatalf("wrong code should fail, got %v", err)
	}
	code2, _ := totp.Code(secret)
	done, err := svc.CompleteMFA(ctx, res.MFAToken, code2, "1.1.1.1", "ua")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if done.SessionToken == "" || done.Principal == nil {
		t.Fatalf("expected a session after MFA, got %+v", done)
	}
	// The session is real and authenticates.
	if _, err := svc.Authenticate(ctx, done.SessionToken); err != nil {
		t.Fatalf("authenticate after MFA: %v", err)
	}

	// 5. The challenge is single-use.
	if _, err := svc.CompleteMFA(ctx, res.MFAToken, code2, "1.1.1.1", "ua"); !errx.IsKind(err, errx.KindUnauthorized) {
		t.Fatalf("challenge should be single-use, got %v", err)
	}
}

func TestMFADisable(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())
	ctx := context.Background()
	seedUser(t, db, "u@example.com", "u", "password123", "admin")

	secret, _, _ := svc.SetupMFA(ctx, 1)
	code, _ := totp.Code(secret)
	_ = svc.EnableMFA(ctx, 1, code)

	// Wrong code cannot disable.
	if err := svc.DisableMFA(ctx, 1, "000000"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("wrong code should fail disable, got %v", err)
	}
	code2, _ := totp.Code(secret)
	if err := svc.DisableMFA(ctx, 1, code2); err != nil {
		t.Fatalf("disable: %v", err)
	}
	// Login is normal again.
	if res, _ := svc.Login(ctx, "u@example.com", "password123", "1.1.1.1", "ua"); res.MFARequired {
		t.Fatal("MFA should be off after disable")
	}
}
