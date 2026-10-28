package agent

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// A 1x1 PNG and the smallest thing that starts like a PDF.
var (
	tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	tinyPDF = []byte("%PDF-1.4\n%%EOF\n")
)

type store struct {
	rows  map[uuid.UUID]sqlc.Attachment
	blobs map[string][]byte
	reads []string
}

func newStore() *store {
	return &store{rows: map[uuid.UUID]sqlc.Attachment{}, blobs: map[string][]byte{}}
}

func (s *store) add(name, contentType string, data []byte) llm.AttachmentPart {
	id := uuid.New()
	key := "k/" + id.String()
	s.rows[id] = sqlc.Attachment{ID: id, StorageKey: key, Filename: name, ContentType: contentType, SizeBytes: int64(len(data))}
	s.blobs[key] = data
	return llm.AttachmentPart{Ref: id.String(), Filename: name, MediaType: contentType}
}

func (s *store) resolver(budget int64, accepts ...string) *attachmentResolver {
	return &attachmentResolver{
		lookup: func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]sqlc.Attachment, error) {
			out := map[uuid.UUID]sqlc.Attachment{}
			for _, id := range ids {
				if row, ok := s.rows[id]; ok {
					out[id] = row
				}
			}
			return out, nil
		},
		read: func(_ context.Context, key string, limit int64) ([]byte, error) {
			s.reads = append(s.reads, key)
			data, ok := s.blobs[key]
			if !ok {
				return nil, errors.New("no such object")
			}
			if int64(len(data)) > limit {
				return nil, errors.New("too large")
			}
			return data, nil
		},
		accepts: func(modality string) bool {
			for _, m := range accepts {
				if m == modality {
					return true
				}
			}
			return false
		},
		budget: budget,
	}
}

func userWith(text string, files ...llm.AttachmentPart) llm.Message {
	parts := []llm.Part{llm.TextPart{Text: text}}
	for _, f := range files {
		parts = append(parts, f)
	}
	return llm.Message{Role: llm.RoleUser, Parts: parts}
}

func attachments(msg llm.Message) []llm.AttachmentPart {
	var out []llm.AttachmentPart
	for _, p := range msg.Parts {
		if a, ok := p.(llm.AttachmentPart); ok {
			out = append(out, a)
		}
	}
	return out
}

