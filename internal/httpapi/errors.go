package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// statusFor maps a domain error Kind to an HTTP status. The mapping lives in one
// place (docs/04 §3).
func statusFor(kind errx.Kind) int {
	switch kind {
	case errx.KindValidation:
		return http.StatusBadRequest
	case errx.KindUnauthorized:
		return http.StatusUnauthorized
	case errx.KindForbidden:
		return http.StatusForbidden
	case errx.KindNotFound:
		return http.StatusNotFound
	case errx.KindConflict:
		return http.StatusConflict
	case errx.KindUpstream:
		return http.StatusBadGateway
	case errx.KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// writeError maps err to the standard error envelope. Internal (5xx) errors are
// logged with full detail but never leak their cause to the client.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	if e, ok := errx.As(err); ok {
		status := statusFor(e.Kind)
		if status >= 500 {
			logInternal(r, err)
		}
		writeAPIError(w, r, status, e.Code, e.Message, e.Fields...)
		return
	}
	// Unknown error: log the detail, return a generic 500.
	logInternal(r, err)
	writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
}

func logInternal(r *http.Request, err error) {
	slog.Error("request failed",
		"err", err,
		"method", r.Method,
		"path", r.URL.Path,
		"request_id", middleware.GetReqID(r.Context()),
	)
}

// notFoundHandler and methodNotAllowedHandler return the standard error envelope
// instead of chi's plain-text defaults.
func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested resource was not found.")
}

func methodNotAllowedHandler(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "This method is not allowed on the resource.")
}
