package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

func (h *askHarness) moderationSettings() ModerationSettings {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.server.GetModerationSettings(rec, h.request(http.MethodGet, ""))
	var s ModerationSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil || rec.Code != http.StatusOK {
		h.t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
	return s
}

func TestModerationSettingsDefaults(t *testing.T) {
	h := newAskHarness(t, openAIWire, askThenText(openAIWire))
	resetModeration(t, h)
	s := h.moderationSettings()
	cats := []string{}
	for _, c := range s.Categories {
		cats = append(cats, string(c))
	}
	if s.Mode != ModerationModeOff || s.OpenaiKeySet || s.OpenaiBaseUrl != "" || s.Model != "" || s.MinScore != 0 || s.ExemptAdmins ||
		s.OnError != ModerationOnErrorAllow || s.Strikes != 3 || s.StrikeWindowHours != 24 ||
		strings.Join(cats, ",") != "sexual/minors,harassment/threatening,hate/threatening,illicit/violent,self-harm/instructions,violence/graphic" {
		t.Errorf("defaults %+v", s)
	}
	// Off is off: nothing is asked of anyone and a message is not looked at.
	if rec := h.post("BLOCKME"); rec.Code != http.StatusAccepted {
		t.Errorf("moderation off: %d %s", rec.Code, rec.Body)
	}
}

// Every setting is validated against what it would leave behind, with a 400 code
// of its own, and nothing is saved from a request that has one bad field.
func TestModerationSettingsValidation(t *testing.T) {
	h := newAskHarness(t, openAIWire, askThenText(openAIWire))
	resetModeration(t, h)

	for name, c := range map[string]struct{ body, code string }{
		"unknown mode":          {`{"mode":"loud"}`, "invalid_moderation_mode"},
		"openai without a key":  {`{"mode":"openai"}`, "moderation_key_required"},
		"model without a model": {`{"mode":"model"}`, "moderation_model_required"},
		"unknown category":      {`{"categories":["sexual","nonsense"]}`, "invalid_moderation_category"},
		"score above 1":         {`{"minScore":1.5}`, "invalid_moderation_score"},
		"negative score":        {`{"minScore":-0.1}`, "invalid_moderation_score"},
		"unknown on-error":      {`{"onError":"shrug"}`, "invalid_moderation_on_error"},
		"negative strikes":      {`{"strikes":-1}`, "invalid_moderation_strikes"},
		"too many strikes":      {`{"strikes":100000}`, "invalid_moderation_strikes"},
		"empty window":          {`{"strikeWindowHours":0}`, "invalid_moderation_window"},
		"huge window":           {`{"strikeWindowHours":100000}`, "invalid_moderation_window"},
		"not a URL":             {`{"openaiBaseUrl":"relay.example.com"}`, "invalid_moderation_base_url"},
		"one bad field of two":  {`{"strikes":9,"minScore":7}`, "invalid_moderation_score"},
	} {
		rec := h.patchModeration(c.body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"`+c.code+`"`) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if s := h.moderationSettings(); s.Strikes != 3 || s.MinScore != 0 || s.Mode != ModerationModeOff {
		t.Errorf("a refused change was saved: %+v", s)
	}

	// A good change sticks; categories are deduplicated and kept in list order;
	// a key is kept when the field is absent and cleared by an empty one.
	rec := h.patchModeration(`{"mode":"openai","openaiApiKey":" sk-abcd1234 ","openaiBaseUrl":"https://relay.example.com/v1/",
		"categories":["violence","sexual","violence"],"minScore":0.4,"exemptAdmins":true,"onError":"block","strikes":5,
		"strikeWindowHours":6}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "sk-abcd1234") {
		t.Fatal("the key came back")
	}
	s := h.moderationSettings()
	if s.Mode != ModerationModeOpenai || !s.OpenaiKeySet || s.OpenaiBaseUrl != "https://relay.example.com/v1" ||
		len(s.Categories) != 2 || s.Categories[0] != ModerationCategorySexual || s.Categories[1] != ModerationCategoryViolence ||
		s.MinScore != 0.4 || !s.ExemptAdmins || s.OnError != ModerationOnErrorBlock || s.Strikes != 5 || s.StrikeWindowHours != 6 {
		t.Errorf("settings %+v", s)
	}
	if h.patchModeration(`{"strikes":4}`).Code != http.StatusOK || !h.moderationSettings().OpenaiKeySet {
		t.Error("the key was lost by a change that did not mention it")
	}
	// Taking the key away under openai mode is refused; turning the mode off first is not.
	if rec := h.patchModeration(`{"openaiApiKey":""}`); rec.Code != http.StatusBadRequest {
		t.Errorf("clearing the key of a mode that needs it: %d", rec.Code)
	}
	if rec := h.patchModeration(`{"mode":"off","openaiApiKey":""}`); rec.Code != http.StatusOK || h.moderationSettings().OpenaiKeySet {
		t.Errorf("clearing the key: %d %s", rec.Code, rec.Body)
	}
	// Whatever is in the database, the key is not in what the API says.
	raw, _ := json.Marshal(h.moderationSettings())
	if strings.Contains(string(raw), "abcd1234") {
		t.Error("the key is in the settings")
	}
}

func pgTime(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func (h *askHarness) addEvent(userID *uuid.UUID, at time.Time, categories ...string) uuid.UUID {
	h.t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(context.Background(), `
		INSERT INTO moderation_events (user_id, created_at, source, mode, categories, excerpt)
		VALUES ($1, $2, 'send', 'openai', $3, $4) RETURNING id`,
		userID, at, categories, "text of "+strings.Join(categories, "+")).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

func (h *askHarness) listEvents(query string) ModerationEventPage {
	h.t.Helper()
	q, _ := url.ParseQuery(query)
	var params ListModerationEventsParams
	if v := q.Get("limit"); v != "" {
		var n int
		_, _ = fmt.Sscan(v, &n)
		params.Limit = &n
	}
	if v := q.Get("cursor"); v != "" {
		params.Cursor = &v
	}
	if v := q.Get("category"); v != "" {
		c := ModerationCategory(v)
		params.Category = &c
	}
	if v := q.Get("userId"); v != "" {
		id := uuid.MustParse(v)
		params.UserId = &id
	}
	rec := httptest.NewRecorder()
	h.server.ListModerationEvents(rec, h.request(http.MethodGet, ""), params)
	var page ModerationEventPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != http.StatusOK {
		h.t.Fatalf("events: %d %s", rec.Code, rec.Body)
	}
	return page
}

// The log pages newest first by cursor, filters by account and category, and
// says who and what for each row.
func TestModerationEventLog(t *testing.T) {
	h := newAskHarness(t, openAIWire, askThenText(openAIWire))
	resetModeration(t, h)
	ctx := context.Background()
	tag := uuid.NewString()
	var other uuid.UUID
	if err := h.pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		tag+"@test.invalid", "log-"+tag).Scan(&other); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM moderation_events WHERE user_id = $1`, other)
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, other)
	})

	now := time.Now()
	var ids []uuid.UUID
	ids = append(ids, h.addEvent(&h.user.ID, now.Add(-5*time.Minute), "violence/graphic"))
	ids = append(ids, h.addEvent(&h.user.ID, now.Add(-4*time.Minute), "sexual/minors", "violence/graphic"))
	ids = append(ids, h.addEvent(&other, now.Add(-3*time.Minute), "hate/threatening"))
	ids = append(ids, h.addEvent(&other, now.Add(-2*time.Minute), "violence/graphic"))
	if _, err := h.pool.Exec(ctx, `UPDATE moderation_events SET sanction = 'disabled' WHERE id = $1`, ids[3]); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE users SET disabled_at = now() WHERE id = $1`, other); err != nil {
		t.Fatal(err)
	}

	// By the account: its two, newest first, who named.
	page := h.listEvents("userId=" + h.user.ID.String())
	if len(page.Items) != 2 || page.Items[0].Id != ids[1] || page.Items[1].Id != ids[0] || page.NextCursor != nil {
		t.Fatalf("by account: %+v", page)
	}
	first := page.Items[0]
	if first.UserName == nil || *first.UserName != h.user.Name || first.UserEmail == nil ||
		first.UserDisabled == nil || *first.UserDisabled ||
		len(first.Categories) != 2 || first.Source != ModerationSourceSend || !strings.HasPrefix(first.Excerpt, "text of") {
		t.Errorf("an account's row: %+v", first)
	}

	// A sanctioned account's row says so, and that the account is disabled now.
	page = h.listEvents("userId=" + other.String())
	if len(page.Items) != 2 || page.Items[0].Id != ids[3] || page.Items[1].Id != ids[2] {
		t.Fatalf("the other account: %+v", page)
	}
	row := page.Items[0]
	if row.UserDisabled == nil || !*row.UserDisabled || row.Sanction == nil || *row.Sanction != ModerationSanctionDisabled {
		t.Errorf("a sanctioned row: %+v", row)
	}

	// By category, and the filters meet.
	page = h.listEvents("category=sexual/minors")
	found := false
	for _, item := range page.Items {
		found = found || item.Id == ids[1]
	}
	if !found {
		t.Errorf("by category: %+v", page)
	}
	page = h.listEvents("category=hate/threatening&userId=" + other.String())
	if len(page.Items) != 1 || page.Items[0].Id != ids[2] {
		t.Errorf("category and account: %+v", page)
	}

	// Paging: one at a time, by cursor, nothing twice and nothing missed.
	var seen []uuid.UUID
	cursor := ""
	for range 10 {
		q := "limit=1&userId=" + other.String()
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		page = h.listEvents(q)
		for _, item := range page.Items {
			seen = append(seen, item.Id)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 2 || seen[0] != ids[3] || seen[1] != ids[2] {
		t.Errorf("paged %v", seen)
	}
	// A bad filter is a 400.
	rec := httptest.NewRecorder()
	bad := ModerationCategory("nope")
	h.server.ListModerationEvents(rec, h.request(http.MethodGet, ""), ListModerationEventsParams{Category: &bad})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a bad category: %d", rec.Code)
	}
}

// The admin endpoints are the administrators': no session is a 401, and a
// member sees nothing there.
func TestModerationEndpointsAreForAdministrators(t *testing.T) {
	h := newAskHarness(t, openAIWire, askThenText(openAIWire))
	resetModeration(t, h)
	ctx := context.Background()
	srv := httptest.NewServer(NewRouter(&config.Config{}, h.server, auth.NewMiddleware(h.q)))
	t.Cleanup(srv.Close)

	session := func(role sqlc.UserRole) *http.Cookie {
		tag := uuid.NewString()
		var id uuid.UUID
		if err := h.pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, $3) RETURNING id`,
			tag+"@test.invalid", "route-"+tag, role).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = h.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id) })
		token, hash, err := auth.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.q.CreateSession(ctx, sqlc.CreateSessionParams{UserID: id, TokenHash: hash, ExpiresAt: pgTime(time.Now().Add(time.Hour)), UserAgent: "test"}); err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: auth.CookieName, Value: token}
	}
	member, admin := session(sqlc.UserRoleUser), session(sqlc.UserRoleAdmin)

	paths := []struct{ method, path string }{
		{http.MethodGet, "/admin/moderation"},
		{http.MethodPatch, "/admin/moderation"},
		{http.MethodGet, "/admin/moderation/events"},
	}
	do := func(method, path string, cookie *http.Cookie) int {
		req, _ := http.NewRequest(method, srv.URL+"/api/v1"+path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	for _, p := range paths {
		if got := do(p.method, p.path, nil); got != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", p.method, p.path, got)
		}
		if got := do(p.method, p.path, member); got != http.StatusNotFound {
			t.Errorf("%s %s as a member: %d, want 404", p.method, p.path, got)
		}
	}
	for _, p := range paths {
		if p.method == http.MethodPatch {
			continue
		}
		if got := do(p.method, p.path, admin); got != http.StatusOK {
			t.Errorf("%s %s as an administrator: %d", p.method, p.path, got)
		}
	}
}

// The log is kept 90 days.
func TestModerationRetention(t *testing.T) {
	h := newAskHarness(t, openAIWire, askThenText(openAIWire))
	resetModeration(t, h)
	ctx := context.Background()
	old := h.addEvent(&h.user.ID, time.Now().Add(-91*24*time.Hour), "violence/graphic")
	recent := h.addEvent(&h.user.ID, time.Now().Add(-89*24*time.Hour), "violence/graphic")

	h.server.purgeModeration(ctx, time.Now())

	exists := func(id uuid.UUID) bool {
		return h.count(`SELECT count(*) FROM moderation_events WHERE id = $1`, id) == 1
	}
	if exists(old) || !exists(recent) {
		t.Errorf("old kept: %v, recent kept: %v", exists(old), exists(recent))
	}
}
