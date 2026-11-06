package api

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// A model that reads PDFs is sent them whole only where every channel that may
// answer it can carry them: Command Code takes text and pictures only, so with
// one of its channels serving the model a PDF goes as its words.
func TestPDFsGoAsWordsWhereAChannelTakesNoFiles(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	modelID := "claude-pdf-" + tag
	if _, err := q.SaveModelOverride(ctx, sqlc.SaveModelOverrideParams{ModelID: modelID, InputModalities: []string{"image", "pdf"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM model_overrides WHERE model_id = $1", modelID) })
	channel := func(kind sqlc.ChannelType) uuid.UUID {
		t.Helper()
		c, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{Name: string(kind) + " " + tag, Type: kind, Enabled: true, ApiKey: "sk-test"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", c.ID) })
		if err := q.CreateSelectedChannelModels(ctx, sqlc.CreateSelectedChannelModelsParams{
			ChannelID: c.ID, ModelIds: []string{modelID}, DisplayNames: []string{""}, Sources: []string{"manual"},
		}); err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	modalities := NewModalities(pool)
	reads := func() bool {
		t.Helper()
		got, err := modalities(ctx, modelID)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(got, "image") {
			t.Fatalf("pictures were dropped too: %v", got)
		}
		return slices.Contains(got, llm.ModalityPDF)
	}

	channel(sqlc.ChannelTypeAnthropic)
	if !reads() {
		t.Fatal("a PDF was not sent whole through Anthropic alone")
	}
	command := channel(sqlc.ChannelTypeCommandCode)
	if reads() {
		t.Fatal("a PDF would go whole to a model Command Code may answer")
	}
	if _, err := pool.Exec(ctx, `UPDATE channels SET enabled = false WHERE id = $1`, command); err != nil {
		t.Fatal(err)
	}
	if !reads() {
		t.Fatal("a switched-off Command Code channel still kept PDFs from the model")
	}
}
