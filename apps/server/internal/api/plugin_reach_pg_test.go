package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// newFakeCamera is an MCP service that answers with a picture, and has a
// tool that changes things.
func newFakeCamera(t *testing.T) string {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var shot bytes.Buffer
	_ = png.Encode(&shot, img)
	server := mcp.NewServer(&mcp.Implementation{Name: "camera", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "snapshot", InputSchema: map[string]any{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.TextContent{Text: "a red dot"},
				&mcp.ImageContent{MIMEType: "image/png", Data: shot.Bytes()},
				&mcp.ImageContent{MIMEType: "image/png", Data: []byte("not a picture")},
			}}, nil
		})
	server.AddTool(&mcp.Tool{Name: "erase", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "erased"}}}, nil
		})
	srv := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestPluginReach(t *testing.T) {
	pool := agentTestPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	s := &Server{pool: pool, q: q, cfg: &config.Config{}}
	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	slug := "camera-" + tag

	var user sqlc.User
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, 'admin') RETURNING id, email, name, role`,
		tag+"@test.invalid", "reach-"+tag).Scan(&user.ID, &user.Email, &user.Name, &user.Role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	var pluginID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO plugins (slug, name, mcp_url) VALUES ($1, 'Camera', $2) RETURNING id`, slug, newFakeCamera(t)).Scan(&pluginID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM plugins WHERE id = $1", pluginID) })
	asUser := auth.WithUser(ctx, user)
	rec := httptest.NewRecorder()
	s.RefreshAdminPluginTools(rec, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(asUser), pluginID)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools: %d %s", rec.Code, rec.Body)
	}
	if err := q.InstallPlugin(ctx, sqlc.InstallPluginParams{UserID: user.ID, PluginID: pluginID}); err != nil {
		t.Fatal(err)
	}
	newRun := func(subagentPlugins []string) agent.Scope {
		t.Helper()
		var conv, msg, run uuid.UUID
		var parent *uuid.UUID
		if subagentPlugins != nil {
			var p uuid.UUID
			if err := pool.QueryRow(ctx, `INSERT INTO conversations (user_id) VALUES ($1) RETURNING id`, user.ID).Scan(&p); err != nil {
				t.Fatal(err)
			}
			parent = &p
			if err := pool.QueryRow(ctx, `INSERT INTO conversations (user_id, kind, parent_id, plugins) VALUES ($1, 'subagent', $2, $3) RETURNING id`,
				user.ID, p, subagentPlugins).Scan(&conv); err != nil {
				t.Fatal(err)
			}
		} else if err := pool.QueryRow(ctx, `INSERT INTO conversations (user_id) VALUES ($1) RETURNING id`, user.ID).Scan(&conv); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO messages (conversation_id, role, content) VALUES ($1, 'user', 'hi') RETURNING id`, conv).Scan(&msg); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO runs (conversation_id, user_id, input_message_id, model_id) VALUES ($1, $2, $3, 'm') RETURNING id`,
			conv, user.ID, msg).Scan(&run); err != nil {
			t.Fatal(err)
		}
		return agent.Scope{RunID: run, UserID: user.ID, ConversationID: conv, Parent: parent}
	}
	script := func(scope agent.Scope, code string) agent.ToolResult {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"code": code})
		out, err := pluginCodeTool{s: s}.Execute(agent.WithToolCall(agent.WithScope(asUser, scope), "call-1"), raw)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	ns := fmt.Sprintf("tools[%q]", slug)

	// A picture comes back stored, for the page and for the model; one that
	// is not a picture is left out.
	scope := newRun(nil)
	out := script(scope, fmt.Sprintf(`return await %s.snapshot({})`, ns))
	if len(out.Images) != 1 || out.Images[0].MediaType != "image/png" || !strings.Contains(out.Content, "returned 1 image(s)") || !strings.Contains(out.Content, "1 more image(s) were left out") {
		t.Fatalf("pictures: %+v", out)
	}
	details := out.Details.(PluginCodeProgress)
	if details.Images == nil || len(*details.Images) != 1 {
		t.Fatalf("details: %+v", details)
	}
	imageID := (*details.Images)[0].Id
	rec = httptest.NewRecorder()
	s.GetToolImage(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(asUser), scope.ConversationID, imageID)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("image: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	// The model sees it after the round's result.
	built, err := agent.BuildContext([]sqlc.Message{
		{Role: sqlc.MessageRoleAssistant, Parts: agent.EncodeParts([]llm.Part{llm.ToolCallPart{ID: "call-1", Name: ToolPluginCode, Arguments: json.RawMessage(`{}`)}})},
		{Role: sqlc.MessageRoleTool, Parts: agent.EncodeParts([]llm.Part{llm.ToolResultPart{CallID: "call-1", Name: ToolPluginCode, Content: "ok", Status: llm.ToolSucceeded, Images: out.Images}})},
	})
	if err != nil || len(built) != 3 || built[2].Role != llm.RoleUser {
		t.Fatalf("context: %v %+v", err, built)
	}
	if ref, ok := built[2].Parts[1].(llm.AttachmentPart); !ok || ref.Ref != llm.ToolImageRef+imageID.String() {
		t.Fatalf("picture part: %+v", built[2].Parts)
	}

	// Nobody watches a scheduled run: a call that needs the person is refused
	// at once.
	scope = newRun(nil)
	scope.Unattended = true
	started := time.Now()
	out = script(scope, fmt.Sprintf(`try { await %s.erase({}) } catch (e) { return e.message }`, ns))
	if !strings.Contains(out.Content, "nobody is here to ask") || time.Since(started) > 5*time.Second {
		t.Fatalf("unattended: %s", out.Content)
	}

	// A sub-agent has the plugins its launch named, and of them only what runs
	// without asking.
	none := newRun([]string{})
	if tools := s.PluginTools(agent.WithScope(asUser, none)); len(tools) != 0 {
		t.Fatalf("a sub-agent with no plugins named was offered %d tools", len(tools))
	}
	named := newRun([]string{slug})
	if tools := s.PluginTools(agent.WithScope(asUser, named)); len(tools) != 1 {
		t.Fatalf("a sub-agent with the plugin named was offered %d tools", len(tools))
	}
	out = script(named, fmt.Sprintf(`const shot = await %s.snapshot({})
try { await %s.erase({}) } catch (e) { return [typeof shot, e.message] }`, ns, ns))
	if !strings.Contains(out.Content, "has no tool") {
		t.Fatalf("sub-agent: %s", out.Content)
	}
	if _, err := s.subagentPluginNames(ctx, agent.Scope{UserID: user.ID}, []string{"nope"}); err == nil || !strings.Contains(err.Error(), slug) {
		t.Fatalf("an unknown plugin: %v", err)
	}

	// A plugin's skill is in the list of its owner's conversations while the
	// plugin is installed.
	body := `{"files":[{"path":"SKILL.md","content":"---\nname: camera-tips-` + tag + `\ndescription: How to frame a shot\n---\nHold still."}]}`
	rec = httptest.NewRecorder()
	s.CreatePluginSkill(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)).WithContext(asUser), pluginID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("plugin skill: %d %s", rec.Code, rec.Body)
	}
	listed := func() bool {
		skills, err := s.notes().Skills(ctx, user.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, skill := range skills {
			if skill.Name == "camera-tips-"+tag {
				return skill.Plugin
			}
		}
		return false
	}
	if !listed() {
		t.Fatal("the plugin's skill is not listed")
	}
	rec = httptest.NewRecorder()
	s.ListSiteSkills(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(asUser))
	if strings.Contains(rec.Body.String(), "camera-tips-"+tag) {
		t.Fatal("a plugin's skill is listed as the site's")
	}
	if _, err := q.UninstallPlugin(ctx, sqlc.UninstallPluginParams{UserID: user.ID, PluginID: pluginID}); err != nil {
		t.Fatal(err)
	}
	if listed() {
		t.Fatal("the skill of a plugin not installed is listed")
	}

	// The administrators' record: newest first, the refused ones on their own.
	rec = httptest.NewRecorder()
	failed := true
	s.ListPluginCalls(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(asUser), ListPluginCallsParams{PluginId: &pluginID, Failed: &failed})
	var page PluginCallLogPage
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	// The sub-agent's erase never reached the host: it has no such tool.
	if rec.Code != http.StatusOK || len(page.Items) != 1 || page.Items[0].Ok || page.Items[0].Outcome != "unattended" {
		t.Fatalf("calls: %d %s", rec.Code, rec.Body)
	}
}
