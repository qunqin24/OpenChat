package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// A message refers to another of its author's conversations: the model is
// sent what was asked and answered there up to the moment it was referred to,
// the message says what it refers to, and a reference that cannot be is
// refused - or, gone since, is said to be.
func TestAMessageReferencesAnotherConversation(t *testing.T) {
	h := newAskHarness(t, openAIWire, func(n int) string { return openAIWire.text(fmt.Sprintf("answer %d", n)) })
	sendWith := func(conv uuid.UUID, content string, refs ...uuid.UUID) *httptest.ResponseRecorder {
		t.Helper()
		ids := make([]string, len(refs))
		for i, id := range refs {
			ids[i] = fmt.Sprintf("%q", id)
		}
		body := fmt.Sprintf(`{"content":%q,"model":%q,"conversationIds":[%s]}`, content, h.model, strings.Join(ids, ","))
		rec := httptest.NewRecorder()
		h.server.CreateMessage(rec, h.request(http.MethodPost, body), conv)
		if rec.Code == http.StatusAccepted {
			var out CreateMessageResponse
			h.accepted(rec, &out)
			h.settle(out.Run.Id)
		}
		return rec
	}
	lastBody := func() string {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.bodies[len(h.bodies)-1]
	}

	other := h.newConv()
	if rec := h.sendIn(other.ID, "STRAWBERRY-QUESTION"); rec.Code != http.StatusAccepted {
		t.Fatalf("the other conversation: %d %s", rec.Code, rec.Body)
	}

	rec := sendWith(h.conv.ID, "use that conversation", other.ID)
	var out CreateMessageResponse
	h.accepted(rec, &out)
	if out.Message.References == nil || len(*out.Message.References) != 1 || (*out.Message.References)[0].ConversationId != other.ID {
		t.Fatalf("the message's references: %+v", out.Message.References)
	}
	body := lastBody()
	if !strings.Contains(body, "STRAWBERRY-QUESTION") || !strings.Contains(body, "answer 1") || !strings.Contains(body, "## Assistant") {
		t.Fatalf("the model was not sent the referenced conversation:\n%.1500s", body)
	}

	// What the other conversation says after it was referred to is not part of it.
	if rec := h.sendIn(other.ID, "LATER-QUESTION"); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body)
	}
	if rec := h.sendIn(h.conv.ID, "and again"); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body)
	}
	if body := lastBody(); !strings.Contains(body, "STRAWBERRY-QUESTION") || strings.Contains(body, "LATER-QUESTION") {
		t.Errorf("the reference moved with the conversation:\n%.1500s", body)
	}

	// Refused: this conversation, one that is not the author's, an empty one, too many.
	empty := h.newConv()
	for name, c := range map[string]struct {
		refs []uuid.UUID
		code string
	}{
		"this one":   {[]uuid.UUID{h.conv.ID}, "reference_unavailable"},
		"not theirs": {[]uuid.UUID{uuid.New()}, "reference_unavailable"},
		"empty":      {[]uuid.UUID{empty.ID}, "reference_empty"},
		"too many":   {[]uuid.UUID{other.ID, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}, "too_many_references"},
	} {
		if rec := sendWith(h.conv.ID, "x", c.refs...); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.code) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}

	// Deleted since: the model is told it is no longer there.
	if _, err := h.q.SoftDeleteConversation(context.Background(), sqlc.SoftDeleteConversationParams{ID: other.ID, UserID: &h.user.ID}); err != nil {
		t.Fatal(err)
	}
	if rec := h.sendIn(h.conv.ID, "once more"); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body)
	}
	if body := lastBody(); strings.Contains(body, "STRAWBERRY-QUESTION") || !strings.Contains(body, "no longer available") {
		t.Errorf("a deleted conversation is still read:\n%.1500s", body)
	}
}

// A conversation's title is its owner's to set, unchecked; referred to, it is
// the name the model reads. So it is moderated with the message that refers
// to it, and the model is given that title - not one set afterwards.
func TestAReferencedConversationsTitleIsModerated(t *testing.T) {
	f := newFakeModeration(t)
	h := newAskHarness(t, openAIWire, func(n int) string { return openAIWire.text(fmt.Sprintf("answer %d", n)) })
	resetModeration(t, h)
	h.server.usage = h.server.runner.Recorder()
	h.server.endUsers = NewEndUsers(h.pool)
	if rec := h.patchModeration(fmt.Sprintf(`{"mode":"openai","openaiApiKey":"sk-mod-secret-1234","openaiBaseUrl":%q}`, f.srv.URL+"/v1")); rec.Code != http.StatusOK {
		t.Fatalf("configure: %d %s", rec.Code, rec.Body)
	}
	ctx := context.Background()
	rename := func(id uuid.UUID, title string) {
		t.Helper()
		if _, err := h.q.UpdateConversationTitle(ctx, sqlc.UpdateConversationTitleParams{ID: id, UserID: &h.user.ID, Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	refer := func(conv, other uuid.UUID, content string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.server.CreateMessage(rec, h.request(http.MethodPost, fmt.Sprintf(`{"content":%q,"model":%q,"conversationIds":[%q]}`, content, h.model, other)), conv)
		if rec.Code == http.StatusAccepted {
			var out CreateMessageResponse
			h.accepted(rec, &out)
			h.settle(out.Run.Id)
		}
		return rec
	}

	other := h.newConv()
	if rec := h.sendIn(other.ID, "notes on strawberries"); rec.Code != http.StatusAccepted {
		t.Fatalf("the other conversation: %d %s", rec.Code, rec.Body)
	}

	rename(other.ID, "BLOCKME notes")
	conv := h.newConv()
	if rec := refer(conv.ID, other.ID, "what did we say?"); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "content_blocked") {
		t.Fatalf("a refused title was sent: %d %s", rec.Code, rec.Body)
	}

	rename(other.ID, "Strawberry notes")
	conv = h.newConv()
	if rec := refer(conv.ID, other.ID, "what did we say?"); rec.Code != http.StatusAccepted {
		t.Fatalf("a clean title: %d %s", rec.Code, rec.Body)
	}
	// Renamed after: the model still reads the title it was referred to by.
	rename(other.ID, "BLOCKME renamed")
	if rec := h.sendIn(conv.ID, "and again"); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body)
	}
	h.mu.Lock()
	body := h.bodies[len(h.bodies)-1]
	h.mu.Unlock()
	if strings.Contains(body, "BLOCKME") || !strings.Contains(body, "Strawberry notes") {
		t.Errorf("the model read the title as it is now:\n%.1200s", body)
	}
}
