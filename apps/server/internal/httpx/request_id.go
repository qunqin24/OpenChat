package httpx

import (
	"context"
	"crypto/rand"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// requestIDAlphabet is Crockford's base32 in lower case: no i, l, o or u, so an
// id read aloud or typed from a screenshot is not misread.
const requestIDAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// RequestID gives every request a short random id, put where chi's
// middleware.GetReqID finds it (the logs, X-Request-Id, error toasts).
//
// It replaces chi's RequestID, whose ids begin with the server's host name -
// in a container, its id - which every person shown an error would read; and
// which takes an X-Request-Id sent by the client as is, so a visitor could
// write any text into the logs under that field. 12 characters of base32 are
// 60 random bits: unique enough to find one request's lines among years of them.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, NewRequestID())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// NewRequestID is a fresh request id.
func NewRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails (Go 1.24+)
	for i := range b {
		b[i] = requestIDAlphabet[b[i]&31]
	}
	return string(b[:])
}
