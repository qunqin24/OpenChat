package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// Content moderation of a guest, over the real routes: the new message is
// checked before any row is written, strikes ban the network address, and a
// banned address is refused by every guest endpoint.

// guestModeration is a guest harness with moderation on against a fake OpenAI
// endpoint. Guests are told apart by the address in X-Forwarded-For, which the
// router's RealIP middleware reads as the client's.
func newGuestModeration(t *testing.T, tune map[string]any) (*guestHarness, *fakeModeration) {
	t.Helper()
	h := newGuestHarness(t)
	f := newFakeModeration(t)
	clear := func() {
		bg := context.Background()
		_, _ = h.pool.Exec(bg, `DELETE FROM app_settings WHERE key = ANY($1)`, moderationKeys)
		_, _ = h.pool.Exec(bg, `DELETE FROM guest_bans`)
		_, _ = h.pool.Exec(bg, `DELETE FROM moderation_events WHERE guest_ip IS NOT NULL`)
	}
	clear()
	t.Cleanup(clear)
	h.set(settingModerationMode, "openai")
	h.set(settingModerationOpenAIKey, "sk-mod-secret-1234")
	h.set(settingModerationOpenAIBaseURL, f.srv.URL+"/v1")
	for key, value := range tune {
		h.set(key, value)
	}
	return h, f
}

// as sends a request from a client address.
func (h *guestHarness) as(ip, method, path string, body any) (int, map[string]any, http.Header) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.http.URL+"/api/v1"+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	// A stream is not JSON; a refusal and the plain endpoints are.
	_ = json.Unmarshal(raw, &parsed)
	return resp.StatusCode, parsed, resp.Header
}

func (h *guestHarness) sendAs(ip, text string, files ...map[string]any) (int, map[string]any) {
	h.t.Helper()
	input := map[string]any{"id": uuid.New(), "role": "user", "content": text, "createdAt": time.Now().Format(time.RFC3339Nano), "parts": []any{}}
	if len(files) > 0 {
		input["attachments"] = files
	}
	status, body, _ := h.as(ip, http.MethodPost, "/guest/runs", map[string]any{
		"action": "send", "conversationId": uuid.New(), "model": h.model, "input": input,
	})
	return status, body
}

