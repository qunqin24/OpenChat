package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// Sign-up control and the limits on signing in, over HTTP through the real
// router (so the RealIP middleware, the protection groups and the generated
// wrapper are all in the path). The package has a database of its own
// (<name in the URL>_api_signup) because these tests need an empty users table
// and exact attempt counts, which nothing else may write.

const signupPassword = "correct horse battery"

type signupHarness struct {
	t       *testing.T
	pool    *pgxpool.Pool
	q       *sqlc.Queries
	server  *Server
	handler http.Handler
}

// newSignupHarness is a server on a database with no accounts, no recorded
// attempts, and both switches unset (so on).
func newSignupHarness(t *testing.T) *signupHarness {
	t.Helper()
	pool := dbtest.Pool(t, "api_signup")
	q := sqlc.New(pool)
	ctx := context.Background()
	for _, stmt := range []string{
		`DELETE FROM auth_attempts`,
		`DELETE FROM users`,
		`DELETE FROM app_settings WHERE key IN ('allow_registration', 'registration_approval')`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{cfg: &config.Config{}, pool: pool, q: q}
	return &signupHarness{t: t, pool: pool, q: q, server: server,
		handler: NewRouter(&config.Config{}, server, auth.NewMiddleware(q))}
}

func (h *signupHarness) set(key string, value bool) {
	h.t.Helper()
	raw, _ := json.Marshal(value)
	if err := h.q.SetSetting(context.Background(), sqlc.SetSettingParams{Key: key, Value: raw}); err != nil {
		h.t.Fatal(err)
	}
}

// modes sets the two switches.
func (h *signupHarness) modes(allow, approval bool) {
	h.set(settingAllowRegistration, allow)
	h.set(settingRegistrationApproval, approval)
}

// call makes a request as a client at this address, with a session if given.
func (h *signupHarness) call(method, path string, body any, ip string, session *http.Cookie) *httptest.ResponseRecorder {
	h.t.Helper()
	var reader bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&reader).Encode(body); err != nil {
			h.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, "/api/v1"+path, &reader)
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	if session != nil {
		req.AddCookie(session)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func code(rec *httptest.ResponseRecorder) string {
	var e struct{ Code string }
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	return e.Code
}

// sessionCookie is the cookie a response set, or nil.
func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

// user inserts an account the way an administrator (or an old version) would:
// not pending unless asked.
func (h *signupHarness) user(role sqlc.UserRole, pending, disabled bool) sqlc.User {
	h.t.Helper()
	hash, err := auth.HashPassword(signupPassword)
	if err != nil {
		h.t.Fatal(err)
	}
	tag := uuid.NewString()[:8]
	var u sqlc.User
	row := h.pool.QueryRow(context.Background(), `
		INSERT INTO users (email, name, password_hash, role, pending, disabled_at)
		VALUES ($1, $2, $3, $4, $5, CASE WHEN $6 THEN now() END) RETURNING id, email, name, role, pending`,
		tag+"@test.invalid", "u"+tag, hash, role, pending, disabled)
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Pending); err != nil {
		h.t.Fatal(err)
	}
	return u
}

// sessionFor gives an account a session directly, as if it had signed in.
func (h *signupHarness) sessionFor(u sqlc.User) *http.Cookie {
	h.t.Helper()
	token, hash, err := auth.NewToken()
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.q.CreateSession(context.Background(), sqlc.CreateSessionParams{
		UserID: u.ID, TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		h.t.Fatal(err)
	}
	return &http.Cookie{Name: auth.CookieName, Value: token}
}

func (h *signupHarness) register(ip, name string) *httptest.ResponseRecorder {
	return h.call(http.MethodPost, "/auth/register", map[string]string{
		"email": name + "@test.invalid", "name": name, "password": signupPassword}, ip, nil)
}

func (h *signupHarness) login(ip, identifier, password string) *httptest.ResponseRecorder {
	return h.call(http.MethodPost, "/auth/login", map[string]string{"identifier": identifier, "password": password}, ip, nil)
}

func (h *signupHarness) userRow(email string) sqlc.User {
	h.t.Helper()
	u, err := h.q.GetUserByLogin(context.Background(), email)
	if err != nil {
		h.t.Fatalf("user %s: %v", email, err)
	}
	return u
}

func (h *signupHarness) sessionCount(userID uuid.UUID) int {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

// ---------------------------------------------------------------- the modes

// The first account is the administrator, is never pending and is never
// refused, whatever the switches say - or a fresh install could lock itself out.
func TestFirstAccountIsAlwaysAnAdmin(t *testing.T) {
	for _, mode := range []struct{ allow, approval bool }{{false, true}, {false, false}, {true, true}, {true, false}} {
		t.Run(fmt.Sprintf("allow=%v,approval=%v", mode.allow, mode.approval), func(t *testing.T) {
			h := newSignupHarness(t)
			h.modes(mode.allow, mode.approval)

			rec := h.register("10.0.0.1", "first")
			if rec.Code != http.StatusCreated {
				t.Fatalf("the first account: %d %s", rec.Code, rec.Body)
			}
			if sessionCookie(rec) == nil {
				t.Error("the first account was not signed in")
			}
			first := h.userRow("first@test.invalid")
			if first.Role != sqlc.UserRoleAdmin || first.Pending {
				t.Errorf("first account: role %s pending %v, want an admin that is not pending", first.Role, first.Pending)
			}
			if rec := h.login("10.0.0.1", "first", signupPassword); rec.Code != http.StatusOK {
				t.Errorf("the first account signing in again: %d %s", rec.Code, rec.Body)
			}

			// The second is subject to the switches.
			second := h.register("10.0.0.2", "second")
			if mode.allow && second.Code < 200 || !mode.allow && second.Code != http.StatusForbidden {
				t.Errorf("the second account with allow=%v: %d %s", mode.allow, second.Code, second.Body)
			}
		})
	}
}

func TestClosedRegistration(t *testing.T) {
	h := newSignupHarness(t)
	h.modes(false, true)
	if rec := h.register("10.0.0.1", "admin1"); rec.Code != http.StatusCreated {
		t.Fatalf("first account: %d %s", rec.Code, rec.Body)
	}
	rec := h.register("10.0.0.2", "late")
	if rec.Code != http.StatusForbidden || code(rec) != "registration_closed" {
		t.Fatalf("registering while closed: %d %s", rec.Code, rec.Body)
	}
	if sessionCookie(rec) != nil {
		t.Error("a refused registration set a session")
	}
	if _, err := h.q.GetUserByLogin(context.Background(), "late@test.invalid"); err == nil {
		t.Error("a refused registration left an account behind")
	}

	// Accounts still come from an administrator, usable at once.
	admin := h.userRow("admin1@test.invalid")
	created := h.call(http.MethodPost, "/admin/users", map[string]any{
		"email": "made@test.invalid", "name": "made", "password": signupPassword, "role": "user"},
		"10.0.0.3", h.sessionFor(admin))
	if created.Code != http.StatusCreated {
		t.Fatalf("admin creating an account while closed: %d %s", created.Code, created.Body)
	}
	if rec := h.login("10.0.0.4", "made", signupPassword); rec.Code != http.StatusOK {
		t.Errorf("the created account signing in: %d %s", rec.Code, rec.Body)
	}
}

func TestOpenRegistration(t *testing.T) {
	h := newSignupHarness(t)
	h.modes(true, false)
	h.register("10.0.0.1", "admin1")

	rec := h.register("10.0.0.2", "joiner")
	if rec.Code != http.StatusCreated {
		t.Fatalf("open registration: %d %s", rec.Code, rec.Body)
	}
	cookie := sessionCookie(rec)
	if cookie == nil {
		t.Fatal("no session after registering in open mode")
	}
	joiner := h.userRow("joiner@test.invalid")
	if joiner.Pending || joiner.Role != sqlc.UserRoleUser {
		t.Errorf("joiner: pending %v role %s", joiner.Pending, joiner.Role)
	}
	if got := h.call(http.MethodGet, "/auth/session", nil, "10.0.0.2", cookie); got.Code != http.StatusOK {
		t.Errorf("the new account's session: %d", got.Code)
	}
}

func TestApprovalRegistration(t *testing.T) {
	h := newSignupHarness(t)
	h.modes(true, true)
	h.register("10.0.0.1", "admin1")

	rec := h.register("10.0.0.2", "waiting")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("registering under approval: %d %s", rec.Code, rec.Body)
	}
	var body RegisterPending
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Status != PendingApproval {
		t.Errorf("body %s (%v)", rec.Body, err)
	}
	if sessionCookie(rec) != nil || len(rec.Result().Cookies()) != 0 {
		t.Error("a pending registration set a cookie")
	}
	waiting := h.userRow("waiting@test.invalid")
	if !waiting.Pending {
		t.Fatal("the account is not pending")
	}
	if h.sessionCount(waiting.ID) != 0 {
		t.Error("a pending account has a session")
	}

	// Right password: told why, no session. By username and by address.
	for _, who := range []string{"waiting", "waiting@test.invalid"} {
		rec := h.login("10.0.0.3", who, signupPassword)
		if rec.Code != http.StatusForbidden || code(rec) != "pending_approval" {
			t.Errorf("login as %s: %d %s", who, rec.Code, rec.Body)
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Errorf("login as %s: a cookie was set", who)
		}
	}
	if h.sessionCount(waiting.ID) != 0 {
		t.Error("a refused login created a session")
	}
	// Wrong password: the generic answer, nothing about the account.
	if rec := h.login("10.0.0.3", "waiting", "not the password"); rec.Code != http.StatusUnauthorized || code(rec) != "invalid_credentials" {
		t.Errorf("wrong password for a pending account: %d %s", rec.Code, rec.Body)
	}

	// An account an administrator made is not pending in this mode either.
	admin := h.userRow("admin1@test.invalid")
	h.call(http.MethodPost, "/admin/users", map[string]any{
		"email": "made@test.invalid", "name": "made", "password": signupPassword, "role": "user"},
		"10.0.0.4", h.sessionFor(admin))
	if h.userRow("made@test.invalid").Pending {
		t.Error("an account created by an administrator is pending")
	}
}

// A pending account is reachable by nothing, even if a session row for it
// existed (which no code path creates).
func TestPendingAccountReachesNothing(t *testing.T) {
	h := newSignupHarness(t)
	pending := h.user(sqlc.UserRoleUser, true, false)
	pendingAdmin := h.user(sqlc.UserRoleAdmin, true, false)

	for _, u := range []sqlc.User{pending, pendingAdmin} {
		cookie := h.sessionFor(u)
		for _, p := range []struct{ method, path string }{
			{http.MethodGet, "/auth/session"},
			{http.MethodGet, "/conversations"},
			{http.MethodPost, "/conversations"},
			{http.MethodGet, "/models"},
			{http.MethodGet, "/features"},
			{http.MethodGet, "/me/personalization"},
			{http.MethodGet, "/me/export"},
			{http.MethodPost, "/files"},
			{http.MethodPatch, "/auth/me"},
			{http.MethodGet, "/admin/users"},
			{http.MethodGet, "/admin/system"},
			{http.MethodPatch, "/admin/users/" + u.ID.String()},
			{http.MethodPost, "/admin/users/bulk"},
		} {
			rec := h.call(p.method, p.path, map[string]any{"approved": true, "userIds": []string{u.ID.String()}}, "10.0.0.9", cookie)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("a pending %s on %s %s: %d, want 401", u.Role, p.method, p.path, rec.Code)
			}
		}
	}
	if h.userRow(pending.Email).Pending != true {
		t.Error("a pending account approved itself")
	}
}

func TestApproving(t *testing.T) {
	h := newSignupHarness(t)
	admin := h.user(sqlc.UserRoleAdmin, false, false)
	adminSession := h.sessionFor(admin)
	member := h.user(sqlc.UserRoleUser, false, false)
	memberSession := h.sessionFor(member)
	one := h.user(sqlc.UserRoleUser, true, false)
	two := h.user(sqlc.UserRoleUser, true, false)
	three := h.user(sqlc.UserRoleUser, true, false)

	approve := func(id uuid.UUID, as *http.Cookie) *httptest.ResponseRecorder {
		return h.call(http.MethodPatch, "/admin/users/"+id.String(), map[string]any{"approved": true}, "10.0.0.5", as)
	}

	// Admin only: 404 for a member, 401 without a session; nothing changes.
	if rec := approve(one.ID, memberSession); rec.Code != http.StatusNotFound {
		t.Errorf("a member approving: %d, want 404", rec.Code)
	}
	if rec := approve(one.ID, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("approving without a session: %d, want 401", rec.Code)
	}
	if rec := h.call(http.MethodPost, "/admin/users/bulk", map[string]any{"userIds": []string{one.ID.String()}, "approved": true}, "10.0.0.5", memberSession); rec.Code != http.StatusNotFound {
		t.Errorf("a member approving in bulk: %d, want 404", rec.Code)
	}
	if !h.userRow(one.Email).Pending {
		t.Fatal("a refused approval changed the account")
	}

	// Pending accounts are told apart in the list, and filterable.
	list := func(query string) UserPage {
		rec := h.call(http.MethodGet, "/admin/users?limit=100"+query, nil, "10.0.0.5", adminSession)
		var page UserPage
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &page) != nil {
			t.Fatalf("list %q: %d %s", query, rec.Code, rec.Body)
		}
		return page
	}
	has := func(page UserPage, id uuid.UUID) (found, pending bool) {
		for _, u := range page.Items {
			if u.Id == id {
				return true, u.Pending
			}
		}
		return false, false
	}
	page := list("&status=pending")
	for _, u := range []sqlc.User{one, two, three} {
		if found, pending := has(page, u.ID); !found || !pending {
			t.Errorf("pending list: %s found %v pending %v", u.Email, found, pending)
		}
	}
	if found, _ := has(page, member.ID); found {
		t.Error("the pending filter lists an approved account")
	}
	if page.Total != 3 {
		t.Errorf("pending total %d, want 3", page.Total)
	}

	// Approve one, then two with the bulk action.
	if rec := approve(one.ID, adminSession); rec.Code != http.StatusOK {
		t.Fatalf("approving: %d %s", rec.Code, rec.Body)
	}
	if h.userRow(one.Email).Pending {
		t.Error("still pending after approval")
	}
	bulk := h.call(http.MethodPost, "/admin/users/bulk", map[string]any{
		"userIds": []string{one.ID.String(), two.ID.String(), three.ID.String(), uuid.NewString()}, "approved": true},
		"10.0.0.5", adminSession)
	var result BulkUpdateResult
	if bulk.Code != http.StatusOK || json.Unmarshal(bulk.Body.Bytes(), &result) != nil {
		t.Fatalf("bulk approve: %d %s", bulk.Code, bulk.Body)
	}
	if result.Updated != 2 || len(result.Skipped) != 1 {
		t.Errorf("bulk approve: updated %d skipped %d, want 2 (the two still waiting) and 1 (no such account)", result.Updated, len(result.Skipped))
	}
	if list("&status=pending").Total != 0 {
		t.Error("accounts are still pending")
	}

	// Approved, they sign in.
	for _, u := range []sqlc.User{one, two, three} {
		if rec := h.login("10.0.0.6", u.Email, signupPassword); rec.Code != http.StatusOK {
			t.Errorf("%s after approval: %d %s", u.Email, rec.Code, rec.Body)
		}
	}

	// There is no way back to pending, and a change must be one thing.
	if rec := h.call(http.MethodPatch, "/admin/users/"+one.ID.String(), map[string]any{"approved": false}, "10.0.0.5", adminSession); rec.Code != http.StatusBadRequest || code(rec) != "invalid_approval" {
		t.Errorf("approved=false: %d %s", rec.Code, rec.Body)
	}
	if rec := h.call(http.MethodPost, "/admin/users/bulk", map[string]any{"userIds": []string{one.ID.String()}, "approved": true, "disabled": true}, "10.0.0.5", adminSession); rec.Code != http.StatusBadRequest {
		t.Errorf("two changes at once: %d", rec.Code)
	}
}

