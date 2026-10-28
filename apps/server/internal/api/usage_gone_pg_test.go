package api

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/usage"
)

// A request whose account and channel are deleted while it is out still has
// its row, with the names it began with.
func TestUsageRowOutlivesItsAccountAndChannel(t *testing.T) {
	pool := dbtest.Pool(t, "api_usage_gone")
	ctx := context.Background()
	q := sqlc.New(pool)

	tag := uuid.NewString()
	var userID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		"gone"+tag+"@test.invalid", "gone"+tag).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "gone " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true, BaseUrl: "http://127.0.0.1:1/v1", ApiKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &userID, Title: "c"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM channels WHERE id = $1", channel.ID)
	})

	meter := usage.NewRecorder(q).Begin(usage.Call{
		Kind: usage.KindChat, UserID: userID, ConversationID: &conv.ID, ChannelID: &channel.ID, ModelID: "m-" + tag,
	}, nil)
	if err := meter.Reserve(ctx); err != nil {
		t.Fatal(err)
	}
	// An administrator deletes the account and the channel meanwhile.
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM channels WHERE id = $1", channel.ID); err != nil {
		t.Fatal(err)
	}
	meter.End(ctx, llm.ModelResponse{Model: "m", Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5}}, errors.New("broken"))

	var (
		owner, channelID, convID *uuid.UUID
		deleted                  *string
		channelName              string
	)
	if err := pool.QueryRow(ctx, `SELECT user_id, channel_id, conversation_id, deleted_user, channel_name FROM usage_logs WHERE model_id = $1`, "m-"+tag).
		Scan(&owner, &channelID, &convID, &deleted, &channelName); err != nil {
		t.Fatalf("no row: %v", err)
	}
	if owner != nil || channelID != nil || convID != nil || deleted == nil || *deleted != "gone"+tag || channelName != "gone "+tag {
		t.Errorf("row = user %v channel %v conversation %v deleted %v channel name %q", owner, channelID, convID, deleted, channelName)
	}
}
