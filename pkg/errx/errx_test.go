package errx_test

import (
	"errors"
	"testing"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

func TestKindClassification(t *testing.T) {
	err := errx.NotFound("no_site", "site not found")
	if !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("KindOf = %s, want not_found", errx.KindOf(err))
	}
	// Non-errx errors classify as internal.
	if errx.KindOf(errors.New("plain")) != errx.KindInternal {
		t.Fatal("plain error should classify as internal")
	}
}

func TestWrapUnwrap(t *testing.T) {
	cause := errors.New("boom")
	err := errx.Upstream(cause, "restart_failed", "failed")
	if !errors.Is(err, cause) {
		t.Fatal("wrapped cause should be reachable via errors.Is")
	}
	e, ok := errx.As(err)
	if !ok || e.Code != "restart_failed" || e.Kind != errx.KindUpstream {
		t.Fatalf("As returned %+v (ok=%v)", e, ok)
	}
}

func TestInternalHidesCause(t *testing.T) {
	cause := errors.New("secret internal detail")
	err := errx.Internal(cause)
	e, _ := errx.As(err)
	if e.Message == cause.Error() {
		t.Fatal("Internal must not surface the raw cause in Message")
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause should still be wrapped for logging")
	}
}

func TestValidationFields(t *testing.T) {
	err := errx.Validation("invalid", "bad", errx.Field{Field: "x", Code: "req", Message: "required"})
	e, _ := errx.As(err)
	if len(e.Fields) != 1 || e.Fields[0].Field != "x" {
		t.Fatalf("fields not carried: %+v", e.Fields)
	}
}
