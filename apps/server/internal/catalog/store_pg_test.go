package catalog

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

// The tests in this file need a database, which CI does not have and a
// developer may not have running, so they ask for one by name and skip when
// they are not given it:
//
//	TEST_DATABASE_URL=postgres://... go test ./internal/catalog/
//
// They run in a database of their own (<name in the URL>_catalog), because they
// empty the catalogue tables.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx := context.Background()
	pool := dbtest.Pool(t, "catalog")
	for _, table := range []string{"catalog_models", "catalog_providers", "channel_models", "channels"} {
		if _, err := pool.Exec(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	// The refresh's bookkeeping too: a server ever started against this
	// database has written it, and the state test starts from nothing.
	if _, err := pool.Exec(ctx, "DELETE FROM app_settings WHERE key = $1", stateKey); err != nil {
		t.Fatalf("clear %s: %v", stateKey, err)
	}
	return pool
}

// TestPgStoreReplace is the counterpart to the fake-store tests: it runs the
// statements themselves, where a NOT NULL column or a numeric conversion can
// fail in a way no amount of Go-level mocking would notice.
func TestPgStoreReplace(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	// A channel serving two models, both switched on: one the incoming snapshot
	// still lists, one it does not. A catalogue sync has no business with either
	// — which models a channel offers is the channel's answer, not the
	// catalogue's, and the sync cannot even reach this table.
	var channelID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO channels (name, type) VALUES ('seed', 'openai') RETURNING id`,
	).Scan(&channelID); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	for _, model := range []string{"gpt-4o", "model-gone"} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO channel_models (channel_id, model_id, enabled) VALUES ($1, $2, true)`,
			channelID, model); err != nil {
			t.Fatalf("seed channel_models: %v", err)
		}
	}

	client := serveFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture(t))
	})
	result, err := client.Fetch(ctx, "")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	store := NewStore(pool)
	if err := store.Replace(ctx, result.Providers); err != nil {
		t.Fatalf("replace: %v", err)
	}
	// Twice, because the second one is the case the delete-then-insert strategy
	// has to survive: every row already exists.
	if err := store.Replace(ctx, result.Providers); err != nil {
		t.Fatalf("second replace: %v", err)
	}

	providers, err := q.CountCatalogProviders(ctx)
	if err != nil {
		t.Fatalf("count providers: %v", err)
	}
	models, err := q.CountCatalogModels(ctx)
	if err != nil {
		t.Fatalf("count models: %v", err)
	}
	if providers != 3 || models != 4 {
		t.Errorf("snapshot = %d providers, %d models; want 3 and 4", providers, models)
	}

	var settings int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM channel_models WHERE enabled").Scan(&settings); err != nil {
		t.Fatalf("count enabled: %v", err)
	}
	if settings != 2 {
		t.Errorf("enabled models = %d, want the two the sync was told nothing about", settings)
	}

	// The costs are stored as numeric and read back as a float, which is the
	// round trip the cost columns exist for.
	model, err := q.GetCatalogModel(ctx, sqlc.GetCatalogModelParams{ProviderID: "openai", ModelID: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("get model: %v", err)
	}
	if model.InputCost != 0.15 || model.OutputCost != 0.6 {
		t.Errorf("cost = %v/%v, want 0.15/0.6", model.InputCost, model.OutputCost)
	}
	if model.ProviderName != "OpenAI" || model.Enabled {
		t.Errorf("model = %+v, want it named by its provider and switched off", model)
	}
	// The one the operator had switched on before the sync is still on.
	enabled, err := q.GetCatalogModel(ctx, sqlc.GetCatalogModelParams{ProviderID: "openai", ModelID: "gpt-4o"})
	if err != nil {
		t.Fatalf("get model: %v", err)
	}
	if !enabled.Enabled {
		t.Error("a refresh switched off a model the operator had switched on")
	}
}

