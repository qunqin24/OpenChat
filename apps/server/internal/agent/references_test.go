package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

func TestAConversationReferenceNamesItsConversation(t *testing.T) {
	conv := sqlc.Conversation{ID: uuid.New(), Title: "草莓"}
	at := time.Date(2026, 10, 29, 9, 30, 0, 123456789, time.UTC)
	part := ConversationReference(conv, at, 42)
	id, got, ok := ReferencedConversation(part)
	if !ok || id != conv.ID || !got.Equal(at) || part.Filename != "草莓" || part.TextSize != 42 {
		t.Fatalf("%+v -> %v %v %v", part, id, got, ok)
	}
	if _, _, ok := ReferencedConversation(llm.AttachmentPart{Ref: uuid.NewString(), MediaType: llm.TextMediaType}); ok {
		t.Error("a file read as a reference")
	}
	if llm.ModalityOf(llm.ConversationMediaType) != llm.ModalityDocument {
		t.Error("a reference is not read as words")
	}
}

func TestAReferencedConversationReadsAsItsQuestionsAndAnswers(t *testing.T) {
	model := "m-1"
	q, a, quiet := uuid.New(), uuid.New(), uuid.New()
	rows := []sqlc.Message{
		{ID: q, Role: sqlc.MessageRoleUser, Content: "how?"},
		{ID: quiet, Role: sqlc.MessageRoleAssistant, Content: "  "},
		{ID: a, Role: sqlc.MessageRoleAssistant, Content: "like this", Model: &model},
	}
	text := renderConversation("", time.Now(), rows, map[uuid.UUID][]string{q: {"notes.txt"}})
	for _, want := range []string{"# Conversation\n", "## User\n\nhow?\n", "_Files attached: notes.txt_", "## Assistant (m-1)\n\nlike this\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "## Assistant") != 1 {
		t.Errorf("an answer with nothing in it is written:\n%s", text)
	}

	long := []sqlc.Message{{Role: sqlc.MessageRoleUser, Content: strings.Repeat("长", MaxReferenceBytes)}}
	if cut := renderConversation("t", time.Now(), long, nil); len(cut) > MaxReferenceBytes+200 || !strings.Contains(cut, "too long to include the rest") {
		t.Errorf("a long conversation is %d bytes", len(cut))
	}
}
