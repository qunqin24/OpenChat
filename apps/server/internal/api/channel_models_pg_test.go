package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/db"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/providers/modellist"
)

// The test in this file needs a database, which CI does not have and a
// developer may not have running, so it asks for one by name and skips when it
// is not given it:
//
//	CHANNEL_TEST_DATABASE_URL=postgres://... go test ./internal/api/
//
// Point it at a scratch database: this test empties the channel tables. Its
// offline counterparts in channel_models_test.go check the same rules by
// reading the SQL; this one checks that Postgres agrees, which is where a
// clause that parses but does not mean what it says gets caught.
func channelTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("CHANNEL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CHANNEL_TEST_DATABASE_URL to run the database-backed channel tests")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// channel_models goes with its channel, so one delete clears both.
	if _, err := pool.Exec(ctx, "DELETE FROM channels"); err != nil {
		t.Fatalf("clear channels: %v", err)
	}
	return pool
}

// TestStoreChannelModels is the whole refresh contract in one run: what a
// second fetch adds, removes, and — above all — leaves alone.
func TestStoreChannelModels(t *testing.T) {
	pool := channelTestPool(t)
	ctx := context.Background()
	server := &Server{pool: pool, q: sqlc.New(pool)}

	channel, err := server.q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name:    "scratch",
		Type:    sqlc.ChannelTypeOpenai,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	first, err := server.storeChannelModels(ctx, channel.ID, []modellist.Model{
		{ID: "gpt-4o", DisplayName: ""},
		{ID: "o3-mini", DisplayName: ""},
	})
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if first.Total != 2 || first.Added != 2 || first.Removed != 0 {
		t.Fatalf("first refresh = %+v, want 2/2/0", first)
	}

	// What the operator does between refreshes: switches one model on, and
	// types in one the endpoint does not publish.
	if _, err := server.q.SetChannelModelEnabled(ctx, sqlc.SetChannelModelEnabledParams{
		ChannelID: channel.ID, ModelID: "gpt-4o", Enabled: true,
	}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := server.q.AddManualChannelModel(ctx, sqlc.AddManualChannelModelParams{
		ChannelID: channel.ID, ModelID: "gpt-4o-secret", DisplayName: "The one they forgot",
	}); err != nil {
		t.Fatalf("add manual: %v", err)
	}

	// The second answer renames one model, drops another, adds a third, and
	// happens to mention the manual one.
	second, err := server.storeChannelModels(ctx, channel.ID, []modellist.Model{
		{ID: "gpt-4o", DisplayName: "GPT-4o"},
		{ID: "gpt-5", DisplayName: "GPT-5"},
		{ID: "gpt-4o-secret", DisplayName: "Not So Secret"},
	})
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	// Added counts rows that were not there: gpt-5 only, since the manual row
	// already existed.
	if second.Total != 3 || second.Added != 1 || second.Removed != 1 {
		t.Fatalf("second refresh = %+v, want 3/1/1", second)
	}

	rows, err := server.q.ListChannelModels(ctx, channel.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	stored := make(map[string]sqlc.ChannelModel, len(rows))
	for _, row := range rows {
		stored[row.ModelID] = row
	}
	if len(stored) != 3 {
		t.Fatalf("stored %d models, want 3: %v", len(stored), stored)
	}

	// The one the operator switched on is still on, and now carries the name
	// the endpoint supplied.
	if kept := stored["gpt-4o"]; !kept.Enabled || kept.DisplayName != "GPT-4o" || !kept.FetchedAt.Valid {
		t.Errorf("gpt-4o = %+v, want enabled, renamed, and stamped", kept)
	}
	// A model the endpoint stopped listing is gone.
	if _, ok := stored["o3-mini"]; ok {
		t.Error("o3-mini survived a refresh that no longer lists it")
	}
	// A new model arrives switched off: a refresh never turns anything on.
	if fresh := stored["gpt-5"]; fresh.Enabled || fresh.Source != "fetched" {
		t.Errorf("gpt-5 = %+v, want a disabled fetched row", fresh)
	}
	// The manual row is untouched, even though the endpoint listed its id: same
	// source, same name the operator gave it, still enabled, never stamped.
	manual := stored["gpt-4o-secret"]
	if manual.Source != "manual" || manual.DisplayName != "The one they forgot" ||
		!manual.Enabled || manual.FetchedAt.Valid {
		t.Errorf("manual row = %+v, want it exactly as the operator left it", manual)
	}

	// An endpoint that lists nothing but the manual model prunes every fetched
	// row and still leaves the manual one standing.
	third, err := server.storeChannelModels(ctx, channel.ID, []modellist.Model{
		{ID: "gpt-4o-secret", DisplayName: "Not So Secret"},
	})
	if err != nil {
		t.Fatalf("third refresh: %v", err)
	}
	if third.Removed != 2 {
		t.Errorf("third refresh removed %d, want the 2 fetched rows", third.Removed)
	}

	count, err := server.q.CountEnabledChannelModelsFor(ctx, channel.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("enabled count = %d, want the manual row alone", count)
	}
}

func TestCreateChannelWithSelectedModels(t *testing.T) {
	pool := channelTestPool(t)
	ctx := context.Background()
	server := &Server{pool: pool, q: sqlc.New(pool)}
	body := `{"name":"selected models","type":"openai","apiKey":"write-only-key","models":[
		{"modelId":"chosen","displayName":"Chosen model","source":"fetched"},
		{"modelId":"private/model","displayName":"","source":"manual"}
	]}`
	response := httptest.NewRecorder()
	server.CreateChannel(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "write-only-key") {
		t.Fatal("create returned a credential")
	}
	var channel Channel
	if err := json.Unmarshal(response.Body.Bytes(), &channel); err != nil {
		t.Fatal(err)
	}
	if channel.ModelCount != 2 {
		t.Fatalf("modelCount = %d", channel.ModelCount)
	}
	rows, err := server.q.ListChannelModels(ctx, channel.Id)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}
	for _, row := range rows {
		if !row.Enabled || (row.Source == "fetched") != row.FetchedAt.Valid {
			t.Errorf("incorrect selected model state: %+v", row)
		}
	}

	// A failure after the channel INSERT must roll that INSERT back as well.
	_, err = pool.Exec(ctx, `ALTER TABLE channel_models ADD CONSTRAINT reject_test_model CHECK (model_id <> 'reject-for-test')`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `ALTER TABLE channel_models DROP CONSTRAINT reject_test_model`)
	})
	body = `{"name":"must roll back","type":"openai","models":[
		{"modelId":"reject-for-test","displayName":"","source":"manual"}
	]}`
	response = httptest.NewRecorder()
	server.CreateChannel(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("failed insert status = %d", response.Code)
	}
	channels, err := server.q.ListChannels(ctx)
	if err != nil || len(channels) != 1 || channels[0].ID != channel.Id {
		t.Fatalf("failed create left a channel: %+v, err = %v", channels, err)
	}
}