// TestPgListServedModelsFilters runs the one paging query the marketplace has.
//
// It is the filters and the total that matter here: the page and the count are
// two separate statements over the same WHERE clause, and if they ever disagree
// the grid pages onto a number the list cannot fill.
func TestPgListServedModelsFilters(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	client := serveFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture(t))
	})
	result, err := client.Fetch(ctx, "")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := NewStore(pool).Replace(ctx, result.Providers); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// A catalogue on its own must never populate the marketplace.
	if total, err := q.CountServedModels(ctx, sqlc.CountServedModelsParams{}); err != nil || total != 0 {
		t.Fatalf("catalogue-only total = %d, err = %v", total, err)
	}

	// Selected models in an enabled and a disabled channel. An unselected
	// discovery must stay out of the marketplace, even without an enabled filter.
	var channelID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO channels (name, type) VALUES ('filters', 'openai') RETURNING id`,
	).Scan(&channelID); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	for _, model := range []struct {
		id      string
		enabled bool
	}{
		{"gpt-4o", true},
		{"claude-sonnet-4-5", true},
		{"bare", true},
		{"private/custom", true},
		{"unselected-discovery", false},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO channel_models (channel_id, model_id, enabled) VALUES ($1, $2, $3)`,
			channelID, model.id, model.enabled); err != nil {
			t.Fatalf("seed %s: %v", model.id, err)
		}
	}
	var disabledChannelID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO channels (name, type, enabled) VALUES ('disabled', 'openai', false) RETURNING id`,
	).Scan(&disabledChannelID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channel_models (channel_id, model_id, enabled)
		VALUES ($1, 'gpt-4o-mini', true), ($1, 'gpt-4o', true), ($1, 'private/custom', false)`, disabledChannelID); err != nil {
		t.Fatal(err)
	}

	provider := "openai"
	search := "MINI"
	enabled := true
	disabled := false

	cases := []struct {
		name   string
		params sqlc.ListServedModelsParams
		want   int
	}{
		{"unfiltered", sqlc.ListServedModelsParams{}, 5},
		{"by provider", sqlc.ListServedModelsParams{ProviderID: &provider}, 2},
		{"search is case insensitive", sqlc.ListServedModelsParams{Search: &search}, 1},
		{"capabilities are all-of", sqlc.ListServedModelsParams{
			RequireReasoning: true, RequireToolCall: true,
		}, 1},
		{"enabled", sqlc.ListServedModelsParams{Enabled: &enabled}, 4},
		{"disabled channels", sqlc.ListServedModelsParams{Enabled: &disabled}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := tc.params
			params.RowLimit = 50

			rows, err := q.ListServedModels(ctx, params)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(rows) != tc.want {
				t.Errorf("rows = %d, want %d", len(rows), tc.want)
			}

			total, err := q.CountServedModels(ctx, sqlc.CountServedModelsParams{
				Search:                  params.Search,
				ProviderID:              params.ProviderID,
				RequireReasoning:        params.RequireReasoning,
				RequireToolCall:         params.RequireToolCall,
				RequireAttachment:       params.RequireAttachment,
				RequireStructuredOutput: params.RequireStructuredOutput,
				Enabled:                 params.Enabled,
			})
			if err != nil {
				t.Fatalf("count: %v", err)
			}
			// The count has to agree with the page, or paging lands on a page
			// number the list cannot fill.
			if int(total) != tc.want {
				t.Errorf("total = %d, want %d", total, tc.want)
			}
		})
	}
	// The channel badges use the same membership rule as the page itself.
	channels, err := q.ListChannelsServingModels(ctx, []string{"private/custom"})
	if err != nil || len(channels) != 1 || channels[0].ID.String() != channelID {
		t.Fatalf("selected channel badges = %+v, err = %v", channels, err)
	}
}

// The bookkeeping shares app_settings with the rest of the installation, so it
// has to survive the round trip through jsonb.
func TestPgStoreState(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	state, err := store.LoadState(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !state.Stale(state.FetchedAt.Add(StaleAfter)) || !state.FetchedAt.IsZero() {
		t.Fatalf("state = %+v, want the zero state before anything is written", state)
	}

	written := State{FetchedAt: state.FetchedAt.UTC(), ETag: `"v1"`, LastError: "upstream said no"}
	if err := store.SaveState(ctx, written); err != nil {
		t.Fatalf("save: %v", err)
	}
	read, err := store.LoadState(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if read.ETag != written.ETag || read.LastError != written.LastError {
		t.Errorf("state = %+v, want %+v", read, written)
	}
}
