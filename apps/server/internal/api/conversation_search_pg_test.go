package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// Search finds a conversation by its title or by anything said in it, only the
// caller's, and treats what is typed literally.
func TestSearchConversations(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	server := &Server{pool: pool, q: q}

	tag := uuid.NewString()[:8]
	newUser := func(name string) sqlc.User {
		var u sqlc.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id, email, name, role`,
			name+tag+"@test.invalid", name+tag).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", u.ID) })
		return u
	}
	alice, bob := newUser("alice"), newUser("bob")
	conv := func(owner sqlc.User, title string, temporary bool, said ...string) uuid.UUID {
		c, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &owner.ID, Title: title, Temporary: temporary})
		if err != nil {
			t.Fatal(err)
		}
		for i, text := range said {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			if _, err := pool.Exec(ctx, `INSERT INTO messages (conversation_id, role, content) VALUES ($1, $2, $3)`, c.ID, role, text); err != nil {
				t.Fatal(err)
			}
		}
		return c.ID
	}
	byTitle := conv(alice, "Trip to Kyoto "+tag, false)
	byWords := conv(alice, "Untitled", false, "how do I bake bread", "Knead the dough for ten minutes, then let it rise "+tag+" until doubled in size.")
	conv(alice, "Kyoto again "+tag, true) // temporary: not history
	conv(bob, "Kyoto "+tag, false)        // someone else's
	percent := conv(alice, "Discount", false, "is it 50% off "+tag+"?")

	search := func(term string) []Conversation {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		server.ListConversations(rec, req.WithContext(auth.WithUser(req.Context(), alice)), ListConversationsParams{Search: &term})
		if rec.Code != http.StatusOK {
			t.Fatalf("search %q: %d %s", term, rec.Code, rec.Body)
		}
		var page ConversationPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page.Items
	}

	if got := search("kyoto " + tag); len(got) != 1 || got[0].Id != byTitle || got[0].Match != nil {
		t.Errorf("by title: %+v", got)
	}
	got := search("RISE " + tag)
	if len(got) != 1 || got[0].Id != byWords || got[0].Match == nil || got[0].Match.Role != ConversationMatchRoleAssistant {
		t.Fatalf("by what was said: %+v", got)
	}
	if excerpt := got[0].Match.Excerpt; !strings.HasPrefix(excerpt, "…") || !strings.Contains(excerpt, "rise "+tag) {
		t.Errorf("excerpt %q: want the words around the match, cut at the front", excerpt)
	}
	// % is a character to find, not a wildcard.
	if got := search("50% off " + tag); len(got) != 1 || got[0].Id != percent {
		t.Errorf("literal percent: %+v", got)
	}
	if got := search("5_% off " + tag); len(got) != 0 {
		t.Errorf("underscore matched as a wildcard: %+v", got)
	}
}

func TestExcerpt(t *testing.T) {
	for _, c := range []struct{ text, needle, want string }{
		{"short line", "line", "short line"},
		{"one\n\ntwo   three", "TWO", "one two three"},
		{"前面有一些很长很长的文字需要被省略掉才能看到我们要找的那个关键词，后面还有", "关键词", "…很长很长的文字需要被省略掉才能看到我们要找的那个关键词，后面还有"},
	} {
		if got := excerpt(c.text, c.needle); got != c.want {
			t.Errorf("excerpt(%q, %q) = %q, want %q", c.text, c.needle, got, c.want)
		}
	}
}
