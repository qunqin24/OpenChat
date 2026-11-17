package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// The recent tab counts a model's text requests of the last six hours, works
// out its speed from answers long enough to measure, and spreads the
// requests over 24 buckets of 15 minutes; a model on offer with no requests
// is listed after the rest.
func TestModelHealth(t *testing.T) {
	pool := dbtest.Pool(t, "api_model_health")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "DELETE FROM usage_logs"); err != nil {
		t.Fatal(err)
	}
	q := sqlc.New(pool)
	server := &Server{pool: pool, q: q}
	// On offer: "fast", which has requests, and "idle", which has none.
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "health", Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true,
		BaseUrl: "http://127.0.0.1:1/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID) })
	if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
		ChannelID: channel.ID, ModelIds: []string{"fast", "idle"}, DisplayNames: []string{"", ""}, Sources: []string{"manual", "manual"},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	log := func(model, kind, status string, ago time.Duration, streamed bool, firstMs *int, durationMs, output int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO usage_logs (created_at, kind, status, model_id, streamed, first_token_ms, duration_ms, output_tokens, input_tokens, cost)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 10, 0.01)`, now.Add(-ago), kind, status, model, streamed, firstMs, durationMs, output); err != nil {
			t.Fatal(err)
		}
	}
	ms := func(v int) *int { return &v }
	log("fast", "chat", "ok", 10*time.Minute, true, ms(400), 2000, 200)  // 100 tok/s
	log("fast", "subagent", "ok", 3*time.Hour, true, ms(600), 4000, 200) // 50 tok/s
	log("fast", "chat", "error", 20*time.Minute, true, nil, 300, 0)      // counted, not timed
	log("fast", "title", "ok", time.Hour, true, ms(200), 500, 5)         // too short to time
	log("fast", "image", "ok", time.Hour, false, nil, 9000, 0)           // not a text request
	log("fast", "chat", "ok", 7*time.Hour, true, ms(100), 1100, 500)     // before the window
	log("quiet", "title", "ok", 2*time.Hour, false, nil, 800, 12)

	rec := httptest.NewRecorder()
	server.GetModelHealth(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var report ModelHealthReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(report.Buckets) != 24 || report.BucketMinutes != 15 || len(report.Models) != 3 {
		t.Fatalf("report: %d buckets of %d minutes, %d models", len(report.Buckets), report.BucketMinutes, len(report.Models))
	}
	// Those with requests first, busiest first, each once; then the idle one.
	fast, quiet, idle := report.Models[0], report.Models[1], report.Models[2]
	if idle.ModelId != "idle" || idle.Requests != 0 || len(idle.Series) != 24 || idle.SpeedP50 != nil {
		t.Errorf("idle = %+v", idle)
	}
	if fast.ModelId != "fast" || fast.Requests != 4 || fast.Errors != 1 || fast.Name != "fast" {
		t.Errorf("fast = %+v", fast)
	}
	if fast.SpeedP50 == nil || *fast.SpeedP50 != 75 {
		t.Errorf("speed = %v, want the median of 100 and 50", deref(fast.SpeedP50))
	}
	if fast.FirstTokenP50Ms == nil || *fast.FirstTokenP50Ms != 400 {
		t.Errorf("first token = %v", fast.FirstTokenP50Ms)
	}
	var requests, errors int64
	for _, p := range fast.Series {
		requests += p.Requests
		errors += p.Errors
	}
	if len(fast.Series) != 24 || requests != 4 || errors != 1 {
		t.Errorf("series: %d points, %d requests, %d errors", len(fast.Series), requests, errors)
	}
	if quiet.SpeedP50 != nil || quiet.FirstTokenP50Ms != nil || quiet.Requests != 1 {
		t.Errorf("quiet = %+v", quiet)
	}
}

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
