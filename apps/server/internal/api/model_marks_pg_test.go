package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// A model nothing serves any more keeps its name and brand: from the
// catalogue, preferring the brand a channel that still lists it names, else
// the name an operator gave it on a channel; one known to neither is left out.
func TestListModelMarks(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	server := &Server{pool: pool, q: q}

	tag := uuid.NewString()[:8]
	catalogued, named, unknown := "gone-"+tag, "renamed-"+tag, "never-"+tag
	for _, p := range []string{"aaa-" + tag, "openai"} {
		if _, err := pool.Exec(ctx, `INSERT INTO catalog_providers (id, name) VALUES ($1, $1) ON CONFLICT DO NOTHING`, p); err != nil {
			t.Fatal(err)
		}
	}
	// Two providers list it; a channel of type openai still lists it, switched
	// off, so openai's row wins over the one first by id.
	for _, row := range [][3]string{{"aaa-" + tag, "Reseller Name", "reseller"}, {"openai", "Gone Model", "gpt"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO catalog_models (provider_id, model_id, name, family, raw) VALUES ($1, $2, $3, $4, '{}')`,
			row[0], catalogued, row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "marks " + tag, Type: sqlc.ChannelTypeOpenai, Enabled: false,
		BaseUrl: "http://127.0.0.1:1/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{catalogued, named}, DisplayNames: []string{"", "Operator Name"}, Sources: []string{"manual", "manual"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM catalog_models WHERE model_id = $1", catalogued)
		_, _ = pool.Exec(context.Background(), "DELETE FROM catalog_providers WHERE id = $1", "aaa-"+tag)
	})

	rec := httptest.NewRecorder()
	server.ListModelMarks(rec, httptest.NewRequest(http.MethodGet, "/", nil), ListModelMarksParams{Ids: []string{catalogued, named, unknown, catalogued}})
	var marks []ModelMark
	if err := json.Unmarshal(rec.Body.Bytes(), &marks); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	got := map[string]ModelMark{}
	for _, m := range marks {
		got[m.Id] = m
	}
	if len(marks) != 2 {
		t.Fatalf("marks = %+v, want the two known models once each", marks)
	}
	if m := got[catalogued]; m.Name != "Gone Model" || m.ProviderId != "openai" || m.Family != "gpt" {
		t.Errorf("catalogued = %+v", m)
	}
	if m := got[named]; m.Name != "Operator Name" || m.ProviderId != "" {
		t.Errorf("named = %+v", m)
	}

	rec = httptest.NewRecorder()
	server.ListModelMarks(rec, httptest.NewRequest(http.MethodGet, "/", nil), ListModelMarksParams{Ids: make([]string, maxModelMarks+1)})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("too many ids: %d", rec.Code)
	}
}
