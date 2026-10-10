package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// The channel list carries what each channel has cost: every priced request,
// all time; unpriced ones add nothing, and an unused channel reads zero.
func TestChannelListShowsSpend(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	server := &Server{pool: pool, q: q}

	tag := uuid.NewString()[:8]
	newChannel := func(name string) uuid.UUID {
		c, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
			Name: name + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true, BaseUrl: "http://127.0.0.1:1/v1",
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", c.ID) })
		return c.ID
	}
	used, idle := newChannel("used"), newChannel("idle")
	for _, cost := range []any{0.5, 1.25, nil} {
		if _, err := pool.Exec(ctx, `INSERT INTO usage_logs (kind, status, channel_id, channel_name, model_id, served_model, cost)
			VALUES ('chat', 'ok', $1, 'used', 'm', 'm', $2)`, used, cost); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM usage_logs WHERE channel_id = $1", used) })

	rec := httptest.NewRecorder()
	server.ListChannels(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var channels []Channel
	if err := json.Unmarshal(rec.Body.Bytes(), &channels); err != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	got := map[uuid.UUID]float64{}
	for _, c := range channels {
		if c.Spent == nil {
			t.Fatalf("channel %s has no spend", c.Name)
		}
		got[c.Id] = *c.Spent
	}
	if got[used] != 1.75 || got[idle] != 0 {
		t.Errorf("spend: used %v, idle %v; want 1.75 and 0", got[used], got[idle])
	}
}
