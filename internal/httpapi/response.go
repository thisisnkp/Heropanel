package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// meta is the standard response metadata (docs/04 §2).
type meta struct {
	RequestID string `json:"request_id"`
	Ts        string `json:"ts"`
}

type envelope struct {
	Data any  `json:"data"`
	Meta meta `json:"meta"`
}

type errorBody struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	RequestID string       `json:"request_id"`
	Fields    []errx.Field `json:"fields,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// writeJSON writes a success envelope with the given status and data.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{
		Data: data,
		Meta: meta{RequestID: middleware.GetReqID(r.Context()), Ts: nowRFC3339()},
	})
}

// writeAPIError writes an error envelope with an explicit status/code/message.
func writeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string, fields ...errx.Field) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: errorBody{
		Code:      code,
		Message:   message,
		RequestID: middleware.GetReqID(r.Context()),
		Fields:    fields,
	}})
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