// Rejecting a sign-up is disabling it: it leaves the pending list, says
// disabled, and enabling it again returns it to the list of those waiting.
func TestRejectingASignup(t *testing.T) {
	h := newSignupHarness(t)
	h.modes(true, true)
	admin := h.user(sqlc.UserRoleAdmin, false, false)
	adminSession := h.sessionFor(admin)
	if rec := h.register("10.0.0.2", "hopeful"); rec.Code != http.StatusAccepted {
		t.Fatalf("register: %d", rec.Code)
	}
	hopeful := h.userRow("hopeful@test.invalid")

	if rec := h.call(http.MethodPatch, "/admin/users/"+hopeful.ID.String(), map[string]any{"disabled": true}, "10.0.0.5", adminSession); rec.Code != http.StatusOK {
		t.Fatalf("rejecting: %d %s", rec.Code, rec.Body)
	}
	if rec := h.login("10.0.0.3", "hopeful", signupPassword); rec.Code != http.StatusForbidden || code(rec) != "account_disabled" {
		t.Errorf("a rejected sign-up signing in: %d %s", rec.Code, rec.Body)
	}
	count := func(status string) int64 {
		rec := h.call(http.MethodGet, "/admin/users?status="+status, nil, "10.0.0.5", adminSession)
		var page UserPage
		_ = json.Unmarshal(rec.Body.Bytes(), &page)
		return page.Total
	}
	if count("pending") != 0 || count("disabled") != 1 {
		t.Errorf("after rejecting: pending %d disabled %d, want 0 and 1", count("pending"), count("disabled"))
	}
	h.call(http.MethodPatch, "/admin/users/"+hopeful.ID.String(), map[string]any{"disabled": false}, "10.0.0.5", adminSession)
	if rec := h.login("10.0.0.3", "hopeful", signupPassword); code(rec) != "pending_approval" {
		t.Errorf("after enabling a rejected sign-up: %d %s, want pending_approval", rec.Code, rec.Body)
	}
	if count("pending") != 1 {
		t.Error("an enabled rejected sign-up is not back in the pending list")
	}
}

