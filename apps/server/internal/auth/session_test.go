package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The app's bearer header is read before the browser's cookie; a header
// with the scheme alone, or another scheme, carries no token.
func TestTokenFromRequest(t *testing.T) {
	for _, c := range []struct {
		name, header, cookie, want string
	}{
		{"cookie only", "", "from-cookie", "from-cookie"},
		{"bearer only", "Bearer from-header", "", "from-header"},
		{"bearer beats the cookie", "Bearer from-header", "from-cookie", "from-header"},
		{"scheme in any case", "bearer from-header", "", "from-header"},
		{"bearer with nothing after", "Bearer ", "from-cookie", "from-cookie"},
		{"another scheme", "Basic dXNlcjpwYXNz", "", ""},
		{"neither", "", "", ""},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		if c.cookie != "" {
			r.AddCookie(&http.Cookie{Name: CookieName, Value: c.cookie})
		}
		if got := TokenFromRequest(r); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
