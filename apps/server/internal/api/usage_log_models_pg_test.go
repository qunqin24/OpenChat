package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// The log page's model filter offers what the log has requests for, whatever
// kind they were - an image model is not among the chat models on offer, and
// a model no channel serves any more is not among any - once each, in order.
func TestUsageLogModelsListWhatTheLogHas(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	server := &Server{pool: pool, q: sqlc.New(pool)}

	tag := uuid.NewString()[:8]
	image, gone, chat := "zz-image-"+tag, "zz-gone-"+tag, "zz-chat-"+tag
	for _, row := range []struct{ kind, model string }{
		{"image", image}, {"chat", chat}, {"chat", gone}, {"chat", chat}, {"title", chat},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO usage_logs (kind, status, channel_name, model_id, served_model)
			VALUES ($1, 'ok', 'c', $2, $2)`, row.kind, row.model); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM usage_logs WHERE model_id = ANY($1)", []string{image, gone, chat})
	})

	rec := httptest.NewRecorder()
	server.ListUsageLogModels(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got UsageLogModels
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !slices.IsSorted(got.Items) {
		t.Errorf("not in order: %v", got.Items)
	}
	var ours []string
	for _, m := range got.Items {
		if m == image || m == gone || m == chat {
			ours = append(ours, m)
		}
	}
	if want := []string{chat, gone, image}; !slices.Equal(ours, want) {
		t.Errorf("listed %v, want %v once each", ours, want)
	}
}
