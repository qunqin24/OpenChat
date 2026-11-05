package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/storage"
	"github.com/qunqin24/openchat/apps/server/internal/usage"
)

func TestImageTool(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	store, err := storage.NewLocal(t.TempDir(), []byte("test-secret-test-secret-test-sec"), "http://files.invalid")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{pool: pool, q: q, store: store, usage: usage.NewRecorder(q), endUsers: NewEndUsers(pool)}

	// An OpenAI-shaped image service: a picture for each request, and a note
	// of which endpoint was asked.
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	var generations, edits atomic.Int32
	var asked atomic.Value // the last generation request's body
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations":
			generations.Add(1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			asked.Store(body)
		case "/v1/images/edits":
			edits.Add(1)
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(picture.Bytes())}},
			"usage": map[string]any{"input_tokens": 20, "output_tokens": 1000},
		})
	}))
	t.Cleanup(service.Close)

	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	// A gpt-image id, so the adapter sends quality and sizes as gpt-image takes them.
	modelID := "gpt-image-2.6-test-" + tag
	channel, err := q.CreateChannel(ctx, sqlc.CreateChannelParams{
		Name: "images " + tag, Type: sqlc.ChannelTypeOpenaiCompatible, Enabled: true, BaseUrl: service.URL + "/v1", ApiKey: "sk-test",
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
	kind := "image"
	price, _ := imagePriceParam(ptrFloat(0.04))
	if _, err := q.SaveModelOverride(ctx, sqlc.SaveModelOverrideParams{ModelID: modelID, Kind: &kind, ImagePrice: price}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.DeleteModelOverride(context.Background(), modelID) })
	setImages := func(c imageConfig) {
		t.Helper()
		value, _ := json.Marshal(c)
		if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingImageGeneration, Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	setImages(imageConfig{Enabled: true, Model: modelID, MaxPerCall: 2, MaxPerRun: 8, DefaultQuality: llm.QualityMedium, MaxQuality: llm.QualityHigh,
		DefaultComplexity: llm.ComplexityLow})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM app_settings WHERE key = $1", settingImageGeneration)
	})

	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, 'user') RETURNING id, email, name, role`,
		tag+"@test.invalid", "images-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: "pictures"})
	if err != nil {
		t.Fatal(err)
	}
	question, err := q.AppendEntry(ctx, sqlc.AppendEntryParams{ConversationID: conv.ID, Role: sqlc.MessageRoleUser, Content: "draw a lake",
		Parts: agent.EncodeParts([]llm.Part{llm.TextPart{Text: "draw a lake"}}), Status: sqlc.MessageStatusComplete})
	if err != nil {
		t.Fatal(err)
	}
	var runID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO runs (conversation_id, user_id, input_message_id, model_id) VALUES ($1, $2, $3, 'chat') RETURNING id`,
		conv.ID, user.ID, question.ID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	scope := agent.Scope{RunID: runID, UserID: user.ID, ConversationID: conv.ID, State: &agent.RunState{}}
	runCtx := agent.WithToolCall(agent.WithScope(auth.WithUser(ctx, user), scope), "call-1")

	// Offered to the person's run; never to a sub-agent's.
	if offered := s.ImageTools(runCtx); len(offered) != 1 {
		t.Fatalf("offered %d tools", len(offered))
	}
	parent := conv.ID
	if offered := s.ImageTools(agent.WithScope(ctx, agent.Scope{RunID: runID, UserID: user.ID, ConversationID: conv.ID, Parent: &parent, State: &agent.RunState{}})); len(offered) != 0 {
		t.Fatal("offered to a sub-agent")
	}

	tool := imageTool{s: s}
	call := func(args map[string]any) agent.ToolResult {
		t.Helper()
		raw, _ := json.Marshal(args)
		result, err := tool.Execute(runCtx, raw)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	made := call(map[string]any{"prompt": "a calm mountain lake at dawn", "aspect": "landscape"})
	if made.IsError || len(made.Files) != 1 || generations.Load() != 1 {
		t.Fatalf("result = %+v", made)
	}
	// The site's default quality when the call names none.
	if body := asked.Load().(map[string]any); body["quality"] != "medium" || body["size"] != "1536x1024" || body["complexity"] != "low" {
		t.Fatalf("first request: %v", body)
	}
	details := made.Details.(ImageToolDetails)
	handle := details.Images[0].Handle
	if !strings.Contains(made.Content, handle) || details.Images[0].Width != 4 || details.Images[0].Height != 3 {
		t.Fatalf("content %q, details %+v", made.Content, details)
	}

	// The picture is the person's file: on their quota, unclaimed until the
	// result's row takes it, with its bytes in storage.
	var key string
	var origin *string
	var message *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT storage_key, origin, message_id FROM attachments WHERE id = $1 AND user_id = $2 AND run_id = $3`,
		made.Files[0], user.ID, runID).Scan(&key, &origin, &message); err != nil {
		t.Fatal(err)
	}
	if origin == nil || *origin != "generated" || message != nil || !strings.HasPrefix(key, "uploads/") {
		t.Fatalf("row: origin %v, message %v, key %s", origin, message, key)
	}
	if stored, err := store.Read(ctx, key, 1<<20); err != nil || !bytes.Equal(stored, picture.Bytes()) {
		t.Fatalf("stored: %v", err)
	}
	// Billed by the picture, at the operator's price, though tokens were reported.
	var images int32
	var cost float64
	if err := pool.QueryRow(ctx, `SELECT images, cost::float8 FROM usage_logs WHERE run_id = $1 AND kind = 'image'`, runID).Scan(&images, &cost); err != nil {
		t.Fatal(err)
	}
	if images != 1 || cost < 0.0399 || cost > 0.0401 {
		t.Fatalf("ledger: %d pictures, $%v", images, cost)
	}

	// The result's row claims it; regenerating the answer hands its key back.
	result, err := q.AppendEntry(ctx, sqlc.AppendEntryParams{ConversationID: conv.ID, Role: sqlc.MessageRoleTool, Content: "made",
		Parts:  agent.EncodeParts([]llm.Part{llm.ToolResultPart{CallID: "call-1", Name: toolImageGenerate, Content: made.Content, Status: llm.ToolSucceeded}}),
		Status: sqlc.MessageStatusComplete, RunID: &runID})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.ClaimRunFiles(ctx, sqlc.ClaimRunFilesParams{MessageID: &result.ID, RunID: &runID, Ids: made.Files}); err != nil {
		t.Fatal(err)
	}

	// Editing it reads it back by its handle and asks the edits endpoint.
	edited := call(map[string]any{"prompt": "the same lake at night", "images": []string{handle}})
	if edited.IsError || edits.Load() != 1 {
		t.Fatalf("edit: %+v", edited)
	}
	if unknown, err := tool.Execute(runCtx, json.RawMessage(`{"prompt":"x","images":["img_00000000"]}`)); err == nil {
		t.Fatalf("an unknown picture was accepted: %+v", unknown)
	}

	// A level above the site's highest is made at the highest, and the model
	// is told; an exact size comes to gpt-image's bounds.
	capped := call(map[string]any{"prompt": "a lake, finely detailed", "quality": "max", "size": "1920x1080", "background": "transparent", "complexity": "high"})
	body := asked.Load().(map[string]any)
	if capped.IsError || body["quality"] != "high" || body["size"] != "1920x1088" || body["background"] != "transparent" || body["complexity"] != "high" ||
		!strings.Contains(capped.Content, "up to high") {
		t.Fatalf("capped: %v\n%s", body, capped.Content)
	}

	// Fixed by the administrator: the site's quality, whatever is asked.
	setImages(imageConfig{Enabled: true, Model: modelID, MaxPerCall: 2, MaxPerRun: 8, DefaultQuality: llm.QualityLow, QualityFixed: true})
	fixed := call(map[string]any{"prompt": "a lake", "quality": "max"})
	if body := asked.Load().(map[string]any); fixed.IsError || body["quality"] != "low" {
		t.Fatalf("fixed: %v", body)
	}

	// The day's allowance: four made, an allowance of four, nothing more.
	if err := setUserImageDaily(ctx, q, user.ID, 4); err != nil {
		t.Fatal(err)
	}
	refused := call(map[string]any{"prompt": "one more"})
	if !refused.IsError || !strings.Contains(refused.Content, "allowance") || generations.Load() != 3 {
		t.Fatalf("over the allowance: %+v", refused)
	}
	// More per call than allowed is the model's mistake to correct.
	if _, err := tool.Execute(runCtx, json.RawMessage(`{"prompt":"x","count":3}`)); err == nil {
		t.Fatal("three pictures in a call of at most two")
	}
	var done int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(count), 0) FROM image_requests WHERE user_id = $1 AND status = 'done'`, user.ID).Scan(&done); err != nil || done != 4 {
		t.Fatalf("requests done = %d, %v", done, err)
	}

	keys, err := q.DeleteAnswerTo(ctx, sqlc.DeleteAnswerToParams{ConversationID: conv.ID, InputID: question.ID})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range keys {
		found = found || k == key
	}
	if !found {
		t.Fatalf("regenerating did not hand back %s: %v", key, keys)
	}
}

func ptrFloat(f float64) *float64 { return &f }