// -------------------------------------------------------------- the options

func TestAuthOptions(t *testing.T) {
	h := newSignupHarness(t)
	options := func() AuthOptions {
		rec := h.call(http.MethodGet, "/auth/options", nil, "10.0.0.1", nil)
		var o AuthOptions
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &o) != nil {
			t.Fatalf("options: %d %s", rec.Code, rec.Body)
		}
		return o
	}

	// Nobody has registered: it is open to the first account, with no approval, however it is set.
	h.modes(false, true)
	if got := options(); !got.Registration || got.Approval {
		t.Errorf("fresh install, closed: %+v, want registration open without approval", got)
	}

	h.user(sqlc.UserRoleAdmin, false, false)
	for _, c := range []struct {
		allow, approval bool
		want            AuthOptions
	}{
		{false, true, AuthOptions{Registration: false, Approval: false, Passkeys: true}},
		{false, false, AuthOptions{Registration: false, Approval: false, Passkeys: true}},
		{true, true, AuthOptions{Registration: true, Approval: true, Passkeys: true}},
		{true, false, AuthOptions{Registration: true, Approval: false, Passkeys: true}},
	} {
		h.modes(c.allow, c.approval)
		if got := options(); got != c.want {
			t.Errorf("allow=%v approval=%v: %+v, want %+v", c.allow, c.approval, got, c.want)
		}
	}
}