// An endpoint answering with an empty list must not be allowed to prune what is
// stored. The guard sits upstream of the transaction, so nothing is read or
// written; running it against Postgres proves the rows survive the failure.
func TestRefreshWithEmptyUpstreamLeavesModelsAlone(t *testing.T) {
	pool := channelTestPool(t)
	ctx := context.Background()
	server := &Server{pool: pool, q: sqlc.New(pool)}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	channel, err := server.q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "empty answer", Type: sqlc.ChannelTypeOpenai,
		BaseUrl: upstream.URL + "/v1", ApiKey: "stored-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := server.q.AddManualChannelModel(ctx, sqlc.AddManualChannelModelParams{
		ChannelID: channel.ID, ModelID: "manual-kept",
	}); err != nil {
		t.Fatalf("seed manual: %v", err)
	}
	if _, err := server.storeChannelModels(ctx, channel.ID, []modellist.Model{{ID: "fetched-kept"}}); err != nil {
		t.Fatalf("seed fetched: %v", err)
	}

	response := httptest.NewRecorder()
	server.RefreshChannelModels(response, httptest.NewRequest(http.MethodPost, "/", nil), channel.ID)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "channel_listed_nothing") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	rows, err := server.q.ListChannelModels(ctx, channel.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v, err = %v; want both left untouched", rows, err)
	}
}