func noteText(msg llm.Message) string {
	var b strings.Builder
	for _, p := range msg.Parts {
		if t, ok := p.(llm.TextPart); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

func TestBuildContextKeepsAttachmentReferences(t *testing.T) {
	ref := llm.AttachmentPart{Ref: uuid.NewString(), Filename: "a.png", MediaType: "image/png"}
	got, err := BuildContext([]sqlc.Message{row(sqlc.MessageRoleUser, "", llm.TextPart{Text: "look"}, ref)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(attachments(got[0])) != 1 || attachments(got[0])[0].Ref != ref.Ref {
		t.Fatalf("context = %+v, want the reference kept for the resolver", got)
	}
}

func TestResolveInlinesImagesAndPDFs(t *testing.T) {
	s := newStore()
	img := s.add("a.png", "image/png", tinyPNG)
	doc := s.add("r.pdf", "application/pdf", tinyPDF)
	messages := []llm.Message{userWith("what are these?", img, doc)}

	got, err := s.resolver(1<<20, llm.ModalityImage, llm.ModalityPDF).resolve(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	files := attachments(got[0])
	if len(files) != 2 {
		t.Fatalf("parts = %+v", got[0].Parts)
	}
	if files[0].MediaType != "image/png" || !bytes.Equal(files[0].Data, tinyPNG) {
		t.Errorf("image = %+v", files[0])
	}
	if files[1].MediaType != "application/pdf" || !bytes.Equal(files[1].Data, tinyPDF) {
		t.Errorf("pdf = %+v", files[1])
	}
	if attachments(messages[0])[0].Data != nil {
		t.Error("the input messages were modified")
	}
}

func TestResolveSendsWhatTheBytesAre(t *testing.T) {
	s := newStore()
	// Declared as a PNG, but not one: the provider would reject the mismatch.
	fake := s.add("evil.png", "image/png", []byte("<html>not an image</html>"))
	got, err := s.resolver(1<<20, llm.ModalityImage).resolve(context.Background(), []llm.Message{userWith("hi", fake)})
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments(got[0])) != 0 || !strings.Contains(noteText(got[0]), "evil.png") {
		t.Fatalf("parts = %+v, want a note instead of the file", got[0].Parts)
	}
}

// A text file goes as its words, to a model that takes no files at all, marked
// off with its name; one that is not UTF-8 text is left out and said to be.
func TestResolveSendsTextFilesAsWords(t *testing.T) {
	s := newStore()
	notes := s.add("notes.md", llm.TextMediaType, []byte("\xef\xbb\xbf# 会议记录\n\n下周一 10 点。\n"))
	binary := s.add("data.csv", llm.TextMediaType, []byte("a,b\x00\xff"))
	got, err := s.resolver(1<<20).resolve(context.Background(), []llm.Message{userWith("总结一下", notes, binary)})
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments(got[0])) != 0 {
		t.Fatalf("parts = %+v, want words only", got[0].Parts)
	}
	text := noteText(got[0])
	want := "总结一下\n\n<file name=\"notes.md\">\n# 会议记录\n\n下周一 10 点。\n</file>"
	if !strings.HasPrefix(text, want) {
		t.Errorf("text = %q, want it to start %q", text, want)
	}
	if !strings.Contains(text, `"data.csv" was not included: it is not UTF-8 text`) {
		t.Errorf("text = %q, want the binary file said to be left out", text)
	}
}

func TestResolveSaysWhatItLeftOut(t *testing.T) {
	s := newStore()
	picture := s.add("r.png", "image/png", tinyPNG)
	gone := llm.AttachmentPart{Ref: uuid.NewString(), Filename: "gone.png", MediaType: "image/png"}
	garbage := llm.AttachmentPart{Ref: "not-a-uuid", Filename: "x.png", MediaType: "image/png"}

	// A model that reads no pictures, a file that is not there, and a ref that
	// is no id. (A PDF goes as its words to a model that does not read PDFs.)
	got, err := s.resolver(1<<20).resolve(context.Background(),
		[]llm.Message{userWith("hi", picture, gone, garbage)})
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments(got[0])) != 0 {
		t.Fatalf("parts = %+v, want no file", got[0].Parts)
	}
	note := noteText(got[0])
	for _, want := range []string{"r.png", "does not accept", "gone.png", "no longer available", "x.png"} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q lacks %q", note, want)
		}
	}
	if len(s.reads) != 0 {
		t.Errorf("read %v for files that were not going to be sent", s.reads)
	}
}

func TestResolveKeepsTheNewestFilesWhenTheyDoNotAllFit(t *testing.T) {
	s := newStore()
	old := s.add("old.png", "image/png", tinyPNG)
	recent := s.add("new.png", "image/png", tinyPNG)
	messages := []llm.Message{userWith("first", old), {Role: llm.RoleAssistant, Parts: []llm.Part{llm.TextPart{Text: "ok"}}}, userWith("second", recent)}

	// Room for one.
	got, err := s.resolver(int64(len(tinyPNG)), llm.ModalityImage).resolve(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments(got[2])) != 1 {
		t.Errorf("the newest message lost its file: %+v", got[2].Parts)
	}
	if len(attachments(got[0])) != 0 || !strings.Contains(noteText(got[0]), "too many files") {
		t.Errorf("the oldest message kept its file: %+v", got[0].Parts)
	}
}

func TestResolveReadsEachFileOncePerRun(t *testing.T) {
	s := newStore()
	r := s.resolver(1<<20, llm.ModalityImage)
	messages := []llm.Message{userWith("look", s.add("a.png", "image/png", tinyPNG))}
	for range 3 { // the rounds of a tool loop
		if _, err := r.resolve(context.Background(), messages); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.reads) != 1 {
		t.Errorf("read %d times, want 1", len(s.reads))
	}
}

func TestResolveWithoutAnyAttachmentsAsksNothing(t *testing.T) {
	r := &attachmentResolver{lookup: func(context.Context, []uuid.UUID) (map[uuid.UUID]sqlc.Attachment, error) {
		t.Fatal("looked up files for a conversation with none")
		return nil, nil
	}}
	if _, err := r.resolve(context.Background(), []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}); err != nil {
		t.Fatal(err)
	}
}
