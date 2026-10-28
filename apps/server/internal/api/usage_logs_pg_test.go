package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// The usage ledger from the user's side to the operator's: messages are sent,
// the model answers, fails or titles, and the admin list reports each request
// with its tokens, cost and latency. Admin-only, like the rest of /admin.
func TestUsageLedgerEndToEnd(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	tag := uuid.NewString()[:8]
	chatModel, badModel := "ledger-chat-"+tag, "ledger-bad-"+tag

	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		if body.Model == badModel {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key provided: sk-abcdef1234567890","code":"invalid_api_key"}}`)
			return
		}
		text := "A fine answer."
		if strings.Contains(string(raw), "Write a short title") {
			text = "Fine"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		time.Sleep(30 * time.Millisecond) // a first token that takes a moment
		frame := func(v any) {
			b, _ := json.Marshal(v)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
		}
		frame(map[string]any{"id": "r", "model": body.Model + "-served", "choices": []any{
			map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}, "finish_reason": nil}}})
		frame(map[string]any{"id": "r", "model": body.Model + "-served", "choices": []any{
			map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			"usage": map[string]any{"prompt_tokens": 1000, "completion_tokens": 200, "total_tokens": 1200,
				"completion_tokens_details": map[string]any{"reasoning_tokens": 50},
				"prompt_tokens_details":     map[string]any{"cached_tokens": 400}}})
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(endpoint.Close)

	newUser := func(role string) sqlc.User {
		var u sqlc.User
		id := uuid.NewString()
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, $3::user_role) RETURNING id, email, name, role`,
			id+"@test.invalid", role+"-"+tag, role).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		return u
	}
	admin, member := newUser("admin"), newUser("user")

	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "ledger channel " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
		BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{chatModel, badModel}, DisplayNames: []string{"", ""}, Sources: []string{"manual", "manual"},
	}); err != nil {
		t.Fatal(err)
	}
	// The catalogue prices the chat model: $3 in, $15 out, $0.3 cached, per million.
	if _, err := pool.Exec(ctx, `INSERT INTO catalog_models (provider_id, model_id, name, raw) VALUES ($1, $2, $2, $3::jsonb)`,
		"ledger-"+tag, chatModel, `{"cost":{"input":3,"output":15,"cache_read":0.3}}`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, "DELETE FROM usage_logs WHERE model_id = ANY($1)", []string{chatModel, badModel})
		_, _ = pool.Exec(bg, "DELETE FROM catalog_models WHERE provider_id = $1", "ledger-"+tag)
		_, _ = pool.Exec(bg, "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{admin.ID, member.ID})
	})

	runner := agent.NewRunner(pool, agent.NewHub(), NewModelResolver(pool),
		func(context.Context) []agent.Tool { return nil },
		agent.Options{SystemPrompt: NewSystemPrompt(pool), PartialInterval: 5 * time.Millisecond})
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	cfg := &config.Config{CORSOrigins: []string{"http://localhost"}}
	server := NewServer(cfg, pool, nil, runner)
	runner.AfterRun(server.TitleAfterRun)
	web := httptest.NewServer(NewRouter(cfg, server, auth.NewMiddleware(q)))
	t.Cleanup(web.Close)

	send := func(user sqlc.User, model string) {
		t.Helper()
		conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: defaultTitle})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"content":"hello","model":%q}`, model)))
		rec := httptest.NewRecorder()
		server.CreateMessage(rec, req.WithContext(auth.WithUser(req.Context(), user)), conv.ID)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("send: %d %s", rec.Code, rec.Body)
		}
	}
	send(admin, chatModel) // a chat row, then a title row
	send(member, badModel) // an auth failure, and the title attempted after it (which fails the same way)

	count := func(where string, args ...any) (n int) {
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM usage_logs WHERE "+where, args...).Scan(&n)
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for count("model_id = ANY($1)", []string{chatModel, badModel}) < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("the ledger has %d rows after 10s, want 4", count("model_id = ANY($1)", []string{chatModel, badModel}))
		}
		time.Sleep(20 * time.Millisecond)
	}

	session := func(user sqlc.User) *http.Cookie {
		token, hash, err := auth.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.CreateSession(ctx, sqlc.CreateSessionParams{UserID: user.ID, TokenHash: hash,
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}}); err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: auth.CookieName, Value: token}
	}
	adminCookie, memberCookie := session(admin), session(member)
	get := func(cookie *http.Cookie, query url.Values) (int, UsageLogPage) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, web.URL+"/api/v1/admin/usage-logs?"+query.Encode(), nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var page UsageLogPage
		if res.StatusCode == http.StatusOK {
			if err := json.NewDecoder(res.Body).Decode(&page); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode, page
	}
	// A member is told there is no such page; nobody at all is told to sign in.
	if code, _ := get(memberCookie, url.Values{}); code != http.StatusNotFound {
		t.Errorf("member: %d, want 404", code)
	}
	if code, _ := get(nil, url.Values{}); code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d, want 401", code)
	}

	// The chat request: priced from the catalogue, with the owner's name and
	// the channel's, the model the provider served, and a first-token time.
	code, page := get(adminCookie, url.Values{"model": {chatModel}, "kind": {"chat"}})
	if code != http.StatusOK || len(page.Items) != 1 || page.Summary.Requests != 1 {
		t.Fatalf("chat rows: %d, %d items, total %d", code, len(page.Items), page.Summary.Requests)
	}
	row := page.Items[0]
	if row.Status != UsageStatus("ok") || row.Kind != UsageKind("chat") || row.Model != chatModel || row.ServedModel != chatModel+"-served" {
		t.Errorf("row = %+v", row)
	}
	if row.InputTokens != 600 || row.OutputTokens != 200 || row.ReasoningTokens != 50 || row.CacheReadTokens != 400 {
		t.Errorf("tokens in %d out %d reasoning %d cached %d; input excludes the cached 400",
			row.InputTokens, row.OutputTokens, row.ReasoningTokens, row.CacheReadTokens)
	}
	if row.InputPrice == nil || *row.InputPrice != 3 || row.OutputPrice == nil || *row.OutputPrice != 15 ||
		row.CacheReadPrice == nil || *row.CacheReadPrice != 0.3 || row.CacheWritePrice != nil {
		t.Errorf("prices = %v %v %v %v", row.InputPrice, row.OutputPrice, row.CacheReadPrice, row.CacheWritePrice)
	}
	if want := (600*3 + 200*15 + 400*0.3) / 1e6; row.Cost == nil || math.Abs(*row.Cost-want) > 1e-12 {
		t.Errorf("cost = %v, want %v", row.Cost, want)
	}
	if row.FirstTokenMs == nil || *row.FirstTokenMs < 25 || row.DurationMs < *row.FirstTokenMs {
		t.Errorf("first token %v ms, total %d ms", row.FirstTokenMs, row.DurationMs)
	}
	if row.UserId == nil || *row.UserId != admin.ID || row.UserName == nil || *row.UserName != admin.Name ||
		row.UserEmail == nil || *row.UserEmail != admin.Email {
		t.Errorf("user = %v %v %v", row.UserId, row.UserName, row.UserEmail)
	}
	if row.ChannelName != channel.Name || row.ChannelId == nil || *row.ChannelId != channel.ID || row.RunId == nil || row.ConversationId == nil {
		t.Errorf("origin = %q %v %v %v", row.ChannelName, row.ChannelId, row.RunId, row.ConversationId)
	}
	if page.Summary.Requests != 1 || page.Summary.Cost == 0 || page.Summary.InputTokens != 600 {
		t.Errorf("summary = %+v", page.Summary)
	}
	// The cache figures: 400 of the 1000 prompt tokens were read from the
	// cache (600 sent plus 400 read), at $0.3 instead of $3 per million.
	sum := page.Summary
	if sum.CacheReadTokens != 400 || sum.CacheWriteTokens != 0 ||
		sum.CacheHitRate == nil || math.Abs(*sum.CacheHitRate-0.4) > 1e-9 ||
		sum.CacheSavings == nil || math.Abs(*sum.CacheSavings-400*(3-0.3)/1e6) > 1e-12 {
		t.Errorf("cache summary: read %d write %d rate %v savings %v", sum.CacheReadTokens, sum.CacheWriteTokens, sum.CacheHitRate, sum.CacheSavings)
	}

	// The title request is the owner's, kind `title`, priced like any other.
	_, titles := get(adminCookie, url.Values{"model": {chatModel}, "kind": {"title"}})
	if len(titles.Items) != 1 || titles.Items[0].UserId == nil || *titles.Items[0].UserId != admin.ID || titles.Items[0].Cost == nil {
		t.Fatalf("title rows = %+v", titles.Items)
	}

	// The failure: an error row, the code the chat shows, the key scrubbed, no cost.
	_, failed := get(adminCookie, url.Values{"status": {"error"}, "model": {badModel}, "kind": {"chat"}})
	if len(failed.Items) != 1 {
		t.Fatalf("error rows = %+v", failed.Items)
	}
	bad := failed.Items[0]
	if bad.ErrorCode == nil || *bad.ErrorCode != agent.CodeModelAuth || bad.Cost != nil || bad.UserId == nil || *bad.UserId != member.ID {
		t.Errorf("error row = %+v", bad)
	}
	if bad.ErrorMessage == nil || !strings.Contains(*bad.ErrorMessage, "invalid_api_key") || strings.Contains(*bad.ErrorMessage, "sk-abcdef") {
		t.Errorf("error message %v", bad.ErrorMessage)
	}
	if failed.Summary.Errors != 1 {
		t.Errorf("summary = %+v", failed.Summary)
	}
	if s := failed.Summary; s.CacheHitRate != nil || s.CacheSavings != nil || s.CacheReadTokens != 0 {
		t.Errorf("a failure sent no tokens, so has no hit rate: %+v", s)
	}

	// Filters combine and are scoped by the other columns.
	_, byUser := get(adminCookie, url.Values{"userId": {member.ID.String()}})
	if byUser.Summary.Requests != 2 || byUser.Items[0].Model != badModel || byUser.Items[1].Model != badModel {
		t.Errorf("by user: %+v", byUser)
	}
	_, byChannel := get(adminCookie, url.Values{"channelId": {channel.ID.String()}, "status": {"ok"}})
	if byChannel.Summary.Requests != 2 {
		t.Errorf("by channel and status: %d, want 2", byChannel.Summary.Requests)
	}
	_, byTime := get(adminCookie, url.Values{"channelId": {channel.ID.String()}, "to": {time.Now().Add(-time.Hour).Format(time.RFC3339)}})
	if byTime.Summary.Requests != 0 || len(byTime.Items) != 0 {
		t.Errorf("before it all: %+v", byTime)
	}
	_, inRange := get(adminCookie, url.Values{"channelId": {channel.ID.String()},
		"from": {time.Now().Add(-time.Hour).Format(time.RFC3339)}, "to": {time.Now().Add(time.Hour).Format(time.RFC3339)}})
	if inRange.Summary.Requests != 4 {
		t.Errorf("in range: %d, want 4", inRange.Summary.Requests)
	}

	// Paging: newest first, by cursor, and the summary covers every page.
	_, first := get(adminCookie, url.Values{"channelId": {channel.ID.String()}, "limit": {"3"}})
	if first.NextCursor == nil {
		t.Fatalf("a first page of 3 of 4 has no next cursor: %+v", first)
	}
	_, second := get(adminCookie, url.Values{"channelId": {channel.ID.String()}, "limit": {"3"}, "cursor": {*first.NextCursor}})
	if first.Summary.Requests != 4 || len(first.Items) != 3 || len(second.Items) != 1 || second.NextCursor != nil || second.Summary.Requests != 4 {
		t.Fatalf("paging: %d + %d items, totals %d and %d", len(first.Items), len(second.Items), first.Summary.Requests, second.Summary.Requests)
	}
	all := append(append([]UsageLog{}, first.Items...), second.Items...)
	for i := 1; i < len(all); i++ {
		if all[i].CreatedAt.After(all[i-1].CreatedAt) {
			t.Errorf("not newest first at %d", i)
		}
	}

	// Bad filter values are rejected, not ignored.
	if code, _ := get(adminCookie, url.Values{"status": {"weird"}}); code != http.StatusBadRequest {
		t.Errorf("bad status: %d, want 400", code)
	}

	// Deleting the channel keeps what was spent through it, under the name it had.
	if _, err := pool.Exec(ctx, "DELETE FROM channels WHERE id = $1", channel.ID); err != nil {
		t.Fatal(err)
	}
	_, after := get(adminCookie, url.Values{"model": {chatModel}, "kind": {"chat"}})
	if len(after.Items) != 1 || after.Items[0].ChannelId != nil || after.Items[0].ChannelName != channel.Name {
		t.Errorf("after the channel was deleted: %+v", after.Items)
	}
}
