package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	// CookieName is the session cookie declared in api/openapi.yaml.
	CookieName = "openchat_session"

	// SessionTTL is how long a session stays valid without being refreshed.
	SessionTTL = 30 * 24 * time.Hour

	tokenBytes = 32
)

// NewToken returns a fresh session token and the hash to store for it. Only the
// hash is persisted: a leaked database then cannot be replayed as a login.
func NewToken() (token string, hash []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("auth: read token: %w", err)
	}

	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken is the lookup key for a session token. SHA-256 rather than argon2
// on purpose: the token is 32 bytes of entropy, so there is nothing to brute
// force, and this runs on every authenticated request.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// SetCookie writes the session cookie. `Secure` is left off in development so
// the cookie survives plain-HTTP localhost.
func SetCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(SessionTTL),
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		// Lax, not Strict: Strict would drop the cookie when the user arrives
		// from an external link, presenting them as signed out on first paint.
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie expires the session cookie in the browser.
func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// TokenFromRequest reads the session token, or "" when there is none: an
// `Authorization: Bearer` header first - how the app sends it, having been
// given the token when it signed in (api/auth.go appClient) - then the
// cookie, how a browser does. Either way it is the same session row, looked
// up by its hash, so every check on a session holds for both.
func TokenFromRequest(r *http.Request) string {
	if token, ok := BearerToken(r); ok {
		return token
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// BearerToken is the token of an `Authorization: Bearer <token>` header (the
// scheme in any case), and whether the request carried one at all. A header
// with the scheme and nothing after it carries none.
func BearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
