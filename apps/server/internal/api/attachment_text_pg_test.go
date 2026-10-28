package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/storage"
)

// A text file is the person's words: moderation reads them with the message,
// any model is sent them as text, and only the bytes that were checked.
func TestTextFileIsCheckedAndSentAsWords(t *testing.T) {
	h := newSwapHarness(t, func(t *testing.T) (swapBackend, *storage.Local) {
		return &swapBucket{memoryStore: &memoryStore{blobs: map[string][]byte{}}}, nil
	})
	bucket := h.backend.(*swapBucket)
	upload := func(name, text string) Upload {
		t.Helper()
		rec := h.call(h.server.CreateUpload, fmt.Sprintf(`{"filename":%q,"contentType":"text/plain","size":%d}`, name, len(text)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("issue: %d %s", rec.Code, rec.Body)
		}
		var up Upload
		if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
			t.Fatal(err)
		}
		bucket.put(h.keyOf(up.Id), []byte(text))
		return up
	}

	// Sent as words, to a model that takes only images.
	notes := upload("notes.md", "# 会议记录\n\n周一 10 点。\n")
	request, status := h.send(notes.Id)
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("run %s", status)
	}
	var sent struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	want := "what is this?\n\n<file name=\"notes.md\">\n# 会议记录\n\n周一 10 点。\n</file>"
	if err := json.Unmarshal([]byte(request), &sent); err != nil || len(sent.Messages) == 0 ||
		!strings.HasSuffix(sent.Messages[len(sent.Messages)-1].Content, want) {
		t.Errorf("the model was not sent the file's words: %s", request)
	}
	if got := h.mod.texts[len(h.mod.texts)-1]; !strings.Contains(strings.Join(got, " "), "周一 10 点") {
		t.Errorf("moderation was sent %q, not the file's words", got)
	}

	// Words moderation refuses are refused in a file as in the message.
	raw, _ := json.Marshal([]string{"sexual", "violence/graphic"})
	if err := h.q.SetSetting(t.Context(), sqlc.SetSettingParams{Key: settingModerationCategories, Value: raw}); err != nil {
		t.Fatal(err)
	}
	bad := upload("bad.txt", "BLOCKME")
	rec := h.call(func(w http.ResponseWriter, r *http.Request) { h.server.CreateMessage(w, r, h.conv.ID) },
		fmt.Sprintf(`{"content":"read this","model":%q,"attachmentIds":["%s"]}`, h.model, bad.Id))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "content_blocked") {
		t.Fatalf("a refused file: %d %s", rec.Code, rec.Body)
	}

	// Revise the refused message (the conversation stops on it) by starting over.
	if _, err := h.pool.Exec(t.Context(), `DELETE FROM messages WHERE conversation_id = $1 AND status = 'blocked'`, h.conv.ID); err != nil {
		t.Fatal(err)
	}
	// Written again after the check read it: not sent.
	swapped := upload("swap.txt", "harmless")
	h.backend.onFirstRead(func(key string) { bucket.put(key, []byte("BLOCKME after the check")) })
	request, status = h.send(swapped.Id)
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("run %s", status)
	}
	if strings.Contains(request, "BLOCKME") || !strings.Contains(request, "changed after it was checked") {
		t.Errorf("a file written again after the check: %s", request)
	}
}
