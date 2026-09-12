// Package httpx holds transport-level helpers shared by every handler:
// JSON encoding, a single error shape, and SSE writing.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// APIError is the error shape defined in the OpenAPI contract.
type APIError struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *APIError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func NewError(status int, code, message string) *APIError {
	return &APIError{Status: status, Code: code, Message: message}
}

var (
	ErrNotFound     = NewError(http.StatusNotFound, "not_found", "resource not found")
	ErrBadRequest   = NewError(http.StatusBadRequest, "bad_request", "invalid request")
	ErrInternal     = NewError(http.StatusInternalServerError, "internal_error", "internal server error")
	ErrUnauthorized = NewError(http.StatusUnauthorized, "unauthorized", "authentication required")
)

// JSON writes v as a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("httpx: encode response", "error", err)
	}
}

// NoContent writes a bare 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// Fail writes err as the contract's error object. Unknown errors are logged in
// full and reported to the client as a generic internal error.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		slog.ErrorContext(r.Context(), "unhandled error", "error", err, "path", r.URL.Path)
		apiErr = ErrInternal
	}
	if apiErr.Status >= 500 {
		slog.ErrorContext(r.Context(), "request failed", "error", err, "path", r.URL.Path)
	}
	JSON(w, apiErr.Status, apiErr)
}

// DecodeJSON reads a JSON body into dst, rejecting unknown fields.
func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return NewError(http.StatusBadRequest, "invalid_body", "request body is not valid JSON: "+err.Error())
	}
	return nil
}
