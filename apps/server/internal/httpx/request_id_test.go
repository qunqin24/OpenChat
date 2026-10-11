package httpx

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestRequestIDIsShortRandomAndOurs(t *testing.T) {
	var got string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = middleware.GetReqID(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "injected\nline")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got == "" || strings.Contains(got, "injected") {
		t.Fatalf("id %q: missing, or taken from the client", got)
	}
	if host, _ := os.Hostname(); host != "" && strings.Contains(got, host) {
		t.Errorf("id %q names the host", got)
	}
	if len(got) != 12 || strings.Trim(got, requestIDAlphabet) != "" {
		t.Errorf("id %q is not 12 characters of the alphabet", got)
	}
	seen := map[string]bool{}
	for range 1000 {
		id := NewRequestID()
		if seen[id] {
			t.Fatalf("id %q repeated", id)
		}
		seen[id] = true
	}
}
