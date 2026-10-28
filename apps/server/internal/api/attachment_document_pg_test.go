package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/storage"
)

func docxOf(t *testing.T, paragraphs ...string) []byte {
	t.Helper()
	var body strings.Builder
	for _, p := range paragraphs {
		body.WriteString(`<w:p><w:r><w:t>` + p + `</w:t></w:r></w:p>`)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("word/document.xml")
	_, _ = f.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		body.String() + `</w:body></w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A Word file is kept as it was uploaded, and is its words to moderation and
// to every model; a damaged one is refused before anything is written.
func TestWordFileIsCheckedAndSentAsWords(t *testing.T) {
	h := newSwapHarness(t, func(t *testing.T) (swapBackend, *storage.Local) {
		return &swapBucket{memoryStore: &memoryStore{blobs: map[string][]byte{}}}, nil
	})
	bucket := h.backend.(*swapBucket)
	upload := func(name string, data []byte) Upload {
		t.Helper()
		rec := h.call(h.server.CreateUpload, fmt.Sprintf(`{"filename":%q,"contentType":%q,"size":%d}`, name, llm.DocxMediaType, len(data)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("issue: %d %s", rec.Code, rec.Body)
		}
		var up Upload
		if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
			t.Fatal(err)
		}
		bucket.put(h.keyOf(up.Id), data)
		return up
	}

	report := upload("report.docx", docxOf(t, "季度总结", "营收增长百分之十二。"))
	request, status := h.send(report.Id)
	if status != sqlc.RunStatusCompleted {
		t.Fatalf("run %s", status)
	}
	if !strings.Contains(request, `file name=\"report.docx\"\u003e\n季度总结\n\n营收增长百分之十二。\n`) {
		t.Errorf("the model was not sent the document's words: %s", request)
	}
	if got := h.mod.texts[len(h.mod.texts)-1]; !strings.Contains(strings.Join(got, " "), "营收增长") {
		t.Errorf("moderation was sent %q, not the document's words", got)
	}
	var parts []byte
	if err := h.pool.QueryRow(t.Context(), `SELECT parts FROM messages WHERE conversation_id = $1 AND role = 'user'
		ORDER BY created_at DESC LIMIT 1`, h.conv.ID).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(parts), `"textSize":`) || !strings.Contains(string(parts), `"sha256":`) {
		t.Errorf("the message does not record the document's words: %s", parts)
	}

	broken := upload("broken.docx", []byte("not a zip at all"))
	rec := h.call(func(w http.ResponseWriter, r *http.Request) { h.server.CreateMessage(w, r, h.conv.ID) },
		fmt.Sprintf(`{"content":"read this","model":%q,"attachmentIds":["%s"]}`, h.model, broken.Id))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "attachment_unreadable") {
		t.Fatalf("a damaged document: %d %s", rec.Code, rec.Body)
	}
}
