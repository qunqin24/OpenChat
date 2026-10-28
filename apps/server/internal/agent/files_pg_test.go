package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// memBlobs is a file store in memory.
type memBlobs map[string][]byte

func (m memBlobs) Read(_ context.Context, key string, limit int64) ([]byte, error) {
	data, ok := m[key]
	if !ok || int64(len(data)) > limit {
		return nil, errors.New("cannot read")
	}
	return data, nil
}

// attachText writes a user message carrying a text file, with the receipt
// moderation would have left (or a wrong one), as the handlers do.
func (f *fixture) attachText(blobs memBlobs, name, text string, status sqlc.MessageStatus, receipt string) sqlc.Message {
	f.t.Helper()
	ctx := context.Background()
	key := "uploads/" + uuid.NewString()
	blobs[key] = []byte(text)
	file, err := f.q.CreateAttachment(ctx, sqlc.CreateAttachmentParams{
		UserID: &f.user, StorageKey: key, Filename: name, ContentType: llm.TextMediaType, SizeBytes: int64(len(text)),
	})
	if err != nil {
		f.t.Fatal(err)
	}
	if receipt == "" {
		sum := sha256.Sum256([]byte(text))
		receipt = hex.EncodeToString(sum[:])
	}
	parts := []llm.Part{llm.TextPart{Text: "here is " + name}, llm.AttachmentPart{
		Ref: file.ID.String(), Filename: name, MediaType: llm.TextMediaType, SHA256: receipt, Size: int64(len(text)),
	}}
	msg, err := f.q.AppendEntry(ctx, sqlc.AppendEntryParams{
		ConversationID: f.conv, Role: sqlc.MessageRoleUser, Content: "here is " + name, Parts: EncodeParts(parts), Status: status,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE attachments SET message_id = $1 WHERE id = $2`, msg.ID, file.ID); err != nil {
		f.t.Fatal(err)
	}
	return msg
}

// A long file is a card in the request, the tools are offered with it, and a
// search and a read find what the card leaves out.
func TestLongFileIsReadWithTheTools(t *testing.T) {
	blobs := memBlobs{}
	f := newFixtureWith(t, func(o *Options) { o.Blobs = blobs }, nil,
		toolCall(t, "c1", ToolFileSearch, `{"query":"REVENUE grew"}`),
		toolCall(t, "c2", ToolFileRead, `{"file":"report.md","start_line":775,"end_line":779}`),
		answer(t, "done"))
	f.attachText(blobs, "report.md", longMarkdown(), sqlc.MessageStatusComplete, "")
	run := f.admit("what does it say about revenue?")
	f.runner.Start(run.ID)
	f.waitFor(run.ID, sqlc.RunStatusCompleted)

	first := f.models.reqs[0]
	offered := map[string]bool{}
	for _, tool := range first.Tools {
		offered[tool.Name] = true
	}
	if !offered[ToolFileRead] || !offered[ToolFileSearch] {
		t.Fatalf("tools offered: %v", offered)
	}
	var sent strings.Builder
	for _, msg := range first.Messages {
		for _, part := range msg.Parts {
			if text, ok := part.(llm.TextPart); ok {
				sent.WriteString(text.Text)
			}
		}
	}
	if !strings.Contains(sent.String(), `partial="true"`) || strings.Contains(sent.String(), "The revenue grew") {
		t.Fatal("the long file was not sent as a card")
	}

	results := map[string]string{}
	for _, msg := range f.models.lastReq.Messages {
		for _, part := range msg.Parts {
			if result, ok := part.(llm.ToolResultPart); ok {
				results[result.CallID] = result.Content
			}
		}
	}
	if got := results["c1"]; !strings.Contains(got, "report.md (id ") || !strings.Contains(got, "777:The revenue grew") {
		t.Errorf("search: %s", got)
	}
	if got := results["c2"]; !strings.Contains(got, "   777\tThe revenue grew") || !strings.Contains(got, "lines 775-779 of 1200") {
		t.Errorf("read: %s", got)
	}
}

// What the tools read is the person's own, unrefused, unchanged files.
func TestFileToolsReadOnlyCheckedFiles(t *testing.T) {
	blobs := memBlobs{}
	f := newFixtureWith(t, func(o *Options) { o.Blobs = blobs }, nil)
	ctx := context.Background()
	scope := Scope{UserID: f.user, ConversationID: f.conv, State: &RunState{}}
	read := func(args string) (ToolResult, error) {
		return fileTool{r: f.runner}.Execute(WithScope(ctx, scope), []byte(args))
	}

	// A refused message's file is not there, and offers no tools.
	f.attachText(blobs, "refused.md", longMarkdown(), sqlc.MessageStatusBlocked, "")
	if long, err := f.runner.hasLongFiles(ctx, f.conv); err != nil || long {
		t.Fatalf("a refused file offers the tools: %v %v", long, err)
	}
	if _, err := read(`{"file":"refused.md"}`); err == nil {
		t.Error("a refused message's file was read")
	}

	// A file written again after it was checked is not read.
	f.attachText(blobs, "changed.md", longMarkdown(), sqlc.MessageStatusComplete, strings.Repeat("0", 64))
	if long, _ := f.runner.hasLongFiles(ctx, f.conv); !long {
		t.Error("a long file offers no tools")
	}
	if _, err := read(`{"file":"changed.md"}`); err == nil || !strings.Contains(err.Error(), "changed after it was checked") {
		t.Errorf("a changed file: %v", err)
	}

	// Someone else's run does not see the files.
	scope.UserID = uuid.New()
	if _, err := read(`{"file":"changed.md"}`); err == nil {
		t.Error("another person's run read the file")
	}
}