func (h *guestHarness) events(ip string) []struct {
	Source, Mode string
	Sanction     *string
	User         *uuid.UUID
	Conversation *uuid.UUID
} {
	h.t.Helper()
	rows, err := h.pool.Query(context.Background(),
		`SELECT source, mode, sanction, user_id, conversation_id FROM moderation_events WHERE guest_ip = $1 ORDER BY created_at, id`, ip)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rows.Close()
	var out []struct {
		Source, Mode string
		Sanction     *string
		User         *uuid.UUID
		Conversation *uuid.UUID
	}
	for rows.Next() {
		var e struct {
			Source, Mode string
			Sanction     *string
			User         *uuid.UUID
			Conversation *uuid.UUID
		}
		if err := rows.Scan(&e.Source, &e.Mode, &e.Sanction, &e.User, &e.Conversation); err != nil {
			h.t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func TestGuestModerationRefusesBeforeAnythingIsWritten(t *testing.T) {
	h, f := newGuestModeration(t, nil)
	const ip = "198.51.100.10"

	status, body := h.sendAs(ip, "please BLOCKME")
	if status != http.StatusUnprocessableEntity || body["code"] != "content_blocked" {
		t.Fatalf("%d %v", status, body)
	}
	if cats := body["details"].(map[string]any)["categories"].([]any); len(cats) != 1 || cats[0] != "violence/graphic" {
		t.Errorf("details %v", body["details"])
	}
	var users int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE guest`).Scan(&users)
	if users != 0 || len(h.requests()) != 0 || len(h.blobs.blobs) != 0 {
		t.Errorf("a refused guest message left %d guests, %d model requests, %d files", users, len(h.requests()), len(h.blobs.blobs))
	}
	events := h.events(ip)
	if len(events) != 1 || events[0].Source != "guest" || events[0].Mode != "openai" || events[0].User != nil || events[0].Conversation != nil {
		t.Fatalf("events %+v", events)
	}

	// A clean one is answered as ever.
	status, _ = h.sendAs(ip, "a fine question")
	if status != http.StatusOK {
		t.Fatalf("a clean message: %d", status)
	}
	if f.calls() != 2 {
		t.Errorf("%d checks", f.calls())
	}

	// A regeneration re-sends the history, and the history is the browser's: a
	// row that comes with no receipt is words nobody has checked
	// (guest_receipts_pg_test.go has the cases of one that has).
	calls := f.calls()
	user := userRow(uuid.New(), "BLOCKME was said long ago", time.Now().Add(-time.Minute))
	if status, _, _ := h.as(ip, http.MethodPost, "/guest/runs", map[string]any{
		"action": "regenerate", "conversationId": uuid.New(), "model": h.model, "messages": []any{user},
	}); status != http.StatusUnprocessableEntity {
		t.Errorf("regenerate: %d", status)
	}
	if f.calls() != calls+1 {
		t.Error("a regeneration of unchecked words was not checked")
	}
}

func TestGuestModerationChecksImagesAndAnswers(t *testing.T) {
	h, f := newGuestModeration(t, map[string]any{settingModerationCategories: []string{"sexual", "violence/graphic"}})
	const ip = "198.51.100.11"
	file := func(data string) map[string]any {
		return map[string]any{"id": uuid.New(), "filename": "x.png", "contentType": "image/png", "size": len(data),
			"data": base64.StdEncoding.EncodeToString([]byte(data))}
	}

	status, body := h.sendAs(ip, "look at this", file("\x89PNG BADIMG"))
	if status != http.StatusUnprocessableEntity || body["code"] != "content_blocked" {
		t.Fatalf("%d %v", status, body)
	}
	if len(h.blobs.blobs) != 0 {
		t.Error("a refused image was stored")
	}
	if last := f.images[len(f.images)-1]; len(last) != 1 || string(last[0]) != "\x89PNG BADIMG" {
		t.Errorf("the service was sent %q", last)
	}
	if status, _ := h.sendAs(ip, "look at this one", file("\x89PNG fine")); status != http.StatusOK {
		t.Errorf("a clean image: %d", status)
	}

	// The words typed in answer to a question are checked; the options are not.
	plan := func(other string) *guestPlan {
		var request GuestRunRequest
		request.Action = "answer"
		raw := `[{"selected":["City"]}]`
		if other != "" {
			raw = fmt.Sprintf(`[{"selected":["City"],"other":%q}]`, other)
		}
		if err := json.Unmarshal([]byte(raw), &request.Answers); err != nil {
			t.Fatal(err)
		}
		return &guestPlan{req: request}
	}
	ctx := context.Background()
	if err := h.server.moderateGuest(ctx, ip, plan("BLOCKME")); err == nil || !strings.Contains(err.Error(), "content_blocked") {
		t.Errorf("a typed answer: %v", err)
	}
	calls := f.calls()
	if err := h.server.moderateGuest(ctx, ip, plan("")); err != nil || f.calls() != calls {
		t.Errorf("an answer of options: %v, %d checks", err, f.calls()-calls)
	}
}

// A guest's message is never sent unchecked: when the service fails it is held
// back, whatever the setting for accounts.
func TestGuestModerationFailsClosed(t *testing.T) {
	h, f := newGuestModeration(t, map[string]any{settingModerationOnError: "allow"})
	const ip = "198.51.100.12"
	f.setStatus(http.StatusInternalServerError)
	status, body := h.sendAs(ip, "hello")
	if status != http.StatusServiceUnavailable || body["code"] != "moderation_unavailable" {
		t.Fatalf("%d %v", status, body)
	}
	if len(h.requests()) != 0 || len(h.events(ip)) != 0 {
		t.Error("a held-back message left a trace")
	}
}

// Enough blocked messages ban the address; every guest endpoint then refuses it,
// and only it.
func TestGuestModerationBansTheAddress(t *testing.T) {
	h, _ := newGuestModeration(t, nil)
	const ip, other = "198.51.100.13", "198.51.100.14"

	// Two are refused as blocked; the third, which bans the address, is
	// answered with the ban itself so the page shows it at once.
	for i := 1; i <= 3; i++ {
		want := http.StatusUnprocessableEntity
		if i == 3 {
			want = http.StatusForbidden
		}
		if status, body := h.sendAs(ip, fmt.Sprintf("BLOCKME %d", i)); status != want || (i == 3 && body["code"] != "guest_banned") {
			t.Fatalf("strike %d: %d %s", i, status, body)
		}
	}
	events := h.events(ip)
	if len(events) != 3 || events[2].Sanction == nil || *events[2].Sanction != "banned" || events[0].Sanction != nil {
		t.Fatalf("events %+v", events)
	}
	var expires time.Time
	var eventID *uuid.UUID
	if err := h.pool.QueryRow(context.Background(), `SELECT expires_at, event_id FROM guest_bans WHERE ip = $1`, ip).Scan(&expires, &eventID); err != nil {
		t.Fatal(err)
	}
	if d := time.Until(expires); d < 167*time.Hour || d > 169*time.Hour || eventID == nil {
		t.Errorf("ban expires in %v (event %v), want 7 days", d, eventID)
	}

	// Everything a banned guest could ask is refused with the time it ends.
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/guest/config"},
		{http.MethodPost, "/guest/runs"},
		{http.MethodPost, "/guest/title"},
		{http.MethodPost, "/guest/compaction"},
		{http.MethodGet, "/guest/images?url=http://example.com/a.png"},
		{http.MethodGet, "/guest/favicons/example.com"},
	} {
		status, body, _ := h.as(ip, c.method, c.path, map[string]any{})
		details, _ := body["details"].(map[string]any)
		until, _ := details["until"].(string)
		if status != http.StatusForbidden || body["code"] != "guest_banned" || until == "" {
			t.Errorf("%s %s: %d %v", c.method, c.path, status, body)
		}
		if at, err := time.Parse(time.RFC3339, until); err != nil || !at.Equal(expires.Truncate(time.Second)) && at.Sub(expires).Abs() > time.Second {
			t.Errorf("%s: until %q, ban ends %v", c.path, until, expires)
		}
	}
	// The status stays open and says so.
	status, body, _ := h.as(ip, http.MethodGet, "/guest/status", nil)
	if status != http.StatusOK || body["banned"] != true || body["bannedUntil"] == nil || body["enabled"] != true {
		t.Errorf("status %d %v", status, body)
	}
	// Stopping a run already going is allowed.
	if status, _, _ := h.as(ip, http.MethodPost, "/guest/runs/"+uuid.NewString()+"/cancel", nil); status != http.StatusAccepted {
		t.Errorf("cancel: %d", status)
	}
	// Another address is untouched.
	if status, body, _ := h.as(other, http.MethodGet, "/guest/status", nil); status != http.StatusOK || body["banned"] != false {
		t.Errorf("another address: %d %v", status, body)
	}
	if status, _, _ := h.as(other, http.MethodGet, "/guest/config", nil); status != http.StatusOK {
		t.Errorf("another address: config %d", status)
	}

	// A ban that has run out is ignored, whether or not it was cleaned up.
	if _, err := h.pool.Exec(context.Background(), `UPDATE guest_bans SET expires_at = now() - interval '1 minute' WHERE ip = $1`, ip); err != nil {
		t.Fatal(err)
	}
	if status, body, _ := h.as(ip, http.MethodGet, "/guest/status", nil); status != http.StatusOK || body["banned"] != false {
		t.Errorf("an expired ban: %d %v", status, body)
	}
	if status, _, _ := h.as(ip, http.MethodGet, "/guest/config", nil); status != http.StatusOK {
		t.Errorf("an expired ban: config %d", status)
	}
	h.server.purgeModeration(context.Background(), time.Now())
	var left int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM guest_bans WHERE ip = $1`, ip).Scan(&left)
	if left != 0 {
		t.Error("the cleanup left an expired ban")
	}
}

func TestGuestBanLengthAndNetworks(t *testing.T) {
	// 0 hours is for good, and the ban does not say when it ends.
	h, _ := newGuestModeration(t, map[string]any{settingGuestBanHours: 0, settingModerationStrikes: 1})
	const v6 = "2001:db8:1:2:3:4:5:6"
	if status, body := h.sendAs(v6, "BLOCKME"); status != http.StatusForbidden || body["code"] != "guest_banned" || body["details"].(map[string]any)["until"] != nil {
		t.Fatalf("the banning message: %d %s", status, body)
	}
	var ip string
	var expires *time.Time
	if err := h.pool.QueryRow(context.Background(), `SELECT ip, expires_at FROM guest_bans`).Scan(&ip, &expires); err != nil {
		t.Fatal(err)
	}
	// An IPv6 address is its /64, which is the unit the limits count by.
	if ip != "2001:db8:1:2::/64" || expires != nil {
		t.Fatalf("ban %q expires %v", ip, expires)
	}
	// Any address of the network is the banned one.
	status, body, _ := h.as("2001:db8:1:2:9:9:9:9", http.MethodGet, "/guest/config", nil)
	details, _ := body["details"].(map[string]any)
	if status != http.StatusForbidden || body["code"] != "guest_banned" {
		t.Fatalf("%d %v", status, body)
	}
	if until, present := details["until"]; !present || until != nil {
		t.Errorf("a permanent ban's details: %v", details)
	}
	if status, _, _ := h.as("2001:db8:1:3::1", http.MethodGet, "/guest/config", nil); status != http.StatusOK {
		t.Errorf("the next network: %d", status)
	}

	// A new ban never shortens an old one.
	later := pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}
	if _, err := h.server.q.UpsertGuestBan(context.Background(), sqlc.UpsertGuestBanParams{Ip: ip, ExpiresAt: later, Reason: "again"}); err != nil {
		t.Fatal(err)
	}
	if err := h.pool.QueryRow(context.Background(), `SELECT expires_at FROM guest_bans WHERE ip = $1`, ip).Scan(&expires); err != nil || expires != nil {
		t.Errorf("a repeat ban %v, %v", expires, err)
	}
}
