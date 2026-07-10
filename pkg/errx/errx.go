// Package errx defines HeroPanel's typed error model. Domain and service code
// returns *Error values with a stable Kind and machine Code; the HTTP edge maps
// Kind -> status and Code -> a stable API error code exactly once, centrally
// (see docs/04-api-design.md). Wrapped causes carry internal detail that is
// logged but never exposed to callers.
package errx

import (
	"errors"
	"fmt"
)

// Kind is the coarse error category that drives transport mapping.
type Kind string

const (
	KindInternal     Kind = "internal"
	KindValidation   Kind = "validation"
	KindNotFound     Kind = "not_found"
	KindConflict     Kind = "conflict"
	KindForbidden    Kind = "forbidden"
	KindUnauthorized Kind = "unauthorized"
	KindUpstream     Kind = "upstream"
	KindUnavailable  Kind = "unavailable"
)

// Field is a single field-level validation problem.
type Field struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error is HeroPanel's structured error.
type Error struct {
	Kind    Kind    `json:"-"`
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Fields  []Field `json:"fields,omitempty"`

	cause error // internal detail; surfaced via Unwrap, never in Message
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the wrapped cause for errors.Is/As.
func (e *Error) Unwrap() error { return e.cause }

// New builds an *Error with no wrapped cause.
func New(kind Kind, code, msg string) *Error {
	return &Error{Kind: kind, Code: code, Message: msg}
}

// Wrap builds an *Error carrying an internal cause.
func Wrap(cause error, kind Kind, code, msg string) *Error {
	return &Error{Kind: kind, Code: code, Message: msg, cause: cause}
}

// ── convenience constructors ────────────────────────────────────────────────

func Validation(code, msg string, fields ...Field) *Error {
	return &Error{Kind: KindValidation, Code: code, Message: msg, Fields: fields}
}
func NotFound(code, msg string) *Error     { return New(KindNotFound, code, msg) }
func Conflict(code, msg string) *Error     { return New(KindConflict, code, msg) }
func Forbidden(code, msg string) *Error    { return New(KindForbidden, code, msg) }
func Unauthorized(code, msg string) *Error { return New(KindUnauthorized, code, msg) }
func Upstream(cause error, code, msg string) *Error {
	return Wrap(cause, KindUpstream, code, msg)
}

// Internal wraps an unexpected error with a generic, safe message. The original
// cause is preserved for logging but not for display.
func Internal(cause error) *Error {
	return Wrap(cause, KindInternal, "internal_error", "An unexpected error occurred.")
}

// ── classification ──────────────────────────────────────────────────────────

// KindOf walks the error chain and returns the Kind of the first *Error found,
// or KindInternal if none.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindInternal
}

// IsKind reports whether err (or a wrapped error) has the given Kind.
func IsKind(err error, k Kind) bool { return KindOf(err) == k }

// As is a typed helper returning the underlying *Error, if any.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
