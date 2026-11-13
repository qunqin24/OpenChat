package api

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// A transcription is billed at the operator's price per minute when they set
// one, and otherwise at the catalogue's (our supplement's `audio_minute`):
// gpt-transcribe is priced out of the box.
func TestAudioPriceFallsBackToTheCatalogue(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	tag := uuid.NewString()[:8]
	provider, model := "zz-audio-"+tag, "zz-transcribe-"+tag
	if _, err := pool.Exec(ctx, `INSERT INTO catalog_providers (id, name) VALUES ($1, $1)`, provider); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO catalog_models (provider_id, model_id, name, raw)
		VALUES ($1, $2, $2, '{"type": "transcription", "cost": {"audio_minute": 0.0045}}')`, provider, model); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_overrides WHERE model_id = $1`, model)
		_, _ = pool.Exec(context.Background(), `DELETE FROM catalog_models WHERE provider_id = $1`, provider)
		_, _ = pool.Exec(context.Background(), `DELETE FROM catalog_providers WHERE id = $1`, provider)
	})

	price := func() *float64 {
		t.Helper()
		row, err := q.UsagePricing(ctx, sqlc.UsagePricingParams{ModelID: model})
		if err != nil {
			t.Fatal(err)
		}
		return numberPtr(row.AudioPrice)
	}
	if got := price(); got == nil || *got != 0.0045 {
		t.Fatalf("catalogue price = %v, want 0.0045", got)
	}
	if !(&Server{pool: pool, q: q}).speechModelPriced(ctx, model) {
		t.Error("a model the catalogue prices by the minute reads as unpriced")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO model_overrides (model_id, audio_price) VALUES ($1, 0.002)`, model); err != nil {
		t.Fatal(err)
	}
	if got := price(); got == nil || *got != 0.002 {
		t.Errorf("with the operator's price = %v, want 0.002", got)
	}
}
