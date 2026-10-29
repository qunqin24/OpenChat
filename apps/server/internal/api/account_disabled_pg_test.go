package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// disabledBody is a 403 account_disabled as the page reads it.
type disabledBody struct {
	Code    string          `json:"code"`
	Details AccountDisabled `json:"details"`
}

func disabledOf(rec *httptest.ResponseRecorder) (disabledBody, bool) {
	var body disabledBody
	if rec.Code != http.StatusForbidden || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Code != "account_disabled" {
		return body, false
	}
	return body, true
}

// bannedByModeration: the answer to the request that set a ban off, saying so.
func bannedByModeration(rec *httptest.ResponseRecorder) bool {
	body, ok := disabledOf(rec)
	return ok && body.Details.Reason == DisabledReasonModeration && body.Details.Strikes != nil && *body.Details.Strikes > 0
}

// A disabled account is not signed out: its session is answered everywhere
// with why - the administrator's note, whom to ask - signs out as any other,
// and is let in again once the account is enabled.
func TestADisabledAccountIsToldWhy(t *testing.T) {
	h := newSignupHarness(t)
	// The longest-standing administrator, whom any refused account is told of;
	// but the one to ask about a disabled account is whoever disabled it.
	elder := h.user(sqlc.UserRoleAdmin, false, false)
	admin := h.user(sqlc.UserRoleAdmin, false, false)
	member := h.user(sqlc.UserRoleUser, false, false)
	login := func(email string) *http.Cookie {
		t.Helper()
		rec := h.call(http.MethodPost, "/auth/login", map[string]string{"identifier": email, "password": signupPassword}, "203.0.113.9", nil)
		cookie := sessionCookie(rec)
		if rec.Code != http.StatusOK || cookie == nil {
			t.Fatalf("login %s: %d %s", email, rec.Code, rec.Body)
		}
		return cookie
	}
	adminSession, session := login(admin.Email), login(member.Email)
	patch := func(body any) *httptest.ResponseRecorder {
		return h.call(http.MethodPatch, "/admin/users/"+member.ID.String(), body, "", adminSession)
	}

	if rec := patch(map[string]any{"disabled": true, "disabledNote": strings.Repeat("长", 501)}); code(rec) != "invalid_disabled_note" {
		t.Fatalf("a note too long: %d %s", rec.Code, rec.Body)
	}
	rec := patch(map[string]any{"disabled": true, "disabledNote": "  Shared the account.  "})
	var updated User
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &updated) != nil ||
		updated.DisabledReason == nil || *updated.DisabledReason != DisabledReasonAdmin || updated.DisabledNote == nil || *updated.DisabledNote != "Shared the account." {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body)
	}

	// Every request of the session says why; none is let through.
	for _, path := range []string{"/auth/session", "/conversations", "/me/storage"} {
		body, ok := disabledOf(h.call(http.MethodGet, path, nil, "", session))
		if !ok || body.Details.Reason != DisabledReasonAdmin || body.Details.Note == nil || *body.Details.Note != "Shared the account." ||
			body.Details.Admin == nil || body.Details.Admin.Email != admin.Email || body.Details.Admin.Email == elder.Email ||
			body.Details.DisabledAt.IsZero() {
			t.Errorf("%s: %+v", path, body)
		}
	}
	// So does signing in again.
	rec = h.call(http.MethodPost, "/auth/login", map[string]string{"identifier": member.Email, "password": signupPassword}, "203.0.113.9", nil)
	if body, ok := disabledOf(rec); !ok || body.Details.Note == nil || sessionCookie(rec) != nil {
		t.Errorf("signing in: %d %s", rec.Code, rec.Body)
	}

	// Enabled again, the same session is let in, and the reason is gone.
	if rec := patch(map[string]any{"disabled": false}); rec.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body)
	}
	if rec := h.call(http.MethodGet, "/auth/session", nil, "", session); rec.Code != http.StatusOK {
		t.Fatalf("enabled again: %d %s", rec.Code, rec.Body)
	}
	if row := h.userRow(member.Email); row.DisabledReason != nil || row.DisabledNote != nil {
		t.Errorf("the reason outlived the ban: %v %v", row.DisabledReason, row.DisabledNote)
	}

	// Disabled in a batch, without a note; the session still signs out.
	rec = h.call(http.MethodPost, "/admin/users/bulk", map[string]any{"userIds": []string{member.ID.String()}, "disabled": true}, "", adminSession)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk disable: %d %s", rec.Code, rec.Body)
	}
	if body, ok := disabledOf(h.call(http.MethodGet, "/auth/session", nil, "", session)); !ok || body.Details.Reason != DisabledReasonAdmin || body.Details.Note != nil {
		t.Errorf("after a bulk disable: %+v", body)
	}
	if rec := h.call(http.MethodPost, "/auth/logout", nil, "", session); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body)
	}
	if rec := h.call(http.MethodGet, "/auth/session", nil, "", session); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out, the session answers %d %s", rec.Code, rec.Body)
	}
}
