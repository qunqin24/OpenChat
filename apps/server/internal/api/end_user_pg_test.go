package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	ledger "github.com/qunqin/openchat/apps/server/internal/usage"
)

func TestEndUserIdentifiers(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	users := NewEndUsers(pool)

	a, b := uuid.New(), uuid.New()
	idA := users.Account(ctx, a)
	if len(idA) != endUserLength || strings.Trim(idA, "0123456789abcdef") != "" {
		t.Fatalf("identifier %q is not %d hex characters", idA, endUserLength)
	}
	if again := users.Account(ctx, a); again != idA {
		t.Errorf("an account's identifier changed: %q then %q", idA, again)
	}
	// A second process (or a restart) reads the same secret from the database.
	if other := NewEndUsers(pool).Account(ctx, a); other != idA {
		t.Errorf("another EndUsers made %q for the same account, want %q", other, idA)
	}
	if users.Account(ctx, b) == idA {
		t.Error("two accounts share an identifier")
	}
	guest := users.Guest(ctx, "203.0.113.7")
	if guest == "" || guest == idA || guest == users.Guest(ctx, "203.0.113.8") {
		t.Errorf("guest identifiers are not told apart: %q", guest)
	}
	if guest != users.Guest(ctx, "203.0.113.7") {
		t.Error("a guest address's identifier changed")
	}
	if strings.Contains(idA, a.String()) || strings.Contains(guest, "203.0.113.7") {
		t.Error("the identifier carries what it was made from")
	}
	// A run is for the address it came from when it is a guest's, else the account.
	ip := "203.0.113.7"
	if got := users.ForRun(ctx, sqlc.Run{UserID: uuid.New(), ClientIp: &ip}); got != guest {
		t.Errorf("a guest's run: %q, want the address's %q", got, guest)
	}
	if got := users.ForRun(ctx, sqlc.Run{UserID: a}); got != idA {
		t.Errorf("an account's run: %q, want %q", got, idA)
	}
	// Nobody has no identifier; neither has an unreadable secret's request.
	if users.Account(ctx, uuid.Nil) != "" || users.Guest(ctx, "") != "" {
		t.Error("an identifier was made for nobody")
	}

	// The secret is in app_settings, and the settings API does not carry it.
	q := sqlc.New(pool)
	secret, err := stringSetting(ctx, q, settingEndUserKey)
	if err != nil || len(secret) != 64 {
		t.Fatalf("stored secret %q, err %v", secret, err)
	}
	server := &Server{pool: pool, q: q}
	rec := httptest.NewRecorder()
	server.GetSystemSettings(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rec.Body.String(), secret) {
		t.Error("the system settings carry the end-user secret")
	}
}

// A title request carries the person's identifier on a channel whose switch is
// on, and leaves it out where it is off.
func TestTitleRequestCarriesTheEndUser(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	var (
		mu     sync.Mutex
		bodies []map[string]any
	)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		chunk, _ := json.Marshal(map[string]any{"id": "r", "model": "m", "choices": []any{
			map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": "A title"}, "finish_reason": "stop"},
		}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	}))
	t.Cleanup(endpoint.Close)

	server := &Server{pool: pool, q: q, usage: ledger.NewRecorder(q), endUsers: NewEndUsers(pool)}
	user := uuid.New()
	for _, on := range []bool{true, false} {
		tag := uuid.NewString()
		modelID := "enduser-model-" + tag
		channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
			Name: "fake " + tag, Type: sqlc.ChannelTypeOpenai, Enabled: true,
			BaseUrl: endpoint.URL + "/v1", ApiKey: "sk-test", SendUserID: on,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID) })
		if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
			ChannelID: channel.ID, ModelIds: []string{modelID}, DisplayNames: []string{""}, Sources: []string{"manual"},
		}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		bodies = nil
		mu.Unlock()
		if _, err := server.generateTitle(ctx, uuid.Nil, server.endUsers.Account(ctx, user), uuid.Nil, modelID, &channel.ID, "<user>hi</user>"); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got := bodies
		mu.Unlock()
		if len(got) != 1 {
			t.Fatalf("switch %v: %d requests", on, len(got))
		}
		sent, has := got[0]["safety_identifier"]
		if on && sent != server.endUsers.Account(ctx, user) {
			t.Errorf("switch on: safety_identifier = %v", sent)
		}
		if !on && has {
			t.Errorf("switch off: safety_identifier = %v", sent)
		}
	}
}