func TestSignupSwitchesInSystemSettings(t *testing.T) {
	h := newSignupHarness(t)
	admin := h.user(sqlc.UserRoleAdmin, false, false)
	session := h.sessionFor(admin)
	read := func() SystemSettings {
		rec := h.call(http.MethodGet, "/admin/system", nil, "10.0.0.1", session)
		var s SystemSettings
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &s) != nil {
			t.Fatalf("system: %d %s", rec.Code, rec.Body)
		}
		return s
	}

	if s := read(); !s.AllowRegistration || !s.RegistrationApproval {
		t.Errorf("defaults: allow %v approval %v, want both on", s.AllowRegistration, s.RegistrationApproval)
	}
	rec := h.call(http.MethodPatch, "/admin/system", map[string]any{"allowRegistration": false}, "10.0.0.1", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	if s := read(); s.AllowRegistration || !s.RegistrationApproval {
		t.Errorf("after turning registration off: allow %v approval %v", s.AllowRegistration, s.RegistrationApproval)
	}
	h.call(http.MethodPatch, "/admin/system", map[string]any{"allowRegistration": true, "registrationApproval": false}, "10.0.0.1", session)
	if s := read(); !s.AllowRegistration || s.RegistrationApproval {
		t.Errorf("after open mode: allow %v approval %v", s.AllowRegistration, s.RegistrationApproval)
	}
	// Not an admin's to change for a member.
	member := h.sessionFor(h.user(sqlc.UserRoleUser, false, false))
	if rec := h.call(http.MethodPatch, "/admin/system", map[string]any{"allowRegistration": false}, "10.0.0.1", member); rec.Code != http.StatusNotFound {
		t.Errorf("a member changing the switches: %d, want 404", rec.Code)
	}
}

