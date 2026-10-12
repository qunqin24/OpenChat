package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every response names the embedded front end's build; without one (development)
// nothing is added.
func TestBuildHeader(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	buildHeader("abc123")(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/options", nil))
	if got := rec.Header().Get(buildHeaderName); got != "abc123" {
		t.Errorf("header %q, want abc123", got)
	}
	rec = httptest.NewRecorder()
	buildHeader("")(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if _, set := rec.Header()[buildHeaderName]; set {
		t.Error("a build header without a build")
	}
}
