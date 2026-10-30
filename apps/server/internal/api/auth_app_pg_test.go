package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// A native app signs in with `X-OpenChat-Client: app` and is given the session
// token in the body instead of a cookie, then sends it as a bearer header. It
// is the same session a browser's cookie is: the same checks, the same
// sign-out. Over the real router, on the sign-up harness's database.

// appCall is a request with the given headers (Authorization, Origin,
// X-OpenChat-Client) and no cookie.
func (h *signupHarness) appCall(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	var reader bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&reader).Encode(body); err != nil {
			h.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, "/api/v1"+path, &reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func signInResultOf(t *testing.T, rec *httptest.ResponseRecorder) SignInResult {
	t.Helper()
	var out SignInResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	return out
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestAnAppSignsInWithABearerToken(t *testing.T) {
	h := newSignupHarness(t)
	h.modes(true, false)
	h.user(sqlc.UserRoleAdmin, false, false) // the first account; the rest are members
	member := h.user(sqlc.UserRoleUser, false, false)
	creds := map[string]string{"identifier": member.Email, "password": signupPassword}
	app := map[string]string{"X-OpenChat-Client": "app"}

	// A browser's sign-in: a cookie, and no token in the body.
	rec := h.appCall(http.MethodPost, "/auth/login", creds, nil)
	if rec.Code != http.StatusOK || sessionCookie(rec) == nil || signInResultOf(t, rec).Token != nil {
		t.Fatalf("a browser's sign-in: %d, cookie %v, %s", rec.Code, sessionCookie(rec), rec.Body)
	}

	// An app's: the token in the body, and no cookie.
	rec = h.appCall(http.MethodPost, "/auth/login", creds, app)
	signedIn := signInResultOf(t, rec)
	if rec.Code != http.StatusOK || sessionCookie(rec) != nil || signedIn.Token == nil || signedIn.ExpiresAt == nil {
		t.Fatalf("an app's sign-in: %d, cookie %v, %s", rec.Code, sessionCookie(rec), rec.Body)
	}
	if signedIn.Id != member.ID || time.Until(*signedIn.ExpiresAt) < 29*24*time.Hour {
		t.Errorf("signed in as %v until %v", signedIn.Id, signedIn.ExpiresAt)
	}
	token := *signedIn.Token

	// The token is a session.
	if rec := h.appCall(http.MethodGet, "/auth/session", nil, bearer(token)); rec.Code != http.StatusOK {
		t.Fatalf("the bearer's session: %d %s", rec.Code, rec.Body)
	}
	if rec := h.appCall(http.MethodGet, "/auth/session", nil, bearer("not-a-token")); rec.Code != http.StatusUnauthorized {
		t.Errorf("a made-up token: %d", rec.Code)
	}

	// App mode is refused to a browser: a page must never be handed a token.
	withOrigin := map[string]string{"X-OpenChat-Client": "app", "Origin": "https://evil.example"}
	if rec := h.appCall(http.MethodPost, "/auth/login", creds, withOrigin); rec.Code != http.StatusBadRequest || code(rec) != "app_client_from_browser" {
		t.Errorf("app mode from a browser: %d %s", rec.Code, rec.Body)
	}

	// Signing out by the bearer ends that session, and only that one.
	before := h.sessionCount(member.ID)
	if rec := h.appCall(http.MethodPost, "/auth/logout", nil, bearer(token)); rec.Code != http.StatusNoContent {
		t.Fatalf("sign out: %d", rec.Code)
	}
	if h.sessionCount(member.ID) != before-1 {
		t.Errorf("sessions %d after signing out of %d", h.sessionCount(member.ID), before)
	}
	if rec := h.appCall(http.MethodGet, "/auth/session", nil, bearer(token)); rec.Code != http.StatusUnauthorized {
		t.Errorf("a signed-out token: %d", rec.Code)
	}

	// An app can sign up too.
	rec = h.appCall(http.MethodPost, "/auth/register", map[string]string{
		"email": "app-signup@test.invalid", "name": "appsignup", "password": signupPassword}, app)
	if rec.Code != http.StatusCreated || signInResultOf(t, rec).Token == nil || sessionCookie(rec) != nil {
		t.Errorf("an app's sign-up: %d %s", rec.Code, rec.Body)
	}
}

// A bearer meets the same checks as a cookie: a disabled account is told why,
// a pending one has no session.
func TestABearerIsCheckedAsACookieIs(t *testing.T) {
	h := newSignupHarness(t)
	h.user(sqlc.UserRoleAdmin, false, false)
	for _, c := range []struct {
		name              string
		pending, disabled bool
		want              int
		wantCode          string
	}{
		{"disabled", false, true, http.StatusForbidden, "account_disabled"},
		{"pending", true, false, http.StatusUnauthorized, ""},
	} {
		u := h.user(sqlc.UserRoleUser, c.pending, c.disabled)
		token := h.sessionFor(u).Value
		rec := h.appCall(http.MethodGet, "/auth/session", nil, bearer(token))
		if rec.Code != c.want || (c.wantCode != "" && code(rec) != c.wantCode) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
		if sessionCookie(rec) != nil || len(rec.Result().Cookies()) > 0 {
			t.Errorf("%s: a cookie was cleared for a bearer", c.name)
		}
	}
}

// An app's session is renewed when it asks who it is, once it is past half
// its life; a fresh one is left alone, and a browser's cookie is never renewed.
func TestAnAppsSessionIsRenewed(t *testing.T) {
	h := newSignupHarness(t)
	u := h.user(sqlc.UserRoleAdmin, false, false)
	ctx := context.Background()
	expiry := func(token string) time.Time {
		t.Helper()
		var at time.Time
		if err := h.pool.QueryRow(ctx, `SELECT expires_at FROM sessions WHERE token_hash = $1`, auth.HashToken(token)).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	setExpiry := func(token string, in time.Duration) {
		t.Helper()
		if _, err := h.pool.Exec(ctx, `UPDATE sessions SET expires_at = $2 WHERE token_hash = $1`, auth.HashToken(token), time.Now().Add(in)); err != nil {
			t.Fatal(err)
		}
	}

	old := h.sessionFor(u).Value
	setExpiry(old, 5*24*time.Hour)
	if rec := h.appCall(http.MethodGet, "/auth/session", nil, bearer(old)); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if left := time.Until(expiry(old)); left < 29*24*time.Hour {
		t.Errorf("an app's session near its end was not renewed: %v left", left)
	}

	fresh := h.sessionFor(u).Value
	setExpiry(fresh, 25*24*time.Hour)
	before := expiry(fresh)
	h.appCall(http.MethodGet, "/auth/session", nil, bearer(fresh))
	if !expiry(fresh).Equal(before) {
		t.Errorf("a fresh session was renewed")
	}

	cookie := h.sessionFor(u)
	setExpiry(cookie.Value, 5*24*time.Hour)
	before = expiry(cookie.Value)
	if rec := h.call(http.MethodGet, "/auth/session", nil, "", cookie); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if !expiry(cookie.Value).Equal(before) {
		t.Errorf("a browser's session was renewed")
	}
}