// ------------------------------------------------------------------- limits

func retryAfter(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	n, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || n < 1 {
		t.Fatalf("Retry-After %q (%v), want a positive number of seconds", rec.Header().Get("Retry-After"), err)
	}
	return n
}

func TestLoginLimitPerAddress(t *testing.T) {
	h := newSignupHarness(t)
	member := h.user(sqlc.UserRoleUser, false, false)

	// Ten failures from one address, each against a different identifier so that
	// only the address limit can be what stops it.
	for i := range 10 {
		if rec := h.login("203.0.113.1", fmt.Sprintf("nobody%d", i), "wrong"); rec.Code != http.StatusUnauthorized || code(rec) != "invalid_credentials" {
			t.Fatalf("failure %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	// The eleventh is turned away - even with the right password, and for an
	// identifier it has not tried.
	rec := h.login("203.0.113.1", member.Email, signupPassword)
	if rec.Code != http.StatusTooManyRequests || code(rec) != "login_rate_limited" {
		t.Fatalf("over the limit: %d %s", rec.Code, rec.Body)
	}
	if wait := retryAfter(t, rec); wait < 14*60 || wait > 15*60 {
		t.Errorf("Retry-After %d, want about 15 minutes", wait)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("a refused sign-in set a cookie")
	}

	// Another address is untouched, and so is the account: nothing locked it.
	if rec := h.login("203.0.113.2", member.Email, signupPassword); rec.Code != http.StatusOK {
		t.Errorf("another address signing in to the same account: %d %s", rec.Code, rec.Body)
	}
	// An IPv6 address counts as its /64.
	for i := range 10 {
		h.login("2001:db8:1:2::"+strconv.Itoa(i+1), "nobody", "wrong")
	}
	if rec := h.login("2001:db8:1:2:ffff::1", "nobody", "wrong"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("a different address in the same /64: %d, want 429", rec.Code)
	}
	if rec := h.login("2001:db8:1:3::1", "nobody", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Errorf("another /64: %d, want 401", rec.Code)
	}

	// The wait ends as the failures age out: with the oldest of the ten made
	// 14 minutes ago, one minute remains; with it 16 minutes old, there is room.
	if _, err := h.pool.Exec(context.Background(), `
		UPDATE auth_attempts SET created_at = now() - interval '14 minutes'
		WHERE id = (SELECT id FROM auth_attempts WHERE ip = '203.0.113.1' ORDER BY created_at LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	rec = h.login("203.0.113.1", "nobody", "wrong")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("still within the window: %d", rec.Code)
	}
	if wait := retryAfter(t, rec); wait < 55 || wait > 65 {
		t.Errorf("Retry-After %d, want about 60", wait)
	}
	if _, err := h.pool.Exec(context.Background(), `
		UPDATE auth_attempts SET created_at = now() - interval '16 minutes'
		WHERE id = (SELECT id FROM auth_attempts WHERE ip = '203.0.113.1' ORDER BY created_at LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	if rec := h.login("203.0.113.1", member.Email, signupPassword); rec.Code != http.StatusOK {
		t.Errorf("after a failure aged out: %d %s", rec.Code, rec.Body)
	}
}

func TestLoginLimitPerAddressAndIdentifier(t *testing.T) {
	h := newSignupHarness(t)
	victim := h.user(sqlc.UserRoleAdmin, false, false)

	// Five failures against one account from one address stop that pair...
	for i := range 5 {
		if rec := h.login("203.0.113.10", victim.Email, "guess"+strconv.Itoa(i)); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	rec := h.login("203.0.113.10", victim.Email, "guess")
	if rec.Code != http.StatusTooManyRequests || code(rec) != "login_rate_limited" {
		t.Fatalf("sixth attempt on one account: %d %s", rec.Code, rec.Body)
	}
	retryAfter(t, rec)
	// ...whatever the password (spaces and case in the identifier do not make a
	// fresh allowance; the address limit bounds the other spelling of the account)...
	if rec := h.login("203.0.113.10", "  "+strings.ToUpper(victim.Email)+" ", signupPassword); rec.Code != http.StatusTooManyRequests {
		t.Errorf("the same identifier spelled differently: %d, want 429", rec.Code)
	}
	if rec := h.login("203.0.113.10", victim.Email, signupPassword); rec.Code != http.StatusTooManyRequests {
		t.Errorf("the right password from the stopped pair: %d, want 429", rec.Code)
	}
	// ...but not the same address trying other accounts, not other addresses
	// trying this one, and not the account itself: the administrator is not
	// locked out by someone else's guesses.
	if rec := h.login("203.0.113.10", "someone-else", "guess"); rec.Code != http.StatusUnauthorized {
		t.Errorf("the same address, another identifier: %d, want 401", rec.Code)
	}
	if rec := h.login("203.0.113.11", victim.Email, "guess"); rec.Code != http.StatusUnauthorized {
		t.Errorf("another address, the same identifier: %d, want 401", rec.Code)
	}
	if rec := h.login("203.0.113.12", victim.Email, signupPassword); rec.Code != http.StatusOK {
		t.Errorf("the account owner from elsewhere: %d %s", rec.Code, rec.Body)
	}
}

// Only failures count: a sign-in with the right password gives its slot back.
func TestSuccessfulSignInsAreNotCounted(t *testing.T) {
	h := newSignupHarness(t)
	member := h.user(sqlc.UserRoleUser, false, false)

	for range 4 {
		h.login("203.0.113.20", member.Email, "wrong")
	}
	// Twenty good sign-ins neither hit the per-address limit (10) nor the
	// per-identifier one (5, with four failures already against it).
	for i := range 20 {
		if rec := h.login("203.0.113.20", member.Email, signupPassword); rec.Code != http.StatusOK {
			t.Fatalf("sign-in %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	var rows int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM auth_attempts WHERE kind = 'login' AND ip = '203.0.113.20'`).Scan(&rows); err != nil || rows != 4 {
		t.Errorf("recorded attempts %d (%v), want the 4 failures", rows, err)
	}
	// The fifth failure is the last one let through.
	h.login("203.0.113.20", member.Email, "wrong")
	if rec := h.login("203.0.113.20", member.Email, signupPassword); rec.Code != http.StatusTooManyRequests {
		t.Errorf("after five failures: %d, want 429", rec.Code)
	}

	// A right password that meets a pending or disabled account is not a failure either.
	pending := h.user(sqlc.UserRoleUser, true, false)
	for range 12 {
		if rec := h.login("203.0.113.21", pending.Email, signupPassword); code(rec) != "pending_approval" {
			t.Fatalf("pending: %d %s", rec.Code, rec.Body)
		}
	}
}

func TestRegisterLimit(t *testing.T) {
	h := newSignupHarness(t)
	h.modes(true, false)

	// Five attempts, whatever came of them: two make accounts, three are
	// refused for a name already in use (which is how addresses get probed).
	tag := uuid.NewString()[:6]
	for i, name := range []string{"a" + tag, "b" + tag, "a" + tag, "a" + tag, "a" + tag} {
		rec := h.register("198.51.100.1", name)
		wantCode := http.StatusCreated
		if i >= 2 {
			wantCode = http.StatusBadRequest
		}
		if rec.Code != wantCode {
			t.Fatalf("attempt %d: %d %s, want %d", i+1, rec.Code, rec.Body, wantCode)
		}
	}
	rec := h.register("198.51.100.1", "onemore")
	if rec.Code != http.StatusTooManyRequests || code(rec) != "register_rate_limited" {
		t.Fatalf("sixth sign-up: %d %s", rec.Code, rec.Body)
	}
	if wait := retryAfter(t, rec); wait < 59*60 || wait > 60*60 {
		t.Errorf("Retry-After %d, want about an hour", wait)
	}
	if _, err := h.q.GetUserByLogin(context.Background(), "onemore@test.invalid"); err == nil {
		t.Error("a limited sign-up made an account")
	}
	if rec := h.register("198.51.100.2", "elsewhere"); rec.Code != http.StatusCreated {
		t.Errorf("another address: %d %s", rec.Code, rec.Body)
	}
	// Sign-ins are counted apart.
	if rec := h.login("198.51.100.1", "nobody", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Errorf("a sign-in from the address that used up its sign-ups: %d, want 401", rec.Code)
	}
	// A refused one (closed registration) is turned away before it is counted.
	h.modes(false, true)
	if rec := h.register("198.51.100.3", "closed"); code(rec) != "registration_closed" {
		t.Errorf("closed: %d %s", rec.Code, rec.Body)
	}
	var rows int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM auth_attempts WHERE ip = '198.51.100.3'`).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("attempts recorded for a closed registration: %d (%v)", rows, err)
	}
}

func TestPurgeAuthAttempts(t *testing.T) {
	h := newSignupHarness(t)
	ctx := context.Background()
	for _, age := range []string{"5 minutes", "59 minutes", "61 minutes", "3 days"} {
		if _, err := h.pool.Exec(ctx, `INSERT INTO auth_attempts (kind, ip, created_at) VALUES ('login', '203.0.113.99', now() - $1::interval)`, age); err != nil {
			t.Fatal(err)
		}
	}
	h.server.purgeAuthAttempts(ctx, time.Now())
	var left int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM auth_attempts WHERE ip = '203.0.113.99'`).Scan(&left); err != nil || left != 2 {
		t.Errorf("rows left %d (%v), want the two within the hour", left, err)
	}
}

// An account that cannot sign in is told whom to ask - the longest-standing
// enabled administrator - but only once the password is proved, and on the
// 202 of a registration waiting for approval.
func TestRefusedAccountsAreToldTheAdmin(t *testing.T) {
	h := newSignupHarness(t)
	h.modes(true, true)
	// The first account: an administrator, and the one to contact.
	h.register("10.0.0.1", "founder")
	// A later administrator, and an earlier one that is disabled: neither.
	h.user(sqlc.UserRoleAdmin, false, false)
	if _, err := h.pool.Exec(context.Background(),
		`UPDATE users SET created_at = now() - interval '1 year', disabled_at = now()
		 WHERE id = $1`, h.user(sqlc.UserRoleAdmin, false, false).ID); err != nil {
		t.Fatal(err)
	}

	type refusal struct {
		Code    string `json:"code"`
		Details struct {
			Admin *AdminContact `json:"admin"`
		} `json:"details"`
	}
	wantFounder := func(what string, admin *AdminContact) {
		t.Helper()
		if admin == nil || admin.Name != "founder" || admin.Email != "founder@test.invalid" {
			t.Errorf("%s: admin %+v, want founder", what, admin)
		}
	}

	rec := h.register("10.0.0.2", "waiting")
	var pending RegisterPending
	if err := json.Unmarshal(rec.Body.Bytes(), &pending); err != nil || rec.Code != http.StatusAccepted {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	wantFounder("the registration's 202", pending.Admin)

	var body refusal
	rec = h.login("10.0.0.3", "waiting", signupPassword)
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != "pending_approval" {
		t.Fatalf("pending login: %d %s", rec.Code, rec.Body)
	}
	wantFounder("pending_approval", body.Details.Admin)

	// Wrong password: nothing about the account, and no contact.
	rec = h.login("10.0.0.3", "waiting", "not the password")
	if strings.Contains(rec.Body.String(), "founder") {
		t.Errorf("a wrong password was told the admin: %s", rec.Body)
	}

	disabled := h.user(sqlc.UserRoleUser, false, true)
	body = refusal{}
	rec = h.login("10.0.0.4", disabled.Email, signupPassword)
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != "account_disabled" {
		t.Fatalf("disabled login: %d %s", rec.Code, rec.Body)
	}
	wantFounder("account_disabled", body.Details.Admin)

	// The shared error is not changed by attaching a contact to a copy.
	if errPendingApproval.Details != nil || errAccountDisabled.Details != nil {
		t.Error("the shared refusal errors were mutated")
	}
}
